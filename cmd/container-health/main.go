package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/x4342/marketplace/internal/config"
	"github.com/x4342/marketplace/internal/db"
	"github.com/x4342/marketplace/internal/platform"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	switch os.Args[1] {
	case "web":
		if webReady(ctx) {
			return
		}
	case "worker":
		if workerReady(ctx) {
			return
		}
	}
	os.Exit(1)
}

func webReady(ctx context.Context) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080/health/ready", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16))
	return err == nil && response.StatusCode == http.StatusOK && string(body) == "ready\n"
}

func workerReady(ctx context.Context) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	pool, err := platform.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return false
	}
	defer pool.Close()
	heartbeat, err := db.New(pool).WorkerHeartbeat(ctx)
	return err == nil && heartbeat.Valid && time.Since(heartbeat.Time) <= 45*time.Second
}
