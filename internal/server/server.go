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
