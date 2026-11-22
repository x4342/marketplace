package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/x4342/marketplace/internal/config"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/platform"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log); err != nil {
		log.Error("worker stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := platform.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := queries.RecordWorkerHeartbeat(writeCtx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Warn("worker heartbeat failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
