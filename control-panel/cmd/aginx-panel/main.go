// aginx-panel adalah control panel native untuk web server AGinX.
//
// Slice pertama berjalan sebagai satu proses dengan UI embedded. Pemisahan
// web (non-root) dan agen (root) dijadwalkan pada milestone berikutnya; kontrak
// layanan di internal/panel sudah dipisahkan agar pemindahan tidak mengubah API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aginx/control-panel/internal/panel"
	"aginx/control-panel/internal/ui"
)

var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error("aginx-panel berhenti", "error", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "127.0.0.1:8090", "alamat listen panel")
	dataDir := flag.String("data", "./control-panel/devdata", "direktori state panel")
	wwwBase := flag.String("www", "", "basis docroot situs (default <data>/www)")
	pidFile := flag.String("engine-pid-file", "", "pid file engine AGinX untuk SIGHUP setelah publish")
	showVersion := flag.Bool("version", false, "cetak versi lalu keluar")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	absData, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absData, 0o700); err != nil {
		return err
	}
	base := *wwwBase
	if base == "" {
		base = filepath.Join(absData, "www")
	}
	store, err := panel.OpenStore(absData)
	if err != nil {
		return err
	}
	reloader := &panel.EngineReloader{PIDFile: *pidFile, Logger: logger}
	p, err := panel.NewPanel(store, base, absData, version, reloader, logger)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	p.Register(mux)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(ui.Assets())))
	mux.Handle("GET /", ui.Handler())

	srv := &http.Server{
		Addr:              *listen,
		Handler:           p.Middleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	fmt.Printf("AGinX Control Panel %s\n", version)
	fmt.Printf("  data : %s\n", absData)
	fmt.Printf("  www  : %s\n", p.WWWBase())
	if token := p.SetupToken(); token != "" {
		fmt.Printf("  setup token: %s\n", token)
		fmt.Println("  buka panel, lalu masukkan token di halaman setup.")
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	logger.Info("panel siap", "listen", *listen, "version", version)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		logger.Info("sinyal diterima, menghentikan panel", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
