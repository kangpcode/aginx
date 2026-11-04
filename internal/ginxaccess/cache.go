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
	if identity(after) != version {
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
