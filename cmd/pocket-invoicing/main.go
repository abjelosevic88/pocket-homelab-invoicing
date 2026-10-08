// Command pocket-invoicing is a self-hosted invoicing platform for freelancers
// and homelabbers: one binary, one SQLite file, one volume.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/config"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/server"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
	"github.com/abjelosevic88/pocket-homelab-invoicing/web"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println("pocket-invoicing", version)
			return
		case "backup":
			runBackup(os.Args[2:])
			return
		case "scheduler":
			runSchedulerOnce()
			return
		case "help", "--help", "-h":
			fmt.Println(`pocket-invoicing — self-hosted invoicing

Usage:
  pocket-invoicing            start the server (default)
  pocket-invoicing backup [-o FILE]   write a consistent SQLite backup
  pocket-invoicing scheduler  run recurring/overdue/reminder jobs once and exit
  pocket-invoicing version    print version

Configuration is read from environment variables, see docs/configuration.md.`)
			return
		}
	}
	serve()
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func mustLoad() (*config.Config, *slog.Logger, *store.Store) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := newLogger(cfg)
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		log.Error("open database", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	return cfg, log, st
}

func serve() {
	server.Version = version
	cfg, log, st := mustLoad()
	defer st.Close()

	var webFS fs.FS
	if sub, err := fs.Sub(web.Dist, "dist"); err == nil {
		if f, err := sub.Open("index.html"); err == nil {
			f.Close()
			webFS = sub
		}
	}
	if webFS == nil {
		log.Warn("web UI not embedded; API only (run `make web` before building)")
	}

	srv := server.New(cfg, st, log, webFS)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := srv.Bootstrap(ctx); err != nil {
		log.Error("bootstrap", "err", err)
		os.Exit(1)
	}

	if cfg.SchedulerEnabled {
		go func() {
			// first run shortly after boot so missed recurring invoices catch up
			t := time.NewTimer(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				res := srv.RunScheduler(ctx)
				log.Debug("scheduler run", "result", res)
				t.Reset(cfg.SchedulerInterval)
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Info("pocket-invoicing started", "version", version, "addr", cfg.Addr(), "base_url", cfg.BaseURL, "db", cfg.DBPath, "pdf_engine", cfg.PDFEngine, "auth_mode", cfg.AuthMode)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, c2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer c2()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("o", "", "output file (default: DATA_DIR/backups/pocket-invoicing-<timestamp>.db)")
	_ = fs.Parse(args)
	cfg, log, st := mustLoad()
	defer st.Close()
	dst := *out
	if dst == "" {
		_ = os.MkdirAll(cfg.DataDir+"/backups", 0o755)
		dst = fmt.Sprintf("%s/backups/pocket-invoicing-%s.db", cfg.DataDir, time.Now().Format("20060102-150405"))
	}
	if err := st.Backup(context.Background(), dst); err != nil {
		log.Error("backup failed", "err", err)
		os.Exit(1)
	}
	fmt.Println(dst)
}

func runSchedulerOnce() {
	cfg, log, st := mustLoad()
	defer st.Close()
	srv := server.New(cfg, st, log, nil)
	res := srv.RunScheduler(context.Background())
	log.Info("scheduler finished", "result", res)
}
