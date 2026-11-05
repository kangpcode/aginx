package panel

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const maxConfigBytes = 64 << 10

// DocrootFor menghasilkan docroot default yang terkurung di bawah base.
// Base sudah dikanonikalisasi saat startup.
func DocrootFor(base, slug string) string {
	return filepath.Join(base, slug, "public_html")
}

// ResolveConfigPath memetakan path direktori virtual ("/", "/admin") ke file
// .ginxaccess di dalam docroot. Semua komponen diperiksa agar tidak ada
// symlink yang keluar dari docroot.
func ResolveConfigPath(docroot, virtual string) (target, clean string, err error) {
	if strings.ContainsRune(virtual, '\x00') || strings.Contains(virtual, "\\") {
		return "", "", errors.New("path mengandung karakter tidak valid")
	}
	if virtual == "" {
		virtual = "/"
	}
	if !strings.HasPrefix(virtual, "/") {
		virtual = "/" + virtual
	}
	clean = path.Clean(virtual)
	if clean == "." {
		clean = "/"
	}
	rel := strings.TrimPrefix(clean, "/")
	dir := docroot
	if rel != "" {
		dir = filepath.Join(docroot, filepath.FromSlash(rel))
	}
	contained, err := filepath.Rel(docroot, dir)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("path keluar dari docroot")
	}
	if err := ensureNoSymlink(docroot, contained); err != nil {
		return "", "", err
	}
	target = filepath.Join(dir, ".ginxaccess")
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("file konfigurasi adalah symlink")
	}
	return target, clean, nil
}

// ensureNoSymlink memeriksa setiap komponen yang sudah ada dari docroot ke
// bawah. Komponen yang belum ada diizinkan; pemanggil tetap wajib memastikan
// direktori induk ada sebelum menulis.
func ensureNoSymlink(docroot, rel string) error {
	if rel == "." || rel == "" {
		return nil
	}
	current := docroot
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("komponen path %q adalah symlink", part)
		}
	}
	return nil
}

// ReadConfig membaca .ginxaccess pada path virtual. exists=false berarti file
// belum ada, bukan error.
func ReadConfig(docroot, virtual string) (content string, exists bool, modTime int64, err error) {
	target, _, err := ResolveConfigPath(docroot, virtual)
	if err != nil {
		return "", false, 0, err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, 0, nil
	}
	if err != nil {
		return "", false, 0, err
	}
	if !info.Mode().IsRegular() {
		return "", false, 0, errors.New("konfigurasi bukan regular file")
	}
	if info.Size() > maxConfigBytes {
		return "", false, 0, fmt.Errorf("konfigurasi melebihi %d byte", maxConfigBytes)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return "", false, 0, err
	}
	return string(raw), true, info.ModTime().Unix(), nil
}

// WriteConfigAtomic menulis konfigurasi dengan pola temp -> fsync -> rename ->
// fsync direktori. Penulisan parsial tidak pernah terlihat oleh engine.
func WriteConfigAtomic(docroot, virtual string, content []byte) (string, error) {
	if len(content) > maxConfigBytes {
		return "", fmt.Errorf("konfigurasi melebihi %d byte", maxConfigBytes)
	}
	target, clean, err := ResolveConfigPath(docroot, virtual)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(target)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("direktori target tidak ada: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("target bukan direktori")
	}
	tmp, err := os.CreateTemp(dir, ".ginxaccess.tmp-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(name, target); err != nil {
		return "", err
	}
	if err := syncDir(dir); err != nil {
		return "", err
	}
	return clean, nil
}

// ListConfigPaths mengembalikan direktori (virtual) yang memiliki .ginxaccess.
func ListConfigPaths(docroot string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(docroot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(docroot, p)
		if err != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator)) + 1
		}
		if depth > 8 || d.Name() == ".aginx-trash" {
			return filepath.SkipDir
		}
		if rel == "." {
			out = append(out, "/")
			return nil
		}
		// Hanya laporkan direktori yang benar-benar punya file konfigurasi.
		if info, err := os.Lstat(filepath.Join(p, ".ginxaccess")); err == nil && info.Mode().IsRegular() {
			out = append(out, "/"+filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func ensureInside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
