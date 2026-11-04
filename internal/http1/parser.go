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
