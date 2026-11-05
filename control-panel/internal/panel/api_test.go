package panel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string  `json:"code"`
		Message string  `json:"message"`
		Details []Issue `json:"details"`
	} `json:"error"`
}

type testClient struct {
	t      *testing.T
	server *httptest.Server
	panel  *Panel
	http   *http.Client
	csrf   string
}

func newTestPanel(t *testing.T) *testClient {
	t.Helper()
	data := t.TempDir()
	store, err := OpenStore(filepath.Join(data, "state"))
	if err != nil {
		t.Fatal(err)
	}
	panel, err := NewPanel(store, filepath.Join(data, "www"), filepath.Join(data, "state"), "test", nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	panel.Register(mux)
	server := httptest.NewServer(panel.Middleware(mux))
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	return &testClient{t: t, server: server, panel: panel, http: &http.Client{Jar: jar}}
}

func (c *testClient) call(method, path string, body any, csrf string) (int, envelope) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, c.server.URL+"/api/v1"+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var env envelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			c.t.Fatalf("respons bukan JSON (%s %s): %s", method, path, raw)
		}
	}
	return response.StatusCode, env
}

func (c *testClient) setup() {
	c.t.Helper()
	status, env := c.call("GET", "/setup/status", nil, "")
	if status != 200 || !strings.Contains(string(env.Data), `"needs_setup":true`) {
		c.t.Fatalf("status setup salah: %d %s", status, env.Data)
	}
	if status, _ := c.call("POST", "/setup", map[string]any{
		"token": "salah", "username": "owner", "email": "owner@example.com", "password": "PanelAmanSekali-9",
	}, ""); status != http.StatusForbidden {
		c.t.Fatalf("token salah harus 403, dapat %d", status)
	}
	status, env = c.call("POST", "/setup", map[string]any{
		"token": c.panel.SetupToken(), "username": "owner",
		"email": "owner@example.com", "password": "PanelAmanSekali-9",
	}, "")
	if status != http.StatusCreated {
		c.t.Fatalf("setup gagal: %d %s", status, env.Data)
	}
	var payload struct {
		User userView `json:"user"`
		CSRF string   `json:"csrf"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		c.t.Fatal(err)
	}
	if payload.User.Username != "owner" || payload.CSRF == "" {
		c.t.Fatalf("payload setup tidak lengkap: %+v", payload)
	}
	if strings.Contains(string(env.Data), "password_hash") {
		c.t.Fatal("hash password bocor di respons")
	}
	c.csrf = payload.CSRF
}

func TestAPIFlow(t *testing.T) {
	c := newTestPanel(t)
	c.setup()

	status, _ := c.call("GET", "/sites", nil, "")
	if status != http.StatusOK {
		t.Fatalf("list situs harus 200, dapat %d", status)
	}

	// CSRF wajib untuk mutasi.
	if status, _ := c.call("POST", "/sites", map[string]any{
		"name": "Example", "slug": "example", "domain": "example.com",
	}, ""); status != http.StatusForbidden {
		t.Fatalf("POST tanpa CSRF harus 403, dapat %d", status)
	}

	status, env := c.call("POST", "/sites", map[string]any{
		"name": "Example", "slug": "example", "domain": "example.com",
	}, c.csrf)
	if status != http.StatusCreated {
		t.Fatalf("buat situs gagal: %d %s", status, env.Data)
	}
	var site Site
	if err := json.Unmarshal(env.Data, &site); err != nil {
		t.Fatal(err)
	}
	if site.ID == "" || site.Status != "active" {
		t.Fatalf("situs tidak lengkap: %+v", site)
	}
	if _, err := os.Stat(filepath.Join(site.Docroot, ".ginxaccess")); err != nil {
		t.Fatalf("config awal tidak dibuat: %v", err)
	}

	status, env = c.call("GET", "/sites/"+site.ID+"/config?path=/", nil, "")
	if status != 200 || !strings.Contains(string(env.Data), `"exists":true`) {
		t.Fatalf("baca config gagal: %d %s", status, env.Data)
	}

	// Path traversal ditolak sebelum menyentuh filesystem.
	if status, _ := c.call("GET", "/sites/"+site.ID+"/config?path=../etc", nil, ""); status != http.StatusBadRequest {
		t.Fatalf("traversal harus 400, dapat %d", status)
	}

	status, env = c.call("POST", "/sites/"+site.ID+"/config/validate", map[string]any{
		"path": "/", "content": "exec shell;\n",
	}, c.csrf)
	if status != 200 || !strings.Contains(string(env.Data), `"valid":false`) {
		t.Fatalf("validasi invalid salah: %d %s", status, env.Data)
	}

	validContent := "header set X-Test \"aktif\";\n"
	status, env = c.call("POST", "/sites/"+site.ID+"/config/publish", map[string]any{
		"path": "/", "content": validContent,
	}, c.csrf)
	if status != 200 {
		t.Fatalf("publish gagal: %d %s", status, env.Data)
	}
	written, err := os.ReadFile(filepath.Join(site.Docroot, ".ginxaccess"))
	if err != nil || string(written) != validContent {
		t.Fatalf("file tidak tertulis benar: %q %v", written, err)
	}

	status, env = c.call("POST", "/sites/"+site.ID+"/config/publish", map[string]any{
		"path": "/", "content": "exec shell;\n",
	}, c.csrf)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("publish invalid harus 422, dapat %d", status)
	}
	if env.Error == nil || env.Error.Code != "config_invalid" || len(env.Error.Details) == 0 {
		t.Fatalf("error validasi tidak terstruktur: %+v", env.Error)
	}
	written, _ = os.ReadFile(filepath.Join(site.Docroot, ".ginxaccess"))
	if string(written) != validContent {
		t.Fatal("file berubah setelah publish invalid")
	}

	status, env = c.call("GET", "/sites/"+site.ID+"/config/revisions?path=/", nil, "")
	if status != 200 || !strings.Contains(string(env.Data), `"status":"published"`) {
		t.Fatalf("revisi tidak tercatat: %d %s", status, env.Data)
	}

	// Logout mencabut sesi.
	if status, _ := c.call("POST", "/auth/logout", map[string]any{}, c.csrf); status != http.StatusNoContent {
		t.Fatalf("logout harus 204, dapat %d", status)
	}
	if status, _ := c.call("GET", "/sites", nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("setelah logout harus 401, dapat %d", status)
	}
	if status, _ := c.call("POST", "/auth/login", map[string]any{
		"username": "owner", "password": "salah-sekali",
	}, ""); status != http.StatusUnauthorized {
		t.Fatalf("password salah harus 401, dapat %d", status)
	}
	status, env = c.call("POST", "/auth/login", map[string]any{
		"username": "owner", "password": "PanelAmanSekali-9",
	}, "")
	if status != 200 {
		t.Fatalf("login gagal: %d %s", status, env.Data)
	}
	var login struct {
		CSRF string `json:"csrf"`
	}
	_ = json.Unmarshal(env.Data, &login)
	c.csrf = login.CSRF

	// Hapus situs memindahkan docroot ke trash.
	status, _ = c.call("DELETE", "/sites/"+site.ID, map[string]any{"confirm": "example"}, c.csrf)
	if status != http.StatusNoContent {
		t.Fatalf("hapus situs harus 204, dapat %d", status)
	}
	if _, err := os.Stat(site.Docroot); !os.IsNotExist(err) {
		t.Fatal("docroot seharusnya sudah dipindahkan")
	}
	trash, _ := filepath.Glob(filepath.Join(c.panel.WWWBase(), ".trash", "example-*"))
	if len(trash) != 1 {
		t.Fatalf("docroot tidak ada di trash: %v", trash)
	}
}

func TestUnauthenticatedAccess(t *testing.T) {
	c := newTestPanel(t)
	if status, _ := c.call("GET", "/sites", nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("harus 401 sebelum login, dapat %d", status)
	}
	if status, _ := c.call("GET", "/overview", nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("overview harus 401, dapat %d", status)
	}
}
