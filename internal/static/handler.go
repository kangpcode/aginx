// Package static menerapkan ACL pada sumber DAN tujuan setiap internal rewrite.
package static

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"aginx/internal/fsroot"
	"aginx/internal/ginxaccess"
	"aginx/internal/http1"
)

type Response struct {
	Status int
	Header http.Header
	File   *os.File
	Body   string
	Size   int64
}

func Error(status int) *Response {
	body := fmt.Sprintf("%d %s\n", status, http.StatusText(status))
	return &Response{Status: status, Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: body, Size: int64(len(body))}
}

type Handler struct {
	Root   *fsroot.Root
	Cache  *ginxaccess.Cache
	Access bool
}

// CleanPath memeriksa bentuk decoded sebelum normalisasi agar '..' tidak hilang diam-diam.
func CleanPath(value string) (string, error) {
	if len(value) > 8192 || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\x00") {
		return "", os.ErrPermission
	}
	parts := strings.Split(value, "/")
	if len(parts) > 34 {
		return "", os.ErrPermission
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return "", os.ErrPermission
		}
		for _, c := range part {
			if c < 32 || c == 127 {
				return "", os.ErrPermission
			}
		}
	}
	return path.Clean(value), nil
}

func dirs(name string) []string {
	result := []string{""}
	if name == "/" {
		return result
	}
	current := ""
	for _, part := range strings.Split(strings.Trim(name, "/"), "/") {
		current = path.Join(current, part)
		result = append(result, current)
	}
	return result
}

func fileError(err error) *Response {
	if errors.Is(err, os.ErrNotExist) {
		return Error(404)
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return Error(403)
	}
	return Error(503)
}

func (h *Handler) Handle(req *http1.Request, ip netip.Addr) *Response {
	if req.Method != "GET" && req.Method != "HEAD" {
		resp := Error(405)
		resp.Header.Set("Allow", "GET, HEAD")
		return resp
	}
	u := *req.URL
	if len(u.RawQuery) > 8192 {
		return Error(414)
	}
	headers := make(http.Header)
	seen := make(map[string]bool)
	budget := 4096
	evaluationTime := time.Duration(0)
	for hop := 0; hop <= 8; hop++ {
		name, err := CleanPath(u.Path)
		if err != nil {
			return Error(403)
		}
		// Hindari membocorkan source aplikasi sebelum handler FastCGI/CGI tersedia.
		extension := strings.ToLower(path.Ext(name))
		if extension == ".php" || extension == ".phtml" || extension == ".phar" || extension == ".cgi" || extension == ".fcgi" {
			return Error(403)
		}
		key := name + "?" + u.RawQuery
		if seen[key] {
			return Error(508)
		}
		seen[key] = true
		var layers []*ginxaccess.Config
		// Hanya parent target; direktori target ditambahkan setelah fstat.
		parents := dirs(path.Dir(name))
		if name == "/" {
			parents = dirs("/")
		}
		if h.Access {
			for _, dir := range parents {
				cfg, err := h.Cache.Get(path.Join(dir, ".ginxaccess"))
				if err != nil {
					return Error(503)
				}
				if cfg != nil {
					layers = append(layers, cfg)
				}
			}
		}
		for _, cfg := range layers {
			if !cfg.Allowed(ip) {
				return Error(403)
			}
		}
		f, openErr := h.Root.Open(strings.TrimPrefix(name, "/"))
		var info os.FileInfo
		if openErr == nil {
			info, openErr = f.Stat()
			if openErr != nil {
				f.Close()
			}
		}
		if openErr == nil && info.IsDir() && name != "/" && h.Access {
			cfg, e := h.Cache.Get(path.Join(strings.TrimPrefix(name, "/"), ".ginxaccess"))
			if e != nil {
				f.Close()
				return Error(503)
			}
			if cfg != nil {
				layers = append(layers, cfg)
				if !cfg.Allowed(ip) {
					f.Close()
					return Error(403)
				}
			}
		}
		var target string
		for _, cfg := range layers {
			for _, rule := range cfg.Headers {
				headers.Set(rule.Name, rule.Value)
			}
			for _, rule := range cfg.Rewrites {
				budget--
				if budget < 0 || evaluationTime >= 20*time.Millisecond {
					if openErr == nil {
						f.Close()
					}
					return Error(503)
				}
				started := time.Now()
				replaced, matched, e := rule.Apply(name)
				evaluationTime += time.Since(started)
				if evaluationTime >= 20*time.Millisecond {
					if openErr == nil {
						f.Close()
					}
					return Error(503)
				}
				if e != nil {
					if openErr == nil {
						f.Close()
					}
					return Error(508)
				}
				if matched {
					target = replaced
					break
				}
			}
			if target != "" {
				break
			}
		}
		if target != "" {
			if openErr == nil {
				f.Close()
			}
			if hop == 8 || len(target) > 8192 {
				return Error(508)
			}
			parsed, e := url.ParseRequestURI(target)
			if e != nil || parsed.Host != "" || parsed.Scheme != "" || strings.Contains(target, "#") {
				return Error(403)
			}
			if !strings.Contains(target, "?") {
				parsed.RawQuery = u.RawQuery
			}
			u = *parsed
			continue
		}
		if openErr != nil {
			return fileError(openErr)
		}
		if info.IsDir() {
			f.Close()
			// Index menjadi internal routing pass sehingga ACL/rewrite target tetap berlaku.
			if hop == 8 {
				return Error(508)
			}
			u.Path = path.Join(name, "index.html")
			u.RawPath = ""
			continue
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return Error(403)
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", contentType)
		}
		headers.Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
		return &Response{Status: 200, Header: headers, File: f, Size: info.Size()}
	}
	return Error(508)
}
