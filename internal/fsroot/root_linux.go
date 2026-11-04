// Package fsroot membatasi akses file pada docroot dengan descriptor relatif.
package fsroot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Root harus tetap terbuka sampai seluruh request selesai.
type Root struct {
	Path string
	file *os.File
}

func New(name string) (*Root, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		f.Close()
		return nil, fmt.Errorf("docroot bukan direktori: %s", abs)
	}
	return &Root{Path: abs, file: f}, nil
}

func (r *Root) Close() error { return r.file.Close() }

// Open menolak symlink pada SEMUA komponen agar alias tidak melewati ACL direktori.
// O_NONBLOCK mencegah FIFO menahan goroutine sebelum tipe file diperiksa.
func (r *Root) Open(name string) (*os.File, error) {
	if name == "." {
		name = ""
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return nil, os.ErrPermission
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "." || part == ".." || (part == "" && name != "") {
			return nil, os.ErrPermission
		}
	}
	// Prefix check sebagai validasi tambahan; openat adalah batas akses sebenarnya.
	full := filepath.Join(r.Path, name)
	rel, err := filepath.Rel(r.Path, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, os.ErrPermission
	}
	parent := int(r.file.Fd())
	owned := false
	for i, part := range parts {
		if part == "" {
			part = "."
		}
		flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= syscall.O_DIRECTORY
		}
		var fd int
		for {
			fd, err = syscall.Openat(parent, part, flags, 0)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		if owned {
			syscall.Close(parent)
		}
		if err != nil {
			return nil, &os.PathError{Op: "openat", Path: name, Err: err}
		}
		parent, owned = fd, true
	}
	return os.NewFile(uintptr(parent), full), nil
}
