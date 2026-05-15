package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvRequiresPassword(t *testing.T) {
	t.Setenv("MIRROR_PASSWORD", "")
	t.Setenv("MIRROR_PASSWORD_FILE", "")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected missing password error")
	}
}

func TestLoadFromEnvReadsPasswordFile(t *testing.T) {
	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "password")
	if err := os.WriteFile(passwordFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIRROR_PASSWORD", "")
	t.Setenv("MIRROR_PASSWORD_FILE", passwordFile)
	t.Setenv("MIRROR_BIND_ADDR", "127.0.0.1")
	t.Setenv("MIRROR_PORT", "9090")
	t.Setenv("MIRROR_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("MIRROR_COOKIE_SECURE", "true")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "secret" {
		t.Fatalf("password = %q, want secret", cfg.Password)
	}
	if cfg.ListenAddress() != "127.0.0.1:9090" {
		t.Fatalf("listen address = %q", cfg.ListenAddress())
	}
	if !cfg.CookieSecure {
		t.Fatal("expected secure cookie")
	}
}

func TestValidateRejectsInvalidPort(t *testing.T) {
	cfg := Config{
		BindAddr:  "127.0.0.1",
		Port:      70000,
		Password:  "secret",
		StateDir:  t.TempDir(),
		CookieTTL: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid port error")
	}
}
