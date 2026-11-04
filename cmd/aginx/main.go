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
