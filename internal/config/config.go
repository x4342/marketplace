package config

import (
	"errors"
	"net"
	"net/netip"
	"os"
)

// Config contains process configuration shared by web and worker.
type Config struct {
	DatabaseURL string
	HTTPAddr    string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    os.Getenv("HTTP_ADDR"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return Config{}, errors.New("HTTP_ADDR must be a loopback IP and port")
	}
	ip, err := netip.ParseAddr(host)
	if err == nil && (ip.IsLoopback() || (host == "0.0.0.0" && os.Getenv("MARKETPLACE_CONTAINER_BRIDGE") == "1")) {
		return cfg, nil
	}
	return Config{}, errors.New("HTTP_ADDR must be a loopback IP and port, or 0.0.0.0 with MARKETPLACE_CONTAINER_BRIDGE=1")
}
