# Tugas 4 — POC siap kompilasi

Kode lengkap berada pada source tree dan disalin ke appendix dokumen ini dengan nama file di atas setiap code block. Target Linux; semua komentar implementasi berbahasa Indonesia. Tidak ada dependency pihak ketiga. `go.mod` menyatakan Go 1.24; gunakan patch terbaru dari cabang yang masih didukung untuk penggunaan di luar eksperimen.

```bash
make build
make test
make vet
make race
make fuzz
make smoke
./bin/aginx -t -config examples/www/.ginxaccess
./bin/aginx -listen 127.0.0.1:8080 -root examples/www
```

Build mengaktifkan `CGO_ENABLED=0`; `-buildvcs=false` memungkinkan source archive tanpa metadata Git. Verifikasi static linkage:

```bash
file bin/aginx
readelf -l bin/aginx
readelf -d bin/aginx
```

Tidak boleh ada program header `INTERP` atau dynamic dependency `NEEDED`. Binary race test menggunakan CGO; binary distribusi tetap dibangun terpisah tanpa CGO.

`make smoke` memakai Python 3 standard library dan TCP loopback untuk menguji binary secara langsung: CLI validator, GET/HEAD/keep-alive, ACL dan rewrite, update tanpa restart, config rusak/last-known-good, SIGHUP, framing invalid, serta SIGTERM selama transfer 10 MiB. Docroot dan log dibuat pada direktori sementara. Untuk merekam bukti syscall, jalankan pada host yang mengizinkan socket dan tracing:

```bash
strace -f -e trace=sendfile -o /tmp/aginx-sendfile-smoke.log python3 scripts/live-smoke.py
rg 'sendfile' /tmp/aginx-sendfile-smoke.log
```

Keberhasilan checksum bersama trace syscall memverifikasi jalur plaintext file-to-TCP, termasuk partial transfer/EAGAIN bila teramati. Ini tidak mengukur throughput karena tracing menambah overhead.

```bash
curl -i http://127.0.0.1:8080/
curl -I http://127.0.0.1:8080/index.html
curl -i http://127.0.0.1:8080/welcome
curl --path-as-is -i http://127.0.0.1:8080/%2e%2e/etc/passwd
wrk -t4 -c128 -d30s --latency http://127.0.0.1:8080/index.html
```

Demonstrasi perubahan tanpa restart pada shell kedua; ini mengubah file contoh di working copy:

```bash
cat > examples/www/.ginxaccess.next <<'EOF'
header set X-Powered-By "AGinX-updated";
rewrite "^/welcome$" "/index.html" last;
EOF
./bin/aginx -t -config examples/www/.ginxaccess.next
mv examples/www/.ginxaccess.next examples/www/.ginxaccess
sleep 0.2
curl -I http://127.0.0.1:8080/index.html
```

TTL divalidasi saat request masuk, bukan background timer. Batas staleness adalah interval TTL ditambah waktu I/O/scheduling untuk request yang menemukan entry expired. SIGHUP hanya memaksa metadata revalidation, tidak memuat global config/listener baru. SIGTERM/SIGINT menutup listener dan idle connection lalu drain response aktif; sesudah 10 s socket sisanya ditutup. Kernel I/O yang macet tetap dapat menahan goroutine, sehingga production membutuhkan pembatasan disk I/O dan supervisor dengan deadline proses.

Untuk sinyal, ambil PID ketika memulai server dari shell yang sama:

```bash
./bin/aginx -root examples/www &
aginx_pid=$!
kill -HUP "$aginx_pid"
kill -TERM "$aginx_pid"
wait "$aginx_pid"
```

