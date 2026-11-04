package ginxaccess

import (
	"net/netip"
	"strings"
	"testing"
)

func TestParseAndACL(t *testing.T) {
	cfg, err := Parse([]byte("# comment\nallow 192.0.2.0/24;\ndeny all;\nheader set X-Test \"dua kata\"; # komentar\nrewrite \"^/lama$\" \"/baru\" last;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Allowed(netip.MustParseAddr("192.0.2.12")) || cfg.Allowed(netip.MustParseAddr("198.51.100.1")) {
		t.Fatal("ACL first-match gagal")
	}
	if cfg.Headers[0].Value != "dua kata" || !cfg.Rewrites[0].Pattern.MatchString("/lama") {
		t.Fatal("hasil parse salah")
	}
	if cfg.Allowed(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("IPv6 harus ditolak")
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, input := range []string{
		"exec shell;", "allow not-an-ip;", "header set Content-Length 20;", "header set X-Test \"a\\r\\nb\";",
		"header set X-Test value", "header set X-Test \"unterminated;", "rewrite \"(\" /x last;", "rewrite x /x unknown;",
		"allow all; extra", "allow all; deny all;", strings.Repeat("allow all;\n", MaxRules+1), strings.Repeat("x", MaxFileBytes+1),
	} {
		if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "baris") {
			t.Fatalf("harus ada error bernomor baris: %q -> %v", input, err)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "allow all;", "header set X-Test \"hello\";", "rewrite \"^/a$\" \"/b\" last;", "header set X \"\\\"\";", "\xff\x00"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxFileBytes+1 {
			t.Skip()
		}
		cfg, err := Parse(data)
		if err == nil && cfg == nil {
			t.Fatal("snapshot nil tanpa error")
		}
	})
}

func TestRewriteCaptureBound(t *testing.T) {
	cfg, err := Parse([]byte("rewrite \"^/old/(.*)$\" \"/new/${1}\" last;"))
	if err != nil {
		t.Fatal(err)
	}
	value, match, err := cfg.Rewrites[0].Apply("/old/file.css")
	if err != nil || !match || value != "/new/file.css" {
		t.Fatalf("capture: %q %v %v", value, match, err)
	}
	rule := RewriteRule{Pattern: cfg.Rewrites[0].Pattern, Target: "/" + strings.Repeat("${1}", 100)}
	if _, _, err := rule.Apply("/old/" + strings.Repeat("a", 1000)); err == nil {
		t.Fatal("ekspansi harus dibatasi")
	}
}
