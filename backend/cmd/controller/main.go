package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaofujie369/RotatoPilot/backend/internal/api"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/config"
	"github.com/xiaofujie369/RotatoPilot/backend/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		slog.Error("database error", "error", err)
		os.Exit(1)
	}
	defer st.DB.Close()
	app := api.New(cfg, st)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go app.RunScheduler(ctx)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: app.Router(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("RotatoPilot controller started", "port", cfg.Port, "version", api.Version)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	_ = server.Shutdown(shutdown)
	slog.Info("controller stopped")
}
