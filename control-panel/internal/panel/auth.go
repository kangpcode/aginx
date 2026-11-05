package panel

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	passwordIterations = 600_000
	sessionTTL         = 12 * time.Hour
	sessionIdleTTL     = 30 * time.Minute
	csrfHeader         = "X-CSRF-Token"
	sessionCookie      = "aginx_session"
)

var errBadPassword = errors.New("password tidak valid")

// HashPassword memakai PBKDF2-HMAC-SHA256 dari standard library. Argon2id
// dipertahankan sebagai target produksi; PBKDF2 dipilih pada slice ini agar
// binary tetap tanpa dependency eksternal.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations < 100_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func ValidatePassword(password, username, email string) error {
	if len(password) < 12 {
		return fmt.Errorf("%w: minimal 12 karakter", errBadPassword)
	}
	if len(password) > 256 {
		return fmt.Errorf("%w: maksimal 256 karakter", errBadPassword)
	}
	lower := strings.ToLower(password)
	if strings.Contains(lower, strings.ToLower(username)) || strings.Contains(lower, strings.ToLower(email)) {
		return fmt.Errorf("%w: tidak boleh memuat username/email", errBadPassword)
	}
	for _, common := range []string{"password", "123456", "qwerty", "admin", "aginx"} {
		if strings.Contains(lower, common) {
			return fmt.Errorf("%w: terlalu mudah ditebak", errBadPassword)
		}
	}
	return nil
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewSession(userID, ip, userAgent string, now time.Time) (Session, string, error) {
	token, err := NewToken()
	if err != nil {
		return Session{}, "", err
	}
	csrf, err := NewToken()
	if err != nil {
		return Session{}, "", err
	}
	return Session{
		ID:        newID(),
		UserID:    userID,
		TokenHash: TokenHash(token),
		CSRF:      csrf,
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: now.UTC(),
		LastSeen:  now.UTC(),
		ExpiresAt: now.UTC().Add(sessionTTL),
	}, token, nil
}

func CSRFValid(session Session, provided string) bool {
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(provided)) == 1
}

// loginLimiter membatasi percobaan per (IP, login) tanpa tabel eksternal.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time)}
}

func (l *loginLimiter) key(ip, login string) string {
	return ip + "|" + strings.ToLower(login)
}

func (l *loginLimiter) allowed(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-15 * time.Minute)
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	l.attempts[key] = kept
	return len(kept) < 5
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func clientIP(r *http.Request) string {
	// Tidak mempercayai X-Forwarded-For pada slice ini: panel default diakses
	// langsung via localhost/SSH tunnel.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	domainPattern = regexp.MustCompile(`^(?i)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

var reservedSlugs = map[string]bool{
	"www": true, "mail": true, "ftp": true, "admin": true,
	"panel": true, "api": true, "static": true, "assets": true,
}

func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return errors.New("slug harus huruf kecil, angka, atau tanda hubung (2-63 karakter)")
	}
	if reservedSlugs[slug] {
		return errors.New("slug ini dicadangkan")
	}
	return nil
}

func ValidateDomain(domain string) error {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if len(domain) > 253 || !domainPattern.MatchString(domain) {
		return errors.New("domain harus FQDN valid, tanpa skema atau path")
	}
	return nil
}
