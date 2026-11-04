package http1

import (
	"bufio"
	"strings"
	"testing"
)

func TestFraming(t *testing.T) {
	cases := []struct {
		name, input string
		status      int
	}{
		{"valid", "GET /a?q=b HTTP/1.1\r\nHost: example.test\r\n\r\n", 0},
		{"http10", "HEAD / HTTP/1.0\r\n\r\n", 0},
		{"missing-host", "GET / HTTP/1.1\r\n\r\n", 400},
		{"two-host", "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n", 400},
		{"bad-host", "GET / HTTP/1.1\r\nHost: a@b\r\n\r\n", 400},
		{"lf", "GET / HTTP/1.1\nHost: a\n\n", 400},
		{"fold", "GET / HTTP/1.1\r\nHost: a\r\n X: b\r\n\r\n", 400},
		{"space-name", "GET / HTTP/1.1\r\nHost: a\r\nX : b\r\n\r\n", 400},
		{"duplicate-cl", "GET / HTTP/1.1\r\nHost: a\r\nContent-Length: 0\r\nContent-Length: 0\r\n\r\n", 400},
		{"te-cl", "GET / HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\nContent-Length: 0\r\n\r\n", 400},
		{"te", "GET / HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\n\r\n", 400},
		{"body", "GET / HTTP/1.1\r\nHost: a\r\nContent-Length: 1\r\n\r\nx", 400},
		{"cl-plus", "GET / HTTP/1.1\r\nHost: a\r\nContent-Length: +0\r\n\r\n", 400},
		{"cl-list", "GET / HTTP/1.1\r\nHost: a\r\nContent-Length: 0, 0\r\n\r\n", 400},
		{"absolute", "GET http://a/ HTTP/1.1\r\nHost: a\r\n\r\n", 400},
		{"http2", "GET / HTTP/2.0\r\nHost: a\r\n\r\n", 505},
		{"oversize", "GET / HTTP/1.1\r\nHost: a\r\nX: " + strings.Repeat("a", 17000) + "\r\n\r\n", 431},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Read(bufio.NewReader(strings.NewReader(tc.input)), 16<<10)
			if tc.status == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			e, ok := err.(*Error)
			if !ok || e.Status != tc.status {
				t.Fatalf("got %v, want status %d", err, tc.status)
			}
		})
	}
}

func TestPipelineAndConnectionClose(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("GET /one HTTP/1.1\r\nHost: a\r\n\r\nHEAD /two HTTP/1.0\r\nConnection: keep-alive, close\r\n\r\n"))
	a, err := Read(r, 4096)
	if err != nil || a.URL.Path != "/one" || a.Close {
		t.Fatalf("first: %+v %v", a, err)
	}
	b, err := Read(r, 4096)
	if err != nil || b.URL.Path != "/two" || !b.Close {
		t.Fatalf("second: %+v %v", b, err)
	}
}

func FuzzRead(f *testing.F) {
	for _, value := range []string{"GET / HTTP/1.1\r\nHost: a\r\n\r\n", "GET /%2e%2e HTTP/1.1\r\nHost: a\r\n\r\n", "\x00", "", "GET / HTTP/1.1\r\nContent-Length: 1\r\nTransfer-Encoding: chunked\r\n\r\n"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 1<<16 {
			t.Skip()
		}
		req, err := Read(bufio.NewReader(strings.NewReader(value)), 16<<10)
		if err == nil && (req == nil || req.URL == nil || req.Header == nil) {
			t.Fatal("request berhasil tanpa struktur lengkap")
		}
	})
}
