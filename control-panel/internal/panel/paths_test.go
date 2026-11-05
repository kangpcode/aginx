package panel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPathRejectsTraversal(t *testing.T) {
	docroot := t.TempDir()
	for _, input := range []string{"../etc", "/../etc", "/a/../../b", "/..", ".."} {
		if _, _, err := ResolveConfigPath(docroot, input); err == nil {
			t.Fatalf("path %q harus ditolak", input)
		}
	}
}

func TestResolveConfigPathRejectsSymlink(t *testing.T) {
	docroot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(docroot, "link")); err != nil {
		t.Skipf("symlink tidak didukung: %v", err)
	}
	if _, _, err := ResolveConfigPath(docroot, "/link"); err == nil {
		t.Fatal("symlink keluar docroot harus ditolak")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	docroot := t.TempDir()
	if err := os.Mkdir(filepath.Join(docroot, "admin"), 0o750); err != nil {
		t.Fatal(err)
	}
	content := []byte("allow all;\n")
	if _, err := WriteConfigAtomic(docroot, "/admin", content); err != nil {
		t.Fatal(err)
	}
	got, exists, _, err := ReadConfig(docroot, "/admin/")
	if err != nil || !exists || got != string(content) {
		t.Fatalf("round trip gagal: %q %v %v", got, exists, err)
	}
	if _, err := WriteConfigAtomic(docroot, "/missing", content); err == nil {
		t.Fatal("direktori yang belum ada harus ditolak")
	}
}

func TestValidateSlugAndDomain(t *testing.T) {
	for _, slug := range []string{"example", "my-site", "a1"} {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("slug %q harus valid: %v", slug, err)
		}
	}
	for _, slug := range []string{"a", "-bad", "Bad", "www", "admin", "with space"} {
		if err := ValidateSlug(slug); err == nil {
			t.Fatalf("slug %q harus ditolak", slug)
		}
	}
	if err := ValidateDomain("example.com"); err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"localhost", "https://example.com", "example", "exa mple.com"} {
		if err := ValidateDomain(domain); err == nil {
			t.Fatalf("domain %q harus ditolak", domain)
		}
	}
}
