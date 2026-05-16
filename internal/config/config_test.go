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
	if cfg.TLSCertFile != filepath.Join(cfg.StateDir, "certs", "server.crt") {
		t.Fatalf("TLS cert file = %q", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != filepath.Join(cfg.StateDir, "certs", "server.key") {
		t.Fatalf("TLS key file = %q", cfg.TLSKeyFile)
	}
	if cfg.AuthSecretFile != filepath.Join(cfg.StateDir, "auth", "session.key") {
		t.Fatalf("auth secret file = %q", cfg.AuthSecretFile)
	}
	if cfg.CookieTTL != 48*60*60 {
		t.Fatalf("cookie ttl = %d, want 172800", cfg.CookieTTL)
	}
}

func TestLoadFromEnvReadsTLSPathOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIRROR_PASSWORD", "secret")
	t.Setenv("MIRROR_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("MIRROR_TLS_CERT_FILE", filepath.Join(dir, "custom.crt"))
	t.Setenv("MIRROR_TLS_KEY_FILE", filepath.Join(dir, "custom.key"))

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSCertFile != filepath.Join(dir, "custom.crt") {
		t.Fatalf("TLS cert file = %q", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != filepath.Join(dir, "custom.key") {
		t.Fatalf("TLS key file = %q", cfg.TLSKeyFile)
	}
}

func TestLoadFromEnvReadsAuthSecretPathOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIRROR_PASSWORD", "secret")
	t.Setenv("MIRROR_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("MIRROR_AUTH_SECRET_FILE", filepath.Join(dir, "custom-session.key"))

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthSecretFile != filepath.Join(dir, "custom-session.key") {
		t.Fatalf("auth secret file = %q", cfg.AuthSecretFile)
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
