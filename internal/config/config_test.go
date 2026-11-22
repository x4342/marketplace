package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing DATABASE_URL accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://local")
	t.Setenv("HTTP_ADDR", "")
	cfg, err := Load()
	if err != nil || cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("default address: %q (%v)", cfg.HTTPAddr, err)
	}
	for _, address := range []string{"0.0.0.0:8080", "[::]:8080", "example.test:8080", "127.0.0.1"} {
		t.Setenv("HTTP_ADDR", address)
		if _, err := Load(); err == nil {
			t.Errorf("accepted non-loopback or malformed address %q", address)
		}
	}
	t.Setenv("HTTP_ADDR", "[::1]:8080")
	if _, err := Load(); err != nil {
		t.Errorf("rejected IPv6 loopback: %v", err)
	}
}
