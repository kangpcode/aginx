package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"aginx/internal/fsroot"
	"aginx/internal/ginxaccess"
	"aginx/internal/static"
)

type memoryListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }

type memoryConn struct{ net.Conn }

func (c memoryConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}

// Sandbox tanpa izin socket tetap menguji protokol lewat net.Pipe.
// AGINX_REQUIRE_TCP=1 memaksa integration suite menggunakan TCP sungguhan (CI).
func testListener(t *testing.T) (net.Listener, func() (net.Conn, error)) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		t.Log("transport: TCP loopback")
		return l, func() (net.Conn, error) { return net.DialTimeout("tcp", l.Addr().String(), time.Second) }
	}
	if os.Getenv("AGINX_REQUIRE_TCP") == "1" || (!errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES)) {
		t.Fatal(err)
	}
	t.Logf("transport: net.Pipe; TCP diblokir sandbox: %v", err)
	m := &memoryListener{conns: make(chan net.Conn), done: make(chan struct{})}
	return m, func() (net.Conn, error) {
		client, server := net.Pipe()
		select {
		case m.conns <- memoryConn{server}:
			return client, nil
		case <-m.done:
			client.Close()
			server.Close()
			return nil, net.ErrClosed
		}
	}
}

func start(t *testing.T, change func(*Options)) (*Server, func() (net.Conn, error), string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("hello AGinX"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := fsroot.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := &static.Handler{Root: root, Cache: ginxaccess.NewCache(root, time.Millisecond, 32, logger), Access: true}
	options := DefaultOptions()
	if change != nil {
		change(&options)
	}
	srv := New(handler, options, logger)
	listener, connect := testListener(t)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
		root.Close()
	})
	return srv, connect, dir
}

func dial(t *testing.T, connect func() (net.Conn, error)) net.Conn {
	t.Helper()
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	return conn
}

func TestTCPKeepAlivePipelineHeadAndClose(t *testing.T) {
	_, connect, _ := start(t, nil)
	conn := dial(t, connect)
	_, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\nHEAD /index.html HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(body) != "hello AGinX" || resp.StatusCode != 200 {
		t.Fatalf("GET: %s %v", body, err)
	}
	resp, err = http.ReadResponse(r, &http.Request{Method: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength != 11 || !resp.Close {
		t.Fatalf("HEAD: %+v", resp)
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatalf("koneksi harus ditutup: %v", err)
	}
}

func TestSlowHeaderAndOversize(t *testing.T) {
	_, connect, _ := start(t, func(o *Options) { o.HeaderTimeout = 80 * time.Millisecond; o.MaxHeaderBytes = 1024 })
	conn := dial(t, connect)
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nX:")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || resp.StatusCode != 408 {
		t.Fatalf("slowloris: %v %v", resp, err)
	}
	resp.Body.Close()
	other := dial(t, connect)
	io.WriteString(other, "GET / HTTP/1.1\r\nHost: test\r\nX: "+strings.Repeat("a", 2048)+"\r\n\r\n")
	resp, err = http.ReadResponse(bufio.NewReader(other), nil)
	if err != nil || resp.StatusCode != 431 {
		t.Fatalf("header size: %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestLargeFileIntegrityAndShutdown(t *testing.T) {
	srv, connect, dir := start(t, nil)
	data := bytes.Repeat([]byte("AGinX-partial-write-test\n"), 200000)
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, connect)
	io.WriteString(conn, "GET /large.bin HTTP/1.1\r\nHost: test\r\n\r\n")
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdown <- srv.Shutdown(ctx)
	}()
	actual, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(data, actual) {
		t.Fatalf("large transfer terpotong: %d/%d %v", len(actual), len(data), err)
	}
	if err := <-shutdown; err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("shutdown tidak menutup keep-alive: %v", err)
	}
}

func TestConcurrentClientsAndConfigChange(t *testing.T) {
	_, connect, dir := start(t, nil)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DialContext: func(context.Context, string, string) (net.Conn, error) { return connect() }}}
			defer client.CloseIdleConnections()
			for i := 0; i < 20; i++ {
				resp, err := client.Get("http://aginx.test/index.html")
				if err != nil {
					t.Error(err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Errorf("status %d", resp.StatusCode)
				}
			}
		}()
	}
	for n := 0; n < 20; n++ {
		tmp := filepath.Join(dir, ".next")
		if err := os.WriteFile(tmp, []byte(fmt.Sprintf("header set X-Generation v%d;", n)), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(dir, ".ginxaccess")); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

type partialWriter struct{ buf bytes.Buffer }

func (w *partialWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	return w.buf.Write(p)
}
func TestWriteAllHandlesPartialWrite(t *testing.T) {
	w := &partialWriter{}
	value := []byte("response lebih panjang dari tiga byte")
	if err := writeAll(w, value); err != nil || !bytes.Equal(w.buf.Bytes(), value) {
		t.Fatal("partial write gagal", err)
	}
}

func TestForwardedHeaderDoesNotBypassACL(t *testing.T) {
	_, connect, dir := start(t, nil)
	if err := os.WriteFile(filepath.Join(dir, ".ginxaccess"), []byte("allow 198.51.100.1;\ndeny all;"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, connect)
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nX-Forwarded-For: 198.51.100.1\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("XFF melewati ACL: %d", resp.StatusCode)
	}
}

func TestShutdownDeadlineClosesActiveWriter(t *testing.T) {
	srv, connect, dir := start(t, nil)
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), bytes.Repeat([]byte("x"), 8<<20), 0600); err != nil {
		t.Fatal(err)
	}
	conn := dial(t, connect)
	if tcp, ok := conn.(*net.TCPConn); ok {
		if err := tcp.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	io.WriteString(conn, "GET /large.bin HTTP/1.1\r\nHost: test\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown harus mencapai deadline: %v", err)
	}
	resp.Body.Close()
}

func TestConnectionLimitAndIdleTimeout(t *testing.T) {
	srv, connect, _ := start(t, func(o *Options) {
		o.MaxConnections = 1
		o.HeaderTimeout = time.Second
		o.IdleTimeout = 30 * time.Millisecond
	})
	first := dial(t, connect)
	io.WriteString(first, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(first), nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	second := dial(t, connect)
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("koneksi berlebih harus ditutup")
	}
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Fatal("idle connection harus ditutup")
	}
	srv.mu.Lock()
	count := len(srv.conns)
	srv.mu.Unlock()
	if count > 1 {
		t.Fatal("batas koneksi terlampaui")
	}
}