Implementasi parser menerima HTTP/1.1 dan 1.0 origin-form. HTTP/1.1 memerlukan satu Host. CRLF wajib, obs-fold dan whitespace sebelum colon ditolak. Duplicate Content-Length, CL+TE, setiap Transfer-Encoding, dan request body nonkosong ditolak lalu koneksi ditutup. Body/chunked dapat ditambahkan dengan state machine framing sebelum proxy/FastCGI, bukan dengan mengabaikan byte sisa. POC menulis response Content-Length, mengimplementasikan HEAD tanpa body, mengurutkan response pipelining, dan menutup koneksi setelah maksimal 1000 request. [Aturan framing HTTP/1.1](https://www.rfc-editor.org/rfc/rfc9112.html).

Header deadline default 5 s, idle 30 s, write 30 s; max header termasuk request line 16 KiB dan 100 fields, koneksi default 10K. Byte pertama pada keep-alive memakai idle timeout lalu header memakai absolute deadline. Partial write header/body ditangani loop; file memakai `io.CopyN` dengan descriptor terpisah per request. Runtime netpoll/sendfile menangani EAGAIN, EINTR dan partial transfer tanpa busy-loop. [Implementasi Go](https://go.dev/src/internal/poll/sendfile_unix.go).

Path diperiksa sesudah URL decode, sebelum canonicalize, dengan penolakan dot components/hidden files, backslash, NUL, control dan kedalaman berlebih. Prefix docroot diperiksa lagi, lalu `openat` berantai dengan `O_NOFOLLOW` pada setiap komponen menjaga symlink tidak dapat dialihkan setelah pemeriksaan string. FIFO dibuka dengan O_NONBLOCK lalu ditolak karena bukan regular file; file special lain juga ditolak. Docroot wajib dikelola publisher terpercaya: tidak ada jaminan terhadap admin yang memasang bind mount, hardlink rahasia, atau menulis isi file langsung saat sedang dikirim. Atomic rename memberi snapshot descriptor file yang konsisten untuk pembaruan content.

`-access=false` hanya digunakan untuk benchmark baseline pada docroot tanpa policy. Menonaktifkannya pada docroot yang membutuhkan ACL akan mematikan pemeriksaan ACL. Jangan menganggap full language di `examples/spec` dapat dijalankan oleh parser POC.

POC belum mencakup master-worker, SO_REUSEPORT, inotify, TLS, HTTP/2/3, auth, Range/ETag/304, chunked request, gzip/Brotli, configurable index, maupun FastCGI. Directory listing selalu off; index fixed `index.html`. Header config diterapkan pada response static sukses; integrasi header pada semua error response adalah pekerjaan engine lanjutan. Error config pertama menghasilkan 503, tidak crash. Paket ini belum merupakan release production.

## Appendix kode lengkap

Bagian berikut dihasilkan dari file implementasi dengan `python3 scripts/render-source.py`.

<!-- GENERATED SOURCE -->

`go.mod`

```text
module aginx

go 1.24.0
```

`Makefile`

```makefile
GO ?= go
export GOCACHE ?= /tmp/aginx-go-cache
export GOMODCACHE ?= /tmp/aginx-go-modcache

.PHONY: build test race vet check fuzz bench smoke run
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags='-s -w' -o bin/aginx ./cmd/aginx
test:
	$(GO) test ./...
race:
	CGO_ENABLED=1 $(GO) test -race ./...
vet:
	$(GO) vet ./...
check: vet test race build
fuzz:
	$(GO) test ./internal/http1 -run='^$$' -fuzz=FuzzRead -fuzztime=10s -parallel=2
	$(GO) test ./internal/ginxaccess -run='^$$' -fuzz=FuzzParse -fuzztime=10s -parallel=2
bench:
	$(GO) test ./internal/ginxaccess -run='^$$' -bench=. -benchmem
smoke: build
	python3 scripts/live-smoke.py
run: build
	./bin/aginx -root ./examples/www
```

`cmd/aginx/main.go`

```go
// AGinX adalah static server mandiri; tidak memerlukan Nginx maupun Apache.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aginx/internal/fsroot"
	"aginx/internal/ginxaccess"
	"aginx/internal/server"
	"aginx/internal/static"
)

func main() {
	if err := run(); err != nil {
		slog.Error("aginx berhenti", "error", err)
		os.Exit(1)
	}
}

func run() error {
	options := server.DefaultOptions()
	listen := flag.String("listen", "127.0.0.1:8080", "alamat listen TCP")
	docroot := flag.String("root", "./examples/www", "document root")
	ttl := flag.Duration("config-ttl", 100*time.Millisecond, "interval validasi metadata .ginxaccess")
	capacity := flag.Int("config-cache", 1024, "maksimum entry cache termasuk file tidak ada")
	access := flag.Bool("access", true, "aktifkan .ginxaccess (false hanya baseline benchmark)")
	check := flag.Bool("t", false, "validasi satu file .ginxaccess lalu keluar")
	config := flag.String("config", ".ginxaccess", "file untuk -t")
	flag.DurationVar(&options.HeaderTimeout, "header-timeout", options.HeaderTimeout, "batas absolut membaca header")
	flag.DurationVar(&options.IdleTimeout, "idle-timeout", options.IdleTimeout, "batas idle keep-alive")
	flag.DurationVar(&options.WriteTimeout, "write-timeout", options.WriteTimeout, "batas menulis satu response")
	flag.IntVar(&options.MaxConnections, "max-connections", options.MaxConnections, "batas koneksi konkuren")
	flag.IntVar(&options.MaxHeaderBytes, "max-header", options.MaxHeaderBytes, "batas request line + header, byte")
	flag.Parse()
	if *check {
		parent, err := fsroot.New(filepath.Dir(*config))
		if err != nil {
			return err
		}
		defer parent.Close()
		cache := ginxaccess.NewCache(parent, time.Millisecond, 1, slog.Default())
		cfg, err := cache.Get(filepath.Base(*config))
		if err != nil {
			return err
		}
		if cfg == nil {
			return fmt.Errorf("file konfigurasi tidak ada: %s", *config)
		}
		fmt.Println("konfigurasi valid (subset POC)")
		return nil
	}
	if *ttl <= 0 || *capacity < 1 || options.MaxConnections < 1 || options.MaxHeaderBytes < 1024 || options.MaxHeaderBytes > 1<<20 || options.HeaderTimeout <= 0 || options.IdleTimeout <= 0 || options.WriteTimeout <= 0 {
		return fmt.Errorf("batas/timeout tidak valid")
	}
	root, err := fsroot.New(*docroot)
	if err != nil {
		return err
	}
	defer root.Close()
	cache := ginxaccess.NewCache(root, *ttl, *capacity, slog.Default())
	handler := &static.Handler{Root: root, Cache: cache, Access: *access}
	srv := server.New(handler, options, slog.Default())
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	slog.Info("AGinX siap", "listen", listener.Addr().String(), "root", root.Path, "config_ttl", ttl.String())
	for {
		select {
		case sig := <-signals:
			if sig == syscall.SIGHUP {
				cache.Invalidate()
				slog.Info("SIGHUP: validasi ulang konfigurasi tanpa putus koneksi")
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := srv.Shutdown(ctx)
			cancel()
			return err
		case err := <-served:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			shutdownErr := srv.Shutdown(ctx)
			cancel()
			if err != nil {
				return err
			}
			return shutdownErr
		}
	}
}
```

`internal/fsroot/root_linux.go`

```go
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
```

`internal/ginxaccess/cache.go`

```go
package ginxaccess

import (
	"container/list"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"

	"aginx/internal/fsroot"
)

type fingerprint struct {
	Inode, Device      uint64
	Size, MTime, CTime int64
}

func identity(info os.FileInfo) fingerprint {
	s := info.Sys().(*syscall.Stat_t)
	return fingerprint{s.Ino, uint64(s.Dev), info.Size(), info.ModTime().UnixNano(), s.Ctim.Sec*1e9 + s.Ctim.Nsec}
}

type entry struct {
	mu         sync.Mutex
	path       string
	version    fingerprint
	seen       bool
	cfg        *Config
	err        error
	next       time.Time
	generation uint64
}

type Cache struct {
	root       *fsroot.Root
	ttl        time.Duration
	capacity   int
	logger     *slog.Logger
	mu         sync.Mutex
	items      map[string]*list.Element
	lru        *list.List
	generation uint64
}

func NewCache(root *fsroot.Root, ttl time.Duration, capacity int, logger *slog.Logger) *Cache {
	if ttl <= 0 {
		ttl = 100 * time.Millisecond
	}
	if capacity < 1 {
		capacity = 1024
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{root: root, ttl: ttl, capacity: capacity, logger: logger, items: make(map[string]*list.Element), lru: list.New()}
}

// Invalidate mempertahankan versi valid terakhir, termasuk pada SIGHUP.
func (c *Cache) Invalidate() { c.mu.Lock(); c.generation++; c.mu.Unlock() }

func (c *Cache) Get(path string) (*Config, error) {
	c.mu.Lock()
	elem, ok := c.items[path]
	if !ok {
		if c.lru.Len() >= c.capacity {
			oldest := c.lru.Back()
			delete(c.items, oldest.Value.(*entry).path)
			c.lru.Remove(oldest)
		}
		elem = c.lru.PushFront(&entry{path: path})
		c.items[path] = elem
	} else {
		c.lru.MoveToFront(elem)
	}
	e := elem.Value.(*entry)
	generation := c.generation
	c.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if time.Now().Before(e.next) && e.generation == generation {
		return e.cfg, e.err
	}
	e.generation, e.next = generation, time.Now().Add(c.ttl)
	f, err := c.root.Open(path)
	if os.IsNotExist(err) {
		e.cfg, e.err, e.seen = nil, nil, false
		return nil, nil
	}
	if err != nil {
		return c.failed(e, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return c.failed(e, err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return c.failed(e, fmt.Errorf("konfigurasi wajib regular file <= 64 KiB"))
	}
	version := identity(info)
	if e.seen && version == e.version {
		return e.cfg, e.err
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return c.failed(e, err)
	}
	after, err := f.Stat()
	if err != nil {
		return c.failed(e, err)
	}
	afterVersion := identity(after)
	// Atomic rename dapat meng-unlink inode lama dan mengubah ctime descriptor
	// yang sedang dibaca tanpa mengubah isinya. Tetap gunakan ctime pada key
	// cache, tetapi cek stabilitas pembacaan memakai inode/device/mtime/size.
	afterVersion.CTime = version.CTime
	if afterVersion != version {
		return c.failed(e, fmt.Errorf("file berubah selama dibaca; gunakan atomic rename"))
	}
	cfg, err := Parse(data)
	e.version, e.seen = version, true
	if err != nil {
		return c.failed(e, err)
	}
	e.cfg, e.err = cfg, nil
	return cfg, nil
}

func (c *Cache) failed(e *entry, err error) (*Config, error) {
	c.logger.Error("ginxaccess tidak valid", "path", e.path, "error", err, "last_valid", e.cfg != nil)
	if e.cfg != nil {
		e.err = nil
		return e.cfg, nil
	}
	e.err = err
	return nil, err
}
```

`internal/ginxaccess/parser.go`

```go
// Package ginxaccess mengompilasi subset konfigurasi menjadi snapshot immutable.
package ginxaccess

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"aginx/internal/http1"
)

const MaxFileBytes = 64 << 10
const MaxRules = 256

type ACLRule struct {
	Allow   bool
	All     bool
	Network netip.Prefix
}
type HeaderRule struct{ Name, Value string }
type RewriteRule struct {
	Pattern *regexp.Regexp
	Target  string
}

// Apply mengganti match pertama; ekspansi capture dibatasi sebelum hasil membesar.
func (r RewriteRule) Apply(source string) (string, bool, error) {
	indexes := r.Pattern.FindStringSubmatchIndex(source)
	if indexes == nil {
		return "", false, nil
	}
	var out strings.Builder
	appendPart := func(part string) error {
		if out.Len()+len(part) > 8192 {
			return fmt.Errorf("hasil rewrite melebihi 8 KiB")
		}
		out.WriteString(part)
		return nil
	}
	if err := appendPart(source[:indexes[0]]); err != nil {
		return "", true, err
	}
	for i := 0; i < len(r.Target); {
		start := i
		if r.Target[i] != '$' {
			for i < len(r.Target) && r.Target[i] != '$' {
				i++
			}
			if err := appendPart(r.Target[start:i]); err != nil {
				return "", true, err
			}
			continue
		}
		i++
		if i < len(r.Target) && r.Target[i] == '$' {
			i++
		} else if i < len(r.Target) && r.Target[i] == '{' {
			end := strings.IndexByte(r.Target[i:], '}')
			if end >= 0 {
				i += end + 1
			}
		} else {
			for i < len(r.Target) {
				b := r.Target[i]
				if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_') {
					break
				}
				i++
			}
		}
		// Satu capture maksimal sepanjang input, sehingga alokasi sementara tetap terbatas.
		expanded := r.Pattern.ExpandString(nil, r.Target[start:i], source, indexes)
		if err := appendPart(string(expanded)); err != nil {
			return "", true, err
		}
	}
	if err := appendPart(source[indexes[1]:]); err != nil {
		return "", true, err
	}
	return out.String(), true, nil
}

type Config struct {
	ACL      []ACLRule
	Headers  []HeaderRule
	Rewrites []RewriteRule
}

// Allowed memakai first-match dalam satu direktori; default deny bila ada ACL.
func (c *Config) Allowed(ip netip.Addr) bool {
	if c == nil || len(c.ACL) == 0 {
		return true
	}
	ip = ip.Unmap()
	for _, rule := range c.ACL {
		if rule.All || rule.Network.Contains(ip) {
			return rule.Allow
		}
	}
	return false
}

// lexLine menerima token telanjang atau string Go berpetik ganda, diakhiri ';'.
// Tidak ada ekspansi variabel, evaluasi kode, include, atau shell.
func lexLine(line string) ([]string, error) {
	var tokens []string
	ended := false
	for i := 0; i < len(line); {
		if line[i] == ' ' || line[i] == '\t' || line[i] == '\r' {
			i++
			continue
		}
		if line[i] == '#' {
			break
		}
		if ended {
			return nil, fmt.Errorf("teks setelah ';'")
		}
		if line[i] == ';' {
			ended = true
			i++
			continue
		}
		if line[i] == '"' {
			start := i
			i++
			closed := false
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("string tidak ditutup")
			}
			value, err := strconv.Unquote(line[start:i])
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, value)
			if i < len(line) && !strings.ContainsRune(" \t\r;#", rune(line[i])) {
				return nil, fmt.Errorf("pemisah token tidak valid")
			}
		} else {
			start := i
			for i < len(line) && !strings.ContainsRune(" \t\r;#", rune(line[i])) {
				i++
			}
			token := line[start:i]
			if strings.ContainsAny(token, "\"{}<>") {
				return nil, fmt.Errorf("token tidak valid")
			}
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != 0 && !ended {
		return nil, fmt.Errorf("wajib ';' di akhir directive")
	}
	return tokens, nil
}

func Parse(data []byte) (*Config, error) {
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("baris 1: file melebihi 64 KiB")
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return nil, fmt.Errorf("baris 1: UTF-8/NUL tidak valid")
	}
	cfg := &Config{}
	count := 0
	for i, line := range strings.Split(string(data), "\n") {
		tokens, err := lexLine(line)
		if err == nil && len(tokens) != 0 {
			count++
			if count > MaxRules {
				err = fmt.Errorf("maksimum %d directive", MaxRules)
			} else {
				err = cfg.add(tokens)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
	}
	return cfg, nil
}

func (c *Config) add(t []string) error {
	switch t[0] {
	case "allow", "deny":
		if len(t) != 2 {
			return fmt.Errorf("%s membutuhkan IP/CIDR/all", t[0])
		}
		rule := ACLRule{Allow: t[0] == "allow", All: t[1] == "all"}
		if !rule.All {
			prefix, err := netip.ParsePrefix(t[1])
			if err != nil {
				ip, e := netip.ParseAddr(t[1])
				if e != nil || ip.Zone() != "" {
					return fmt.Errorf("IP/CIDR tidak valid")
				}
				ip = ip.Unmap()
				prefix = netip.PrefixFrom(ip, ip.BitLen())
			}
			if prefix.Addr().Is4In6() {
				return fmt.Errorf("gunakan CIDR IPv4 biasa")
			}
			rule.Network = prefix.Masked()
		}
		c.ACL = append(c.ACL, rule)
	case "header":
		if len(t) != 4 || t[1] != "set" || !http1.Token(t[2]) || !http1.Value(t[3]) {
			return fmt.Errorf("format: header set Nama \"Nilai\";")
		}
		name := http.CanonicalHeaderKey(t[2])
		switch name {
		case "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Upgrade", "Keep-Alive", "Proxy-Connection", "Date", "Server":
			return fmt.Errorf("header %s dikelola server", name)
		}
		c.Headers = append(c.Headers, HeaderRule{name, t[3]})
	case "rewrite":
		if len(t) != 4 || t[3] != "last" {
			return fmt.Errorf("format: rewrite \"regex\" \"/target\" last;")
		}
		if len(t[1]) > 1024 || !strings.HasPrefix(t[2], "/") || len(t[2]) > 8192 || !http1.Value(t[2]) {
			return fmt.Errorf("regex/target rewrite tidak valid")
		}
		re, err := regexp.Compile(t[1])
		if err != nil {
			return err
		}
		c.Rewrites = append(c.Rewrites, RewriteRule{re, t[2]})
	default:
		return fmt.Errorf("directive tidak dikenal: %s", t[0])
	}
	return nil
}
```

`internal/http1/parser.go`

```go
// Package http1 menyediakan parser request HTTP/1.x khusus static server.
package http1

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Request struct {
	Method string
	URL    *url.URL
	Header http.Header
	Close  bool
}

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }
func bad(message string) error { return &Error{400, message} }

// Token mengikuti tchar RFC 9110; nama header tidak boleh mengandung whitespace.
func Token(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func Value(s string) bool {
	for i := 0; i < len(s); i++ {
		if (s[i] < 32 && s[i] != '\t') || s[i] == 127 {
			return false
		}
	}
	return true
}

// readLine menegakkan batas kumulatif SEBELUM alokasi bertambah tanpa batas.
func readLine(r *bufio.Reader, remaining *int) (string, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		*remaining -= len(part)
		if *remaining < 0 {
			return "", &Error{431, "header terlalu besar"}
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) != 0 {
				return "", io.ErrUnexpectedEOF
			}
			return "", err
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return "", bad("wajib CRLF")
		}
		return string(line[:len(line)-2]), nil
	}
}

// Read menolak framing ambigu dan semua body. POC hanya melayani GET/HEAD.
// Request dengan body tidak didrain: koneksi wajib ditutup setelah error.
func Read(r *bufio.Reader, maxBytes int) (*Request, error) {
	remaining := maxBytes
	line, err := readLine(r, &remaining)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(line, " ")
	if len(parts) != 3 || !Token(parts[0]) {
		return nil, bad("request line tidak valid")
	}
	if parts[2] != "HTTP/1.1" && parts[2] != "HTTP/1.0" {
		return nil, &Error{505, "versi HTTP tidak didukung"}
	}
	if !strings.HasPrefix(parts[1], "/") || strings.HasPrefix(parts[1], "//") || strings.ContainsAny(parts[1], "#\r\n\t") {
		return nil, bad("wajib origin-form target")
	}
	u, err := url.ParseRequestURI(parts[1])
	if err != nil || u.Host != "" || u.Scheme != "" {
		return nil, bad("target tidak valid")
	}
	req := &Request{Method: parts[0], URL: u, Header: make(http.Header), Close: parts[2] == "HTTP/1.0"}
	for count := 0; ; count++ {
		line, err = readLine(r, &remaining)
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		if count >= 100 {
			return nil, &Error{431, "terlalu banyak header"}
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !Token(name) || !Value(value) {
			return nil, bad("header tidak valid/obs-fold")
		}
		req.Header.Add(name, strings.Trim(value, " \t"))
	}
	hosts := req.Header.Values("Host")
	if len(hosts) > 1 || (parts[2] == "HTTP/1.1" && len(hosts) != 1) {
		return nil, bad("Host harus tepat satu")
	}
	if len(hosts) == 1 {
		host := hosts[0]
		parsed, e := url.Parse("http://" + host)
		if e != nil || host == "" || strings.ContainsAny(host, " \t,/@?#\\%") || parsed.Host != host || parsed.Hostname() == "" {
			return nil, bad("Host tidak valid")
		}
	}
	// Jangan izinkan TE+CL, duplicate CL, chunked, atau body tersisa menjadi request berikutnya.
	if _, ok := req.Header["Transfer-Encoding"]; ok {
		return nil, bad("Transfer-Encoding belum didukung")
	}
	lengths := req.Header.Values("Content-Length")
	if len(lengths) > 1 {
		return nil, bad("duplicate Content-Length")
	}
	if len(lengths) == 1 {
		v := lengths[0]
		for _, c := range v {
			if c < '0' || c > '9' {
				return nil, bad("Content-Length tidak valid")
			}
		}
		n, e := strconv.ParseUint(v, 10, 63)
		if e != nil || n != 0 {
			return nil, bad("request body belum didukung")
		}
	}
	keep, closeConn := false, false
	for _, value := range req.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(token)) {
			case "close":
				closeConn = true
			case "keep-alive":
				keep = true
			}
		}
	}
	if keep {
		req.Close = false
	}
	if closeConn {
		req.Close = true
	}
	if req.Header.Get("Expect") != "" || req.Header.Get("Upgrade") != "" {
		return nil, bad("Expect/Upgrade belum didukung")
	}
	return req, nil
}
```

`internal/server/server.go`

```go
// Package server memakai goroutine per koneksi dengan socket non-blocking milik Go.
package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"aginx/internal/http1"
	"aginx/internal/static"
)

type Options struct {
	HeaderTimeout, IdleTimeout, WriteTimeout time.Duration
	MaxHeaderBytes, MaxConnections           int
}

func DefaultOptions() Options {
	return Options{HeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, MaxConnections: 10000}
}

type Server struct {
	handler   *static.Handler
	options   Options
	logger    *slog.Logger
	mu        sync.Mutex
	listener  net.Listener
	conns     map[net.Conn]bool
	stopping  bool
	wg        sync.WaitGroup
	serveDone chan struct{}
}

func New(handler *static.Handler, options Options, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{handler: handler, options: options, logger: logger, conns: make(map[net.Conn]bool), serveDone: make(chan struct{})}
}

func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		listener.Close()
		close(s.serveDone)
		return nil
	}
	s.listener = listener
	s.mu.Unlock()
	defer close(s.serveDone)
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			stopping := s.stopping
			s.mu.Unlock()
			if stopping {
				return nil
			}
			return err
		}
		s.mu.Lock()
		if s.stopping || len(s.conns) >= s.options.MaxConnections {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.conns[conn] = false
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serveConn(conn)
	}
}

func (s *Server) active(conn net.Conn, value bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false
	}
	s.conns[conn] = value
	return true
}

func (s *Server) serveConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() { conn.Close(); s.mu.Lock(); delete(s.conns, conn); s.mu.Unlock() }()
	addr, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return
	}
	reader := bufio.NewReaderSize(conn, 4096)
	for requestCount := 0; requestCount < 1000; requestCount++ {
		if !s.active(conn, false) {
			return
		}
		timeout := s.options.IdleTimeout
		if requestCount == 0 {
			timeout = s.options.HeaderTimeout
		}
		if conn.SetReadDeadline(time.Now().Add(timeout)) != nil {
			return
		}
		if _, err := reader.Peek(1); err != nil {
			return
		}
		if conn.SetReadDeadline(time.Now().Add(s.options.HeaderTimeout)) != nil {
			return
		}
		req, err := http1.Read(reader, s.options.MaxHeaderBytes)
		if err != nil {
			status := 400
			var parseErr *http1.Error
			if errors.As(err, &parseErr) {
				status = parseErr.Status
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				status = 408
			}
			if !errors.Is(err, io.EOF) {
				_ = s.write(conn, static.Error(status), false, true)
			}
			return
		}
		if !s.active(conn, true) {
			return
		}
		closeConn := req.Close || requestCount == 999
		resp := s.handler.Handle(req, addr.Addr().Unmap())
		err = s.write(conn, resp, req.Method == "HEAD", closeConn)
		if err != nil {
			s.logger.Debug("response terputus", "remote", conn.RemoteAddr().String(), "error", err)
			return
		}
		if closeConn {
			return
		}
	}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (s *Server) write(conn net.Conn, response *static.Response, head, closeConn bool) error {
	if response.File != nil {
		defer response.File.Close()
	}
	if err := conn.SetWriteDeadline(time.Now().Add(s.options.WriteTimeout)); err != nil {
		return err
	}
	header := response.Header
	header.Set("Content-Length", strconv.FormatInt(response.Size, 10))
	header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	header.Set("Server", "AGinX/0.1-poc")
	if header.Get("X-Content-Type-Options") == "" {
		header.Set("X-Content-Type-Options", "nosniff")
	}
	if closeConn {
		header.Set("Connection", "close")
	} else {
		header.Set("Connection", "keep-alive")
	}
	var text strings.Builder
	fmt.Fprintf(&text, "HTTP/1.1 %d %s\r\n", response.Status, http.StatusText(response.Status))
	if err := header.Write(&text); err != nil {
		return err
	}
	text.WriteString("\r\n")
	if err := writeAll(conn, []byte(text.String())); err != nil {
		return err
	}
	if head {
		return nil
	}
	if response.File != nil {
		// Mempertahankan *net.TCPConn dan *os.File tanpa wrapper menjaga jalur sendfile.
		// Runtime menangani EINTR, EAGAIN, kesiapan epoll, deadline, dan partial transfer.
		_, err := io.CopyN(conn, response.File, response.Size)
		return err
	}
	return writeAll(conn, []byte(response.Body))
}

// Shutdown menutup listener/idle connection lalu menunggu response aktif selesai.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	if s.listener != nil {
		s.listener.Close()
	}
	for conn, active := range s.conns {
		if !active {
			conn.Close()
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { <-s.serveDone; s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for conn := range s.conns {
			conn.Close()
		}
		s.mu.Unlock()
		<-done
		return ctx.Err()
	}
}
```

`internal/static/handler.go`

```go
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
	components := 0
	for _, part := range parts {
		if part != "" {
			components++
			if components > 32 {
				return "", os.ErrPermission
			}
		}
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
```
