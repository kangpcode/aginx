// Package ui menyediakan aset panel yang di-embed ke binary.
package ui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var embedded embed.FS

// Assets mengembalikan subtree aset statis.
func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// Index mengirim shell SPA. Semua route non-API memakai file yang sama dan
// router sisi klien yang menentukan halaman.
func Index(w http.ResponseWriter, r *http.Request) {
	f, err := Assets().Open("index.html")
	if err != nil {
		http.Error(w, "aset UI tidak ditemukan", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}

// Handler menangani aset dan fallback SPA tanpa membocorkan index untuk /api.
func Handler() http.Handler {
	files := http.StripPrefix("/assets/", http.FileServerFS(Assets()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Endpoint tidak ditemukan"}}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			files.ServeHTTP(w, r)
			return
		}
		Index(w, r)
	})
}
