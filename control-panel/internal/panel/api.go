package panel

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxJSONBody = 1 << 20

type ctxKey int

const authKey ctxKey = 1

type authContext struct {
	User    User
	Session Session
}

// userView menghindari kebocoran hash password ke response JSON.
type userView struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

func viewUser(u User) userView {
	return userView{u.ID, u.Username, u.Email, u.DisplayName, u.Role, u.CreatedAt, u.LastLoginAt}
}

type Panel struct {
	store      *Store
	configs    *ConfigService
	system     *SystemSampler
	logger     *slog.Logger
	limiter    *loginLimiter
	wwwBase    string
	dataDir    string
	version    string
	reloader   *EngineReloader
	setupToken string
}

func NewPanel(store *Store, wwwBase, dataDir, version string, reloader *EngineReloader, logger *slog.Logger) (*Panel, error) {
	if logger == nil {
		logger = slog.Default()
	}
	base, err := filepath.Abs(wwwBase)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(base, 0o750); err != nil {
		return nil, fmt.Errorf("buat direktori www: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	p := &Panel{
		store:    store,
		configs:  NewConfigService(store, logger, reloader),
		system:   NewSystemSampler(dataDir),
		logger:   logger,
		limiter:  newLoginLimiter(),
		wwwBase:  base,
		dataDir:  dataDir,
		version:  version,
		reloader: reloader,
	}
	if !store.HasUsers() {
		token, err := NewToken()
		if err != nil {
			return nil, err
		}
		p.setupToken = token
		path := filepath.Join(dataDir, "setup.token")
		if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("tulis setup token: %w", err)
		}
		logger.Warn("panel belum memiliki user; gunakan setup token", "file", path)
	}
	return p, nil
}

func (p *Panel) SetupToken() string { return p.setupToken }

func (p *Panel) WWWBase() string { return p.wwwBase }

// Register mendaftarkan seluruh route API. Middleware global dipasang di main.
func (p *Panel) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", p.health)
	mux.HandleFunc("GET /api/v1/setup/status", p.setupStatus)
	mux.HandleFunc("POST /api/v1/setup", p.setup)
	mux.HandleFunc("POST /api/v1/auth/login", p.login)

	mux.Handle("POST /api/v1/auth/logout", p.authed(http.HandlerFunc(p.logout)))
	mux.Handle("GET /api/v1/auth/me", p.authed(http.HandlerFunc(p.me)))
	mux.Handle("GET /api/v1/overview", p.authed(http.HandlerFunc(p.overview)))
	mux.Handle("GET /api/v1/audit", p.authed(http.HandlerFunc(p.auditList)))

	mux.Handle("GET /api/v1/sites", p.authed(http.HandlerFunc(p.listSites)))
	mux.Handle("POST /api/v1/sites", p.authed(http.HandlerFunc(p.createSite)))
	mux.Handle("GET /api/v1/sites/{id}", p.authed(http.HandlerFunc(p.getSite)))
	mux.Handle("DELETE /api/v1/sites/{id}", p.authed(http.HandlerFunc(p.deleteSite)))
	mux.Handle("GET /api/v1/sites/{id}/configs", p.authed(http.HandlerFunc(p.listConfigs)))
	mux.Handle("GET /api/v1/sites/{id}/config", p.authed(http.HandlerFunc(p.readConfig)))
	mux.Handle("PUT /api/v1/sites/{id}/config/draft", p.authed(http.HandlerFunc(p.saveDraft)))
	mux.Handle("POST /api/v1/sites/{id}/config/validate", p.authed(http.HandlerFunc(p.validateConfig)))
	mux.Handle("POST /api/v1/sites/{id}/config/publish", p.authed(http.HandlerFunc(p.publishConfig)))
	mux.Handle("GET /api/v1/sites/{id}/config/revisions", p.authed(http.HandlerFunc(p.listRevisions)))
	mux.Handle("GET /api/v1/sites/{id}/config/revisions/{revID}", p.authed(http.HandlerFunc(p.getRevision)))
	mux.Handle("POST /api/v1/sites/{id}/config/rollback", p.authed(http.HandlerFunc(p.rollbackConfig)))
}

