package static

import (
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"aginx/internal/fsroot"
	"aginx/internal/ginxaccess"
	"aginx/internal/http1"
)

func setup(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := fsroot.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	cache := ginxaccess.NewCache(root, time.Hour, 64, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &Handler{root, cache, true}, dir
}

func put(t *testing.T, dir, name, value string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func request(t *testing.T, h *Handler, target string) *Response {
	t.Helper()
	u, err := url.ParseRequestURI(target)
	if err != nil {
		t.Fatal(err)
	}
	resp := h.Handle(&http1.Request{Method: "GET", URL: u}, netip.MustParseAddr("127.0.0.1"))
	if resp.File != nil {
		t.Cleanup(func() { resp.File.Close() })
	}
	return resp
}

func TestInheritanceRewriteAndProtectedTarget(t *testing.T) {
	h, dir := setup(t)
	put(t, dir, "index.html", "index")
	put(t, dir, ".ginxaccess", "header set X-Test parent;\nrewrite \"^/old$\" \"/index.html\" last;\nrewrite \"^/leak$\" \"/admin/secret.html\" last;")
	put(t, dir, "admin/secret.html", "SECRET")
	put(t, dir, "admin/.ginxaccess", "deny all;\nheader set X-Test child;")
	if resp := request(t, h, "/old"); resp.Status != 200 || resp.Header.Get("X-Test") != "parent" {
		t.Fatalf("rewrite: %+v", resp)
	}
	for _, target := range []string{"/leak", "/admin/secret.html", "/admin", "/admin/"} {
		if resp := request(t, h, target); resp.Status != 403 {
			t.Fatalf("ACL bypass: %s = %d", target, resp.Status)
		}
	}
	put(t, dir, "admin/.ginxaccess", "allow all;\nheader set X-Test child;")
	h.Cache.Invalidate()
	if resp := request(t, h, "/admin/secret.html"); resp.Status != 200 || resp.Header.Get("X-Test") != "child" {
		t.Fatalf("child: %+v", resp)
	}
	put(t, dir, ".ginxaccess", "deny all;")
	h.Cache.Invalidate()
	if resp := request(t, h, "/admin/secret.html"); resp.Status != 403 {
		t.Fatal("child memperlemah ACL parent")
	}
}

func TestTraversalSymlinkAndSpecialFiles(t *testing.T) {
	h, dir := setup(t)
	put(t, dir, "index.html", "safe")
	put(t, dir, "app.PHP", "<?php secret();")
	outside := t.TempDir()
	put(t, outside, "secret", "SECRET")
	if err := os.Symlink(outside, filepath.Join(dir, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("index.html", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/../secret", "/%2e%2e/secret", "/x/%2e%2e/index.html", "/.ginxaccess", "/%00", "/a%5cb", "/alias", "/outside/secret", "/pipe", "/app.PHP"} {
		if resp := request(t, h, target); resp.Status == 200 {
			t.Fatalf("akses tidak aman: %s", target)
		}
	}
	if resp := request(t, h, "/"); resp.Status != 200 {
		t.Fatalf("directory index: %d", resp.Status)
	}
	if _, err := CleanPath("/" + strings.Repeat("a/", 40)); err == nil {
		t.Fatal("depth limit gagal")
	}
}

func TestRewriteCycleAndFirstBadConfig(t *testing.T) {
	h, dir := setup(t)
	put(t, dir, ".ginxaccess", "rewrite \"^/a$\" \"/b\" last;\nrewrite \"^/b$\" \"/a\" last;")
	if resp := request(t, h, "/a"); resp.Status != 508 {
		t.Fatalf("cycle: %d", resp.Status)
	}
	h2, dir2 := setup(t)
	put(t, dir2, "index.html", "safe")
	put(t, dir2, ".ginxaccess", "invalid;")
	if resp := request(t, h2, "/index.html"); resp.Status != 503 {
		t.Fatal("konfigurasi rusak harus fail closed")
	}
}
