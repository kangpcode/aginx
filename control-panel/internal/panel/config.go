package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"

	"aginx/internal/ginxaccess"
)

// Issue adalah error konfigurasi yang sudah dipetakan ke baris sumber.
type Issue struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type PublishResult struct {
	Path      string   `json:"path"`
	Revision  Revision `json:"revision"`
	Reload    string   `json:"reload"`
	ReloadErr string   `json:"reload_error,omitempty"`
}

// ConfigService memusatkan validasi, penulisan, revisi, dan reload engine.
type ConfigService struct {
	store    *Store
	logger   *slog.Logger
	reloader *EngineReloader
}

func NewConfigService(store *Store, logger *slog.Logger, reloader *EngineReloader) *ConfigService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigService{store: store, logger: logger, reloader: reloader}
}

// Validate memakai parser engine yang sama dengan runtime. Panel tidak
// memiliki parser kedua yang bisa menyimpang dari semantik engine.
func (c *ConfigService) Validate(content string) ([]Issue, bool) {
	if len(content) > maxConfigBytes {
		return []Issue{{Line: 1, Message: fmt.Sprintf("file melebihi %d KiB", maxConfigBytes/1024)}}, false
	}
	if _, err := ginxaccess.Parse([]byte(content)); err != nil {
		return []Issue{engineIssue(err)}, false
	}
	return nil, true
}

func engineIssue(err error) Issue {
	msg := err.Error()
	issue := Issue{Line: 0, Message: msg}
	if !strings.HasPrefix(msg, "baris ") {
		return issue
	}
	rest := strings.TrimPrefix(msg, "baris ")
	colon := strings.Index(rest, ":")
	if colon <= 0 {
		return issue
	}
	line, parseErr := strconv.Atoi(strings.TrimSpace(rest[:colon]))
	if parseErr != nil || line < 1 {
		return issue
	}
	issue.Line = line
	issue.Message = strings.TrimSpace(rest[colon+1:])
	return issue
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Draft menyimpan isi editor tanpa menyentuh file aktif. Draft invalid tetap
// disimpan agar pengguna tidak kehilangan pekerjaan.
func (c *ConfigService) Draft(site Site, virtual, content string, user User) (Revision, []Issue, error) {
	if len(content) > maxConfigBytes {
		return Revision{}, []Issue{{Line: 1, Message: "file melebihi 64 KiB"}}, errors.New("konten terlalu besar")
	}
	issues, valid := c.Validate(content)
	status := "draft"
	errText := ""
	if !valid {
		status = "draft_invalid"
		if len(issues) > 0 {
			errText = fmt.Sprintf("baris %d: %s", issues[0].Line, issues[0].Message)
		}
	}
	rev, err := c.store.AddRevision(Revision{
		SiteID:     site.ID,
		Path:       normalizeVirtual(virtual),
		Content:    content,
		Hash:       contentHash(content),
		Status:     status,
		Error:      errText,
		AuthorID:   user.ID,
		AuthorName: user.Username,
	})
	return rev, issues, err
}

// Publish melakukan validasi, penulisan atomik, pencatatan revisi, audit, dan
// reload engine. File aktif tidak pernah ditulis jika validasi gagal.
func (c *ConfigService) Publish(site Site, virtual, content string, user User, ip, action string) (PublishResult, []Issue, error) {
	cleanVirtual := normalizeVirtual(virtual)
	issues, valid := c.Validate(content)
	if !valid {
		c.store.AddAudit(AuditEvent{
			ActorID: user.ID, ActorName: user.Username, IP: ip,
			Action: action, Target: site.Slug + ":" + cleanVirtual,
			Result: "rejected", Meta: "validasi engine gagal",
		})
		return PublishResult{}, issues, errors.New("konfigurasi tidak valid")
	}
	written, err := WriteConfigAtomic(site.Docroot, cleanVirtual, []byte(content))
	if err != nil {
		c.store.AddAudit(AuditEvent{
			ActorID: user.ID, ActorName: user.Username, IP: ip,
			Action: action, Target: site.Slug + ":" + cleanVirtual,
			Result: "error", Meta: err.Error(),
		})
		return PublishResult{}, nil, err
	}
	rev, err := c.store.AddRevision(Revision{
		SiteID:     site.ID,
		Path:       written,
		Content:    content,
		Hash:       contentHash(content),
		Status:     "published",
		AuthorID:   user.ID,
		AuthorName: user.Username,
	})
	if err != nil {
		return PublishResult{}, nil, err
	}
	reloadStatus, reloadErr := c.reloader.Reload()
	result := PublishResult{Path: written, Revision: rev, Reload: reloadStatus}
	if reloadErr != nil {
		result.ReloadErr = reloadErr.Error()
	}
	c.store.AddAudit(AuditEvent{
		ActorID: user.ID, ActorName: user.Username, IP: ip,
		Action: action, Target: site.Slug + ":" + written,
		Result: "success",
		Meta:   "hash=" + rev.Hash + " reload=" + reloadStatus,
	})
	c.logger.Info("config dipublish", "site", site.Slug, "path", written, "reload", reloadStatus, "author", user.Username)
	return result, nil, nil
}

func (c *ConfigService) Rollback(site Site, revisionID string, user User, ip string) (PublishResult, []Issue, error) {
	rev, ok := c.store.GetRevision(revisionID)
	if !ok || rev.SiteID != site.ID {
		return PublishResult{}, nil, ErrNotFound
	}
	return c.Publish(site, rev.Path, rev.Content, user, ip, "config.rollback")
}

func normalizeVirtual(virtual string) string {
	if strings.TrimSpace(virtual) == "" {
		return "/"
	}
	if !strings.HasPrefix(virtual, "/") {
		return "/" + virtual
	}
	return virtual
}

// EngineReloader mengirim SIGHUP ke proses engine berdasarkan PID file yang
// ditentukan operator. Tidak ada shell dan tidak ada path dari input pengguna.
type EngineReloader struct {
	PIDFile string
	Logger  *slog.Logger
}

func (r *EngineReloader) Reload() (string, error) {
	if r == nil || r.PIDFile == "" {
		return "skipped", nil
	}
	raw, err := os.ReadFile(r.PIDFile)
	if err != nil {
		return "error", fmt.Errorf("baca pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return "error", errors.New("pid file tidak valid")
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return "error", fmt.Errorf("kirim SIGHUP: %w", err)
	}
	return "reloaded", nil
}
