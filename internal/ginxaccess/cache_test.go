package ginxaccess

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"aginx/internal/fsroot"
)

func newTestCache(t testing.TB, capacity int) (*Cache, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := fsroot.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return NewCache(root, time.Hour, capacity, slog.New(slog.NewTextHandler(io.Discard, nil))), dir
}

func publish(t testing.TB, dir, text string) {
	t.Helper()
	tmp := filepath.Join(dir, ".next")
	if err := os.WriteFile(tmp, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, ".ginxaccess")); err != nil {
		t.Fatal(err)
	}
}

func TestCacheLifecycle(t *testing.T) {
	c, dir := newTestCache(t, 8)
	if cfg, err := c.Get(".ginxaccess"); err != nil || cfg != nil {
		t.Fatalf("missing: %v %v", cfg, err)
	}
	publish(t, dir, "header set X-Test one;")
	c.Invalidate()
	cfg, err := c.Get(".ginxaccess")
	if err != nil || cfg.Headers[0].Value != "one" {
		t.Fatalf("create: %v %v", cfg, err)
	}
	publish(t, dir, "deny broken;")
	c.Invalidate()
	kept, err := c.Get(".ginxaccess")
	if err != nil || kept != cfg {
		t.Fatal("versi valid terakhir harus bertahan")
	}
	c.Invalidate()
	if kept, err := c.Get(".ginxaccess"); err != nil || kept != cfg {
		t.Fatal("SIGHUP kehilangan versi valid")
	}
	publish(t, dir, "header set X-Test two;")
	c.Invalidate()
	updated, err := c.Get(".ginxaccess")
	if err != nil || updated.Headers[0].Value != "two" {
		t.Fatalf("replace: %v %v", updated, err)
	}
	if err := os.Remove(filepath.Join(dir, ".ginxaccess")); err != nil {
		t.Fatal(err)
	}
	c.Invalidate()
	if cfg, err := c.Get(".ginxaccess"); err != nil || cfg != nil {
		t.Fatal("deletion tidak terdeteksi")
	}
}

func TestInvalidFirstConfigFailsClosed(t *testing.T) {
	c, dir := newTestCache(t, 2)
	publish(t, dir, "not-valid;")
	if cfg, err := c.Get(".ginxaccess"); cfg != nil || err == nil {
		t.Fatal("konfigurasi pertama rusak harus fail closed")
	}
	for i := 0; i < 10; i++ {
		_, _ = c.Get(fmt.Sprintf("missing-%d/.ginxaccess", i))
	}
	if len(c.items) > 2 {
		t.Fatal("cache melampaui kapasitas")
	}
}

func TestTTLDetectsCreateWithoutReload(t *testing.T) {
	c, dir := newTestCache(t, 8)
	c.ttl = time.Millisecond
	if _, err := c.Get(".ginxaccess"); err != nil {
		t.Fatal(err)
	}
	publish(t, dir, "deny all;")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		cfg, err := c.Get(".ginxaccess")
		if err != nil {
			t.Fatal(err)
		}
		if cfg != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("TTL tidak mendeteksi file baru")
}

func TestConcurrentPublishAndRead(t *testing.T) {
	c, dir := newTestCache(t, 8)
	publish(t, dir, "allow all;")
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = c.Get(".ginxaccess")
			}
		}()
	}
	for i := 0; i < 20; i++ {
		publish(t, dir, fmt.Sprintf("header set X-Test value-%d;", i))
		c.Invalidate()
	}
	wg.Wait()
}

func BenchmarkCacheHit(b *testing.B) {
	c, dir := newTestCache(b, 8)
	publish(b, dir, "allow all;\nheader set X-Test cached;")
	if _, err := c.Get(".ginxaccess"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.Get(".ginxaccess"); err != nil {
				b.Error(err)
			}
		}
	})
}
