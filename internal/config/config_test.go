package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvRequiresPassword(t *testing.T) {
	t.Setenv("CONTROL_AGENTS_PASSWORD", "")
	t.Setenv("CONTROL_AGENTS_PASSWORD_FILE", "")

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

	t.Setenv("CONTROL_AGENTS_PASSWORD", "")
	t.Setenv("CONTROL_AGENTS_PASSWORD_FILE", passwordFile)
	t.Setenv("CONTROL_AGENTS_BIND_ADDR", "127.0.0.1")
	t.Setenv("CONTROL_AGENTS_PORT", "9090")
	t.Setenv("CONTROL_AGENTS_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("CONTROL_AGENTS_COOKIE_SECURE", "true")

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
	if cfg.MaxSessions != 32 {
		t.Fatalf("max sessions = %d, want 32", cfg.MaxSessions)
	}
	if cfg.SnapshotMaxBytes != 32*1024*1024 {
		t.Fatalf("snapshot max bytes = %d, want 32 MiB", cfg.SnapshotMaxBytes)
	}
}

func TestLoadFromEnvReadsSnapshotMaxBytes(t *testing.T) {
	t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
	t.Setenv("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES", "1048576")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SnapshotMaxBytes != 1048576 {
		t.Fatalf("snapshot max bytes = %d, want 1048576", cfg.SnapshotMaxBytes)
	}
}

func TestLoadFromEnvRejectsInvalidSnapshotMaxBytes(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", "many"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
			t.Setenv("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES", value)
			if _, err := LoadFromEnv(); err == nil {
				t.Fatalf("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES=%q was accepted", value)
			}
		})
	}
}

func TestLoadFromEnvReadsMaxSessions(t *testing.T) {
	t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
	t.Setenv("CONTROL_AGENTS_MAX_SESSIONS", "7")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSessions != 7 {
		t.Fatalf("max sessions = %d, want 7", cfg.MaxSessions)
	}
}

func TestLoadFromEnvRejectsInvalidMaxSessions(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", "many"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
			t.Setenv("CONTROL_AGENTS_MAX_SESSIONS", value)
			if _, err := LoadFromEnv(); err == nil {
				t.Fatalf("CONTROL_AGENTS_MAX_SESSIONS=%q was accepted", value)
			}
		})
	}
}

func TestLoadFromEnvReadsTLSPathOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
	t.Setenv("CONTROL_AGENTS_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("CONTROL_AGENTS_TLS_CERT_FILE", filepath.Join(dir, "custom.crt"))
	t.Setenv("CONTROL_AGENTS_TLS_KEY_FILE", filepath.Join(dir, "custom.key"))

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
	t.Setenv("CONTROL_AGENTS_PASSWORD", "secret")
	t.Setenv("CONTROL_AGENTS_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("CONTROL_AGENTS_AUTH_SECRET_FILE", filepath.Join(dir, "custom-session.key"))

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
		BindAddr:         "127.0.0.1",
		Port:             70000,
		Password:         "secret",
		StateDir:         t.TempDir(),
		CookieTTL:        60,
		MaxSessions:      32,
		SnapshotMaxBytes: 32 * 1024 * 1024,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid port error")
	}
}
