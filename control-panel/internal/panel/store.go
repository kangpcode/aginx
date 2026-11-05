// Package panel mengimplementasikan backend AGinX Control Panel.
//
// Slice pertama menyimpan metadata di satu file JSON dengan penulisan atomik.
// Ini menjaga binary tetap tanpa dependency eksternal; kontrak struct dibuat
// kompatibel dengan skema SQLite di PRD agar migrasi berikutnya tidak mengubah API.
package panel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound dipakai handler untuk memetakan hasil ke HTTP 404.
var ErrNotFound = errors.New("tidak ditemukan")

// ErrConflict dipakai untuk pelanggaran keunikan (slug/domain).
var ErrConflict = errors.New("konflik data")

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"password_hash"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	CSRF      string    `json:"csrf"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Site struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	Docroot   string    `json:"docroot"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Revision struct {
	ID         string    `json:"id"`
	SiteID     string    `json:"site_id"`
	Path       string    `json:"path"`
	Content    string    `json:"-"`
	Hash       string    `json:"hash"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`
	Published  time.Time `json:"published_at,omitempty"`
}

type AuditEvent struct {
	ID        string    `json:"id"`
	TS        time.Time `json:"ts"`
	ActorID   string    `json:"actor_id,omitempty"`
	ActorName string    `json:"actor_name,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Action    string    `json:"action"`
	Target    string    `json:"target,omitempty"`
	Result    string    `json:"result"`
	Meta      string    `json:"meta,omitempty"`
	PrevHash  string    `json:"prev_hash,omitempty"`
	Hash      string    `json:"hash"`
}

type Data struct {
	Users     []User       `json:"users"`
	Sessions  []Session    `json:"sessions"`
	Sites     []Site       `json:"sites"`
	Revisions []Revision   `json:"revisions"`
	Audit     []AuditEvent `json:"audit"`
}

// Store adalah satu-satunya pemilik state panel. Semua mutasi diserialisasi
// dengan mutex tunggal; untuk jumlah situs v1 ini jauh lebih murah daripada
// contention SQLite dan cukup untuk satu proses.
type Store struct {
	mu   sync.RWMutex
	path string
	dir  string
	data Data
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("buat direktori data: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "panel.json"), dir: dir}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("panel.json rusak: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, fmt.Errorf("baca panel.json: %w", err)
	}
	if s.data.Users == nil {
		s.data.Users = []User{}
	}
	if s.data.Sessions == nil {
		s.data.Sessions = []Session{}
	}
	if s.data.Sites == nil {
		s.data.Sites = []Site{}
	}
	if s.data.Revisions == nil {
		s.data.Revisions = []Revision{}
	}
	if s.data.Audit == nil {
		s.data.Audit = []AuditEvent{}
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand gagal berarti OS dalam kondisi fatal; jangan lanjutkan
		// dengan ID yang dapat diprediksi.
		panic("crypto/rand tidak tersedia: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".panel-*.tmp")
	if err != nil {
		return fmt.Errorf("buat temp state: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("tulis state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return syncDir(s.dir)
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *Store) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Users) > 0
}

func (s *Store) CreateUser(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Users {
		if strings.EqualFold(existing.Username, u.Username) || strings.EqualFold(existing.Email, u.Email) {
			return ErrConflict
		}
	}
	if u.ID == "" {
		u.ID = newID()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	s.data.Users = append(s.data.Users, u)
	return s.saveLocked()
}

func (s *Store) FindUserByLogin(login string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.data.Users {
		if strings.EqualFold(u.Username, login) || strings.EqualFold(u.Email, login) {
			return u, true
		}
	}
	return User{}, false
}

func (s *Store) GetUser(id string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.data.Users {
		if u.ID == id {
			return u, true
		}
	}
	return User{}, false
}

func (s *Store) CountUsers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Users)
}

func (s *Store) TouchLogin(userID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Users {
		if s.data.Users[i].ID == userID {
			s.data.Users[i].LastLoginAt = at.UTC()
			_ = s.saveLocked()
			return
		}
	}
}

func (s *Store) CreateSession(session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneSessionsLocked(time.Now().UTC())
	s.data.Sessions = append(s.data.Sessions, session)
	return s.saveLocked()
}

// SessionByToken mencari sesi aktif, menegakkan idle timeout, dan menyimpan
// last_seen paling sering sekali per menit agar tidak menulis file tiap request.
func (s *Store) SessionByToken(hash string, now time.Time) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, session := range s.data.Sessions {
		if session.TokenHash != hash {
			continue
		}
		if now.After(session.ExpiresAt) || now.Sub(session.LastSeen) > 30*time.Minute {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			_ = s.saveLocked()
			return Session{}, false
		}
		if now.Sub(session.LastSeen) > time.Minute {
			s.data.Sessions[i].LastSeen = now.UTC()
			_ = s.saveLocked()
			session.LastSeen = now.UTC()
		}
		return session, true
	}
	return Session{}, false
}

func (s *Store) DeleteSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, session := range s.data.Sessions {
		if session.ID == id {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			_ = s.saveLocked()
			return
		}
	}
}

func (s *Store) pruneSessionsLocked(now time.Time) {
	kept := s.data.Sessions[:0]
	for _, session := range s.data.Sessions {
		if now.Before(session.ExpiresAt) && now.Sub(session.LastSeen) <= 30*time.Minute {
			kept = append(kept, session)
		}
	}
	s.data.Sessions = kept
}

func (s *Store) ListSites() []Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Site(nil), s.data.Sites...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) GetSite(id string) (Site, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, site := range s.data.Sites {
		if site.ID == id {
			return site, true
		}
	}
	return Site{}, false
}

func (s *Store) CreateSite(site Site) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Sites {
		if strings.EqualFold(existing.Slug, site.Slug) ||
			strings.EqualFold(existing.Domain, site.Domain) ||
			existing.Docroot == site.Docroot {
			return ErrConflict
		}
	}
	if site.ID == "" {
		site.ID = newID()
	}
	now := time.Now().UTC()
	if site.CreatedAt.IsZero() {
		site.CreatedAt = now
	}
	site.UpdatedAt = now
	s.data.Sites = append(s.data.Sites, site)
	return s.saveLocked()
}

func (s *Store) DeleteSite(id string) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, site := range s.data.Sites {
		if site.ID != id {
			continue
		}
		s.data.Sites = append(s.data.Sites[:i], s.data.Sites[i+1:]...)
		kept := s.data.Revisions[:0]
		for _, rev := range s.data.Revisions {
			if rev.SiteID != id {
				kept = append(kept, rev)
			}
		}
		s.data.Revisions = kept
		if err := s.saveLocked(); err != nil {
			return Site{}, err
		}
		return site, nil
	}
	return Site{}, ErrNotFound
}

func (s *Store) AddRevision(rev Revision) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rev.ID == "" {
		rev.ID = newID()
	}
	if rev.CreatedAt.IsZero() {
		rev.CreatedAt = time.Now().UTC()
	}
	if rev.Status == "published" {
		for i := range s.data.Revisions {
			if s.data.Revisions[i].SiteID == rev.SiteID &&
				s.data.Revisions[i].Path == rev.Path &&
				s.data.Revisions[i].Status == "published" {
				s.data.Revisions[i].Status = "superseded"
			}
		}
		rev.Published = time.Now().UTC()
	}
	s.data.Revisions = append(s.data.Revisions, rev)
	// Batasi riwayat agar file state tidak tumbuh tanpa batas.
	var count int
	for _, existing := range s.data.Revisions {
		if existing.SiteID == rev.SiteID && existing.Path == rev.Path {
			count++
		}
	}
	if count > 200 {
		s.trimRevisionsLocked(rev.SiteID, rev.Path)
	}
	if err := s.saveLocked(); err != nil {
		return Revision{}, err
	}
	return rev, nil
}

func (s *Store) trimRevisionsLocked(siteID, path string) {
	// Hapus draft tertua lebih dulu, lalu published tertua kecuali yang terakhir.
	var ids []string
	for _, rev := range s.data.Revisions {
		if rev.SiteID == siteID && rev.Path == path && rev.Status == "draft" {
			ids = append(ids, rev.ID)
		}
	}
	drop := map[string]bool{}
	if len(ids) > 50 {
		for _, id := range ids[:len(ids)-50] {
			drop[id] = true
		}
	}
	kept := s.data.Revisions[:0]
	for _, rev := range s.data.Revisions {
		if !drop[rev.ID] {
			kept = append(kept, rev)
		}
	}
	s.data.Revisions = kept
}

func (s *Store) ListRevisions(siteID, path string) []Revision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Revision
	for _, rev := range s.data.Revisions {
		if rev.SiteID == siteID && rev.Path == path {
			copy := rev
			copy.Content = ""
			out = append(out, copy)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) GetRevision(id string) (Revision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rev := range s.data.Revisions {
		if rev.ID == id {
			return rev, true
		}
	}
	return Revision{}, false
}

// AddAudit menambahkan event dengan hash berantai agar manipulasi isi log
// dapat dideteksi. Log lama dipangkas pada 20.000 event.
func (s *Store) AddAudit(event AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = newID()
	event.TS = time.Now().UTC()
	if n := len(s.data.Audit); n > 0 {
		event.PrevHash = s.data.Audit[n-1].Hash
	}
	event.Hash = hashAudit(event)
	s.data.Audit = append(s.data.Audit, event)
	if len(s.data.Audit) > 20000 {
		s.data.Audit = s.data.Audit[len(s.data.Audit)-20000:]
	}
	_ = s.saveLocked()
}

func hashAudit(e AuditEvent) string {
	e.Hash = ""
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(append([]byte(e.PrevHash), raw...))
	return hex.EncodeToString(sum[:])
}

func (s *Store) ListAudit(limit int) []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := make([]AuditEvent, 0, limit)
	for i := len(s.data.Audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.data.Audit[i])
	}
	return out
}
