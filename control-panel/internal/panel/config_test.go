package panel

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func testUser(t *testing.T, store *Store) User {
	t.Helper()
	user := User{Username: "owner", Email: "owner@example.com", DisplayName: "Owner", Role: "owner", PasswordHash: "x"}
	if err := store.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	user, _ = store.FindUserByLogin("owner")
	return user
}

func testSite(t *testing.T, store *Store, dir string) Site {
	t.Helper()
	docroot := filepath.Join(dir, "www", "example", "public_html")
	if err := os.MkdirAll(docroot, 0o750); err != nil {
		t.Fatal(err)
	}
	site := Site{Slug: "example", Name: "Example", Domain: "example.com", Docroot: docroot, Status: "active"}
	if err := store.CreateSite(site); err != nil {
		t.Fatal(err)
	}
	site, _ = store.GetSite(site.ID)
	return site
}

func TestValidateMapsEngineLine(t *testing.T) {
	store, _ := testStore(t)
	service := NewConfigService(store, testLogger(), nil)
	issues, valid := service.Validate("allow all;\nexec shell;\n")
	if valid || len(issues) != 1 {
		t.Fatalf("harus invalid dengan satu isu: %+v", issues)
	}
	if issues[0].Line != 2 {
		t.Fatalf("baris salah: %+v", issues[0])
	}
	if !strings.Contains(issues[0].Message, "directive tidak dikenal") {
		t.Fatalf("pesan tidak informatif: %q", issues[0].Message)
	}
}

func TestPublishAtomicAndRollback(t *testing.T) {
	store, dir := testStore(t)
	user := testUser(t, store)
	site := testSite(t, store, dir)
	service := NewConfigService(store, testLogger(), nil)

	if issues, valid := service.Validate("header set X-A \"satu\";\n"); !valid {
		t.Fatalf("config awal harus valid: %+v", issues)
	}
	first, issues, err := service.Publish(site, "/", "header set X-A \"satu\";\n", user, "127.0.0.1", "config.publish")
	if err != nil || len(issues) != 0 {
		t.Fatalf("publish pertama gagal: %v %+v", err, issues)
	}
	raw, err := os.ReadFile(filepath.Join(site.Docroot, ".ginxaccess"))
	if err != nil || string(raw) != "header set X-A \"satu\";\n" {
		t.Fatalf("isi file salah: %q %v", raw, err)
	}

	second, _, err := service.Publish(site, "/", "header set X-A \"dua\";\n", user, "127.0.0.1", "config.publish")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision.ID == first.Revision.ID {
		t.Fatal("revisi harus baru")
	}

	if _, issues, err := service.Publish(site, "/", "exec shell;\n", user, "127.0.0.1", "config.publish"); err == nil {
		t.Fatal("config invalid harus ditolak")
	} else if len(issues) == 0 || issues[0].Line != 1 {
		t.Fatalf("isu validasi tidak dipetakan: %+v", issues)
	}
	raw, _ = os.ReadFile(filepath.Join(site.Docroot, ".ginxaccess"))
	if string(raw) != "header set X-A \"dua\";\n" {
		t.Fatalf("file aktif berubah setelah publish invalid: %q", raw)
	}

	result, issues, err := service.Rollback(site, first.Revision.ID, user, "127.0.0.1")
	if err != nil || len(issues) != 0 {
		t.Fatalf("rollback gagal: %v %+v", err, issues)
	}
	raw, _ = os.ReadFile(filepath.Join(site.Docroot, ".ginxaccess"))
	if string(raw) != "header set X-A \"satu\";\n" {
		t.Fatalf("rollback tidak memulihkan byte: %q", raw)
	}
	if result.Revision.Status != "published" {
		t.Fatalf("revisi rollback harus published: %+v", result.Revision)
	}
}

func TestRevisionsPersistContent(t *testing.T) {
	store, dir := testStore(t)
	user := testUser(t, store)
	site := testSite(t, store, dir)
	service := NewConfigService(store, testLogger(), nil)
	content := "allow 192.0.2.0/24;\ndeny all;\n"
	published, _, err := service.Publish(site, "/", content, user, "127.0.0.1", "config.publish")
	if err != nil {
		t.Fatal(err)
	}

	// Buka ulang store untuk memastikan konten revisi benar-benar tersimpan.
	reopened, err := OpenStore(store.Dir())
	if err != nil {
		t.Fatal(err)
	}
	rev, ok := reopened.GetRevision(published.Revision.ID)
	if !ok || rev.Content != content {
		t.Fatalf("konten revisi hilang setelah restart: %+v", rev)
	}
}

func TestListConfigPaths(t *testing.T) {
	_, dir := testStore(t)
	docroot := filepath.Join(dir, "www")
	if err := os.MkdirAll(filepath.Join(docroot, "admin"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, ".ginxaccess"), []byte("allow all;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "admin", ".ginxaccess"), []byte("deny all;"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := ListConfigPaths(docroot)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/" || paths[1] != "/admin" {
		t.Fatalf("daftar config salah: %v", paths)
	}
}
