package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"syncd/internal/config"
	"syncd/internal/fanout"
	"syncd/internal/hook"
	"syncd/internal/journal"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.HubRoot, 0o700); err != nil {
		log.Error("hub_root", "err", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SQLite), 0o700); err != nil {
		log.Error("sqlite dir", "err", err)
		os.Exit(1)
	}
	db, err := journal.Open(cfg.SQLite)
	if err != nil {
		log.Error("sqlite", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	eng := fanout.NewEngine(cfg, db, log)
	mux := hook.Mux(eng.Enqueue, func(source, repoName string) string {
		r, ok := cfg.Repo(repoName)
		if !ok {
			return ""
		}
		switch source {
		case "github":
			return r.Secrets.GitHub
		case "forgejo":
			return r.Secrets.Forgejo
		case "gitlawb":
			return r.Secrets.GitLawb
		default:
			return r.Secrets.Forgejo
		}
	}, log)

	srv := &http.Server{Addr: cfg.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listen", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http", "err", err)
			stop()
		}
	}()
	go eng.Run(ctx)

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