func (p *Panel) authed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			p.fail(w, r, http.StatusUnauthorized, "unauthorized", "Sesi tidak ditemukan", nil)
			return
		}
		session, ok := p.store.SessionByToken(TokenHash(cookie.Value), time.Now())
		if !ok {
			p.fail(w, r, http.StatusUnauthorized, "unauthorized", "Sesi kedaluwarsa", nil)
			return
		}
		user, ok := p.store.GetUser(session.UserID)
		if !ok {
			p.fail(w, r, http.StatusUnauthorized, "unauthorized", "Pengguna tidak ditemukan", nil)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !CSRFValid(session, r.Header.Get(csrfHeader)) {
				p.fail(w, r, http.StatusForbidden, "csrf_invalid", "Token CSRF tidak valid", nil)
				return
			}
		}
		ctx := context.WithValue(r.Context(), authKey, authContext{User: user, Session: session})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authFrom(r *http.Request) authContext {
	value, _ := r.Context().Value(authKey).(authContext)
	return value
}

// Middleware mengembalikan handler dengan proteksi dasar untuk semua request.
func (p *Panel) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.URL.Path != "/api/v1/health" {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !p.originAllowed(r) {
			p.fail(w, r, http.StatusForbidden, "origin_invalid", "Origin tidak cocok dengan host panel", nil)
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				p.logger.Error("panic pada handler", "panic", recovered, "path", r.URL.Path, "request_id", requestID)
				p.fail(w, r, http.StatusInternalServerError, "internal", "Terjadi kesalahan internal", nil)
			}
			p.logger.Info("request", "method", r.Method, "path", r.URL.Path,
				"elapsed_ms", time.Since(start).Milliseconds(), "request_id", requestID)
		}()
		next.ServeHTTP(w, r)
	})
}

func (p *Panel) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func (p *Panel) health(w http.ResponseWriter, r *http.Request) {
	p.data(w, http.StatusOK, map[string]any{"status": "ok", "version": p.version, "time": time.Now().UTC()})
}

func (p *Panel) setupStatus(w http.ResponseWriter, r *http.Request) {
	p.data(w, http.StatusOK, map[string]any{"needs_setup": !p.store.HasUsers()})
}

type setupRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,31}$`)

func (p *Panel) setup(w http.ResponseWriter, r *http.Request) {
	if p.store.HasUsers() {
		p.fail(w, r, http.StatusForbidden, "setup_closed", "Panel sudah dikonfigurasi", nil)
		return
	}
	var req setupRequest
	if !p.decode(w, r, &req) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(p.setupToken)) != 1 {
		p.fail(w, r, http.StatusForbidden, "setup_token", "Setup token tidak valid", nil)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if !usernamePattern.MatchString(req.Username) {
		p.fail(w, r, http.StatusUnprocessableEntity, "username_invalid", "Username 3-32 karakter: huruf, angka, titik, garis bawah, tanda hubung", nil)
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil || !strings.Contains(req.Email, "@") {
		p.fail(w, r, http.StatusUnprocessableEntity, "email_invalid", "Email tidak valid", nil)
		return
	}
	if err := ValidatePassword(req.Password, req.Username, req.Email); err != nil {
		p.fail(w, r, http.StatusUnprocessableEntity, "password_invalid", err.Error(), nil)
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "hash_failed", "Gagal mengamankan password", nil)
		return
	}
	user := User{
		Username:     req.Username,
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		PasswordHash: hash,
		Role:         "owner",
	}
	if err := p.store.CreateUser(user); err != nil {
		p.fail(w, r, http.StatusConflict, "user_conflict", "Username atau email sudah dipakai", nil)
		return
	}
	user, _ = p.store.FindUserByLogin(req.Username)
	session, token, err := NewSession(user.ID, clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "session_failed", "Gagal membuat sesi", nil)
		return
	}
	if err := p.store.CreateSession(session); err != nil {
		p.fail(w, r, http.StatusInternalServerError, "session_failed", "Gagal menyimpan sesi", nil)
		return
	}
	p.setCookie(w, r, token, session.ExpiresAt)
	_ = os.Remove(filepath.Join(p.dataDir, "setup.token"))
	p.setupToken = ""
	p.store.AddAudit(AuditEvent{ActorID: user.ID, ActorName: user.Username, IP: clientIP(r),
		Action: "auth.setup", Target: "panel", Result: "success"})
	p.data(w, http.StatusCreated, map[string]any{"user": viewUser(user), "csrf": session.CSRF})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (p *Panel) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !p.decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	key := p.limiter.key(ip, req.Username)
	now := time.Now()
	if !p.limiter.allowed(key, now) {
		p.fail(w, r, http.StatusTooManyRequests, "rate_limited", "Terlalu banyak percobaan. Coba lagi dalam 15 menit.", nil)
		return
	}
	user, ok := p.store.FindUserByLogin(strings.TrimSpace(req.Username))
	if !ok || !VerifyPassword(user.PasswordHash, req.Password) {
		p.limiter.fail(key, now)
		p.store.AddAudit(AuditEvent{IP: ip, Action: "auth.failed", Target: req.Username, Result: "failure"})
		p.fail(w, r, http.StatusUnauthorized, "credentials_invalid", "Username atau password salah", nil)
		return
	}
	p.limiter.reset(key)
	session, token, err := NewSession(user.ID, ip, r.UserAgent(), now)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "session_failed", "Gagal membuat sesi", nil)
		return
	}
	if err := p.store.CreateSession(session); err != nil {
		p.fail(w, r, http.StatusInternalServerError, "session_failed", "Gagal menyimpan sesi", nil)
		return
	}
	p.store.TouchLogin(user.ID, now)
	p.setCookie(w, r, token, session.ExpiresAt)
	p.store.AddAudit(AuditEvent{ActorID: user.ID, ActorName: user.Username, IP: ip,
		Action: "auth.login", Target: "panel", Result: "success"})
	p.data(w, http.StatusOK, map[string]any{"user": viewUser(user), "csrf": session.CSRF})
}

func (p *Panel) logout(w http.ResponseWriter, r *http.Request) {
	auth := authFrom(r)
	p.store.DeleteSession(auth.Session.ID)
	p.clearCookie(w, r)
	p.store.AddAudit(AuditEvent{ActorID: auth.User.ID, ActorName: auth.User.Username, IP: clientIP(r),
		Action: "auth.logout", Target: "panel", Result: "success"})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) me(w http.ResponseWriter, r *http.Request) {
	auth := authFrom(r)
	p.data(w, http.StatusOK, map[string]any{
		"user":       viewUser(auth.User),
		"csrf":       auth.Session.CSRF,
		"expires_at": auth.Session.ExpiresAt,
		"version":    p.version,
	})
}

func (p *Panel) overview(w http.ResponseWriter, r *http.Request) {
	sites := p.store.ListSites()
	active := 0
	for _, site := range sites {
		if site.Status == "active" {
			active++
		}
	}
	engineState := "not_configured"
	if p.reloader != nil && p.reloader.PIDFile != "" {
		engineState = "configured"
	}
	p.data(w, http.StatusOK, map[string]any{
		"system":       p.system.Overview(),
		"sites":        map[string]any{"total": len(sites), "active": active},
		"engine":       map[string]any{"state": engineState, "reload": "SIGHUP"},
		"version":      p.version,
		"www_base":     p.wwwBase,
		"recent_audit": p.store.ListAudit(8),
	})
}

func (p *Panel) auditList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	p.data(w, http.StatusOK, p.store.ListAudit(limit))
}

type createSiteRequest struct {
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Domain string `json:"domain"`
}

func (p *Panel) listSites(w http.ResponseWriter, r *http.Request) {
	sites := p.store.ListSites()
	out := make([]map[string]any, 0, len(sites))
	for _, site := range sites {
		configs, _ := ListConfigPaths(site.Docroot)
		out = append(out, map[string]any{"site": site, "configs": configs})
	}
	p.data(w, http.StatusOK, out)
}

func (p *Panel) createSite(w http.ResponseWriter, r *http.Request) {
	auth := authFrom(r)
	var req createSiteRequest
	if !p.decode(w, r, &req) {
		return
	}
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	req.Domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Domain)), ".")
	if err := ValidateSlug(req.Slug); err != nil {
		p.fail(w, r, http.StatusUnprocessableEntity, "slug_invalid", err.Error(), nil)
		return
	}
	if err := ValidateDomain(req.Domain); err != nil {
		p.fail(w, r, http.StatusUnprocessableEntity, "domain_invalid", err.Error(), nil)
		return
	}
	if req.Name == "" {
		req.Name = req.Slug
	}
	if len(req.Name) > 80 {
		p.fail(w, r, http.StatusUnprocessableEntity, "name_invalid", "Nama maksimal 80 karakter", nil)
		return
	}
	docroot := DocrootFor(p.wwwBase, req.Slug)
	if !ensureInside(p.wwwBase, docroot) {
		p.fail(w, r, http.StatusUnprocessableEntity, "path_invalid", "Docroot keluar dari direktori www", nil)
		return
	}
	if _, err := os.Stat(docroot); err == nil {
		p.fail(w, r, http.StatusConflict, "docroot_exists", "Docroot sudah ada di disk", nil)
		return
	}
	created := false
	if err := os.MkdirAll(docroot, 0o750); err != nil {
		p.fail(w, r, http.StatusInternalServerError, "docroot_failed", "Gagal membuat docroot", nil)
		return
	}
	created = true
	rollback := func() {
		if created {
			_ = os.RemoveAll(filepath.Join(p.wwwBase, req.Slug))
		}
	}
	seed := "# Konfigurasi awal dibuat oleh AGinX Control Panel\n" +
		"header set X-Content-Type-Options \"nosniff\";\n"
	if err := os.WriteFile(filepath.Join(docroot, ".ginxaccess"), []byte(seed), 0o644); err != nil {
		rollback()
		p.fail(w, r, http.StatusInternalServerError, "config_failed", "Gagal menulis konfigurasi awal", nil)
		return
	}
	index := "<!doctype html>\n<html lang=\"id\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + html.EscapeString(req.Name) + "</title>" +
		"<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:15vh auto;padding:0 1.5rem;color:#0f172a}" +
		"code{background:#f1f5f9;padding:.2rem .4rem;border-radius:.3rem}</style></head><body>" +
		"<h1>" + html.EscapeString(req.Name) + "</h1>" +
		"<p>Situs ini dikelola oleh AGinX Control Panel.</p>" +
		"<p>Domain: <code>" + html.EscapeString(req.Domain) + "</code></p></body></html>\n"
	if _, err := os.Stat(filepath.Join(docroot, "index.html")); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(filepath.Join(docroot, "index.html"), []byte(index), 0o644); err != nil {
			rollback()
			p.fail(w, r, http.StatusInternalServerError, "index_failed", "Gagal menulis halaman awal", nil)
			return
		}
	}
	site := Site{
		Slug:    req.Slug,
		Name:    req.Name,
		Domain:  req.Domain,
		Docroot: docroot,
		Status:  "active",
	}
	if err := p.store.CreateSite(site); err != nil {
		rollback()
		if errors.Is(err, ErrConflict) {
			p.fail(w, r, http.StatusConflict, "site_conflict", "Slug, domain, atau docroot sudah dipakai", nil)
			return
		}
		p.fail(w, r, http.StatusInternalServerError, "site_failed", "Gagal menyimpan situs", nil)
		return
	}
	site, _ = p.siteBySlug(req.Slug)
	p.store.AddAudit(AuditEvent{ActorID: auth.User.ID, ActorName: auth.User.Username, IP: clientIP(r),
		Action: "site.create", Target: site.Slug, Result: "success", Meta: site.Domain})
	p.data(w, http.StatusCreated, site)
}

func (p *Panel) siteBySlug(slug string) (Site, bool) {
	for _, site := range p.store.ListSites() {
		if site.Slug == slug {
			return site, true
		}
	}
	return Site{}, false
}

func (p *Panel) getSite(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	configs, _ := ListConfigPaths(site.Docroot)
	p.data(w, http.StatusOK, map[string]any{"site": site, "configs": configs})
}

type deleteSiteRequest struct {
	Confirm string `json:"confirm"`
}

func (p *Panel) deleteSite(w http.ResponseWriter, r *http.Request) {
	auth := authFrom(r)
	var req deleteSiteRequest
	if !p.decode(w, r, &req) {
		return
	}
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	if req.Confirm != site.Slug {
		p.fail(w, r, http.StatusUnprocessableEntity, "confirm_mismatch", "Ketik nama situs untuk konfirmasi", nil)
		return
	}
	if !ensureInside(p.wwwBase, site.Docroot) {
		p.fail(w, r, http.StatusInternalServerError, "path_invalid", "Docroot berada di luar direktori www", nil)
		return
	}
	moved := ""
	if _, err := os.Stat(site.Docroot); err == nil {
		trash := filepath.Join(p.wwwBase, ".trash")
		if err := os.MkdirAll(trash, 0o750); err != nil {
			p.fail(w, r, http.StatusInternalServerError, "trash_failed", "Gagal menyiapkan trash", nil)
			return
		}
		dest := filepath.Join(trash, fmt.Sprintf("%s-%d", site.Slug, time.Now().Unix()))
		if err := os.Rename(site.Docroot, dest); err != nil {
			p.fail(w, r, http.StatusInternalServerError, "trash_failed", "Gagal memindahkan docroot ke trash", nil)
			return
		}
		moved = dest
	}
	if _, err := p.store.DeleteSite(site.ID); err != nil {
		// Kembalikan docroot jika metadata gagal dihapus agar situs tidak setengah terhapus.
		if moved != "" {
			_ = os.Rename(moved, site.Docroot)
		}
		p.fail(w, r, http.StatusInternalServerError, "delete_failed", "Gagal menghapus metadata situs", nil)
		return
	}
	p.store.AddAudit(AuditEvent{ActorID: auth.User.ID, ActorName: auth.User.Username, IP: clientIP(r),
		Action: "site.delete", Target: site.Slug, Result: "success"})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) listConfigs(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	configs, err := ListConfigPaths(site.Docroot)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "list_failed", "Gagal membaca daftar konfigurasi", nil)
		return
	}
	p.data(w, http.StatusOK, configs)
}

func (p *Panel) readConfig(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	virtual := r.URL.Query().Get("path")
	content, exists, modTime, err := ReadConfig(site.Docroot, virtual)
	if err != nil {
		p.fail(w, r, http.StatusBadRequest, "path_invalid", err.Error(), nil)
		return
	}
	p.data(w, http.StatusOK, map[string]any{
		"path": normalizeVirtual(virtual), "content": content,
		"exists": exists, "size": len(content), "mod_time": modTime,
	})
}

type configRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (p *Panel) saveDraft(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	var req configRequest
	if !p.decode(w, r, &req) {
		return
	}
	rev, issues, err := p.configs.Draft(site, req.Path, req.Content, authFrom(r).User)
	if err != nil {
		p.fail(w, r, http.StatusUnprocessableEntity, "draft_failed", err.Error(), issues)
		return
	}
	p.data(w, http.StatusOK, map[string]any{"revision": rev, "issues": issues})
}

func (p *Panel) validateConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequest
	if !p.decode(w, r, &req) {
		return
	}
	issues, valid := p.configs.Validate(req.Content)
	p.data(w, http.StatusOK, map[string]any{"valid": valid, "issues": issues})
}

func (p *Panel) publishConfig(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	var req configRequest
	if !p.decode(w, r, &req) {
		return
	}
	result, issues, err := p.configs.Publish(site, req.Path, req.Content, authFrom(r).User, clientIP(r), "config.publish")
	if err != nil {
		status := http.StatusInternalServerError
		code := "publish_failed"
		if len(issues) > 0 {
			status = http.StatusUnprocessableEntity
			code = "config_invalid"
		}
		p.fail(w, r, status, code, err.Error(), issues)
		return
	}
	p.data(w, http.StatusOK, result)
}

func (p *Panel) listRevisions(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	virtual := normalizeVirtual(r.URL.Query().Get("path"))
	p.data(w, http.StatusOK, p.store.ListRevisions(site.ID, virtual))
}

func (p *Panel) getRevision(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	rev, ok := p.store.GetRevision(r.PathValue("revID"))
	if !ok || rev.SiteID != site.ID {
		p.fail(w, r, http.StatusNotFound, "revision_not_found", "Revisi tidak ditemukan", nil)
		return
	}
	p.data(w, http.StatusOK, rev)
}

type rollbackRequest struct {
	RevisionID string `json:"revision_id"`
}

func (p *Panel) rollbackConfig(w http.ResponseWriter, r *http.Request) {
	site, ok := p.store.GetSite(r.PathValue("id"))
	if !ok {
		p.fail(w, r, http.StatusNotFound, "site_not_found", "Situs tidak ditemukan", nil)
		return
	}
	var req rollbackRequest
	if !p.decode(w, r, &req) {
		return
	}
	result, issues, err := p.configs.Rollback(site, req.RevisionID, authFrom(r).User, clientIP(r))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			p.fail(w, r, http.StatusNotFound, "revision_not_found", "Revisi tidak ditemukan", nil)
			return
		}
		p.fail(w, r, http.StatusInternalServerError, "rollback_failed", err.Error(), issues)
		return
	}
	p.data(w, http.StatusOK, result)
}

func (p *Panel) setCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
	})
}

func (p *Panel) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}

func (p *Panel) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		p.fail(w, r, http.StatusBadRequest, "body_invalid", "Body JSON tidak valid: "+err.Error(), nil)
		return false
	}
	return true
}

func (p *Panel) data(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value})
}

func (p *Panel) fail(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := map[string]any{
		"error": map[string]any{
			"code": code, "message": message, "details": details,
			"request_id": w.Header().Get("X-Request-ID"),
		},
	}
	_ = json.NewEncoder(w).Encode(body)
}
