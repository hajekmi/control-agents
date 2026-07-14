package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultBindAddr    = "0.0.0.0"
	defaultPort        = 8080
	defaultCookieTTL   = 48 * 60 * 60
	defaultMaxSessions = 32
	defaultSnapshotMax = 32 * 1024 * 1024
)

type Config struct {
	BindAddr         string
	Port             int
	Password         string
	StateDir         string
	HomeDir          string
	TLSCertFile      string
	TLSKeyFile       string
	AuthSecretFile   string
	CookieSecure     bool
	CookieTTL        int
	MaxSessions      int
	SnapshotMaxBytes int64
}

func LoadFromEnv() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("determine service user home directory: %w", err)
	}
	defaultStateDir := filepath.Join(home, ".local", "state", "control-agents")

	cfg := Config{
		BindAddr:         getEnv("CONTROL_AGENTS_BIND_ADDR", defaultBindAddr),
		Port:             defaultPort,
		Password:         os.Getenv("CONTROL_AGENTS_PASSWORD"),
		StateDir:         getEnv("CONTROL_AGENTS_STATE_DIR", defaultStateDir),
		HomeDir:          home,
		CookieSecure:     getBoolEnv("CONTROL_AGENTS_COOKIE_SECURE", true),
		CookieTTL:        defaultCookieTTL,
		MaxSessions:      defaultMaxSessions,
		SnapshotMaxBytes: defaultSnapshotMax,
	}
	cfg.TLSCertFile = getEnv("CONTROL_AGENTS_TLS_CERT_FILE", filepath.Join(cfg.StateDir, "certs", "server.crt"))
	cfg.TLSKeyFile = getEnv("CONTROL_AGENTS_TLS_KEY_FILE", filepath.Join(cfg.StateDir, "certs", "server.key"))
	cfg.AuthSecretFile = getEnv("CONTROL_AGENTS_AUTH_SECRET_FILE", filepath.Join(cfg.StateDir, "auth", "session.key"))

	if value := os.Getenv("CONTROL_AGENTS_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("CONTROL_AGENTS_PORT must be a number: %w", err)
		}
		cfg.Port = port
	}

	if value := os.Getenv("CONTROL_AGENTS_COOKIE_TTL_SECONDS"); value != "" {
		ttl, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("CONTROL_AGENTS_COOKIE_TTL_SECONDS must be a number: %w", err)
		}
		cfg.CookieTTL = ttl
	}

	if value := os.Getenv("CONTROL_AGENTS_MAX_SESSIONS"); value != "" {
		maximum, err := strconv.Atoi(value)
		if err != nil || maximum <= 0 {
			return Config{}, errors.New("CONTROL_AGENTS_MAX_SESSIONS must be a positive integer")
		}
		cfg.MaxSessions = maximum
	}

	if value := os.Getenv("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES"); value != "" {
		maximum, err := strconv.ParseInt(value, 10, 64)
		if err != nil || maximum <= 0 {
			return Config{}, errors.New("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES must be a positive integer")
		}
		cfg.SnapshotMaxBytes = maximum
	}

	if passwordFile := os.Getenv("CONTROL_AGENTS_PASSWORD_FILE"); passwordFile != "" {
		password, err := os.ReadFile(passwordFile)
		if err != nil {
			return Config{}, fmt.Errorf("read CONTROL_AGENTS_PASSWORD_FILE: %w", err)
		}
		cfg.Password = strings.TrimRight(string(password), "\r\n")
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.BindAddr) == "" {
		return errors.New("CONTROL_AGENTS_BIND_ADDR cannot be empty")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("CONTROL_AGENTS_PORT must be between 1 and 65535, got %d", c.Port)
	}
	if c.Password == "" {
		return errors.New("CONTROL_AGENTS_PASSWORD or CONTROL_AGENTS_PASSWORD_FILE is required")
	}
	if c.StateDir == "" {
		return errors.New("CONTROL_AGENTS_STATE_DIR cannot be empty")
	}
	if c.HomeDir == "" || !filepath.IsAbs(c.HomeDir) {
		return errors.New("service user home directory must be absolute")
	}
	if c.TLSCertFile == "" {
		return errors.New("CONTROL_AGENTS_TLS_CERT_FILE cannot be empty")
	}
	if c.TLSKeyFile == "" {
		return errors.New("CONTROL_AGENTS_TLS_KEY_FILE cannot be empty")
	}
	if c.AuthSecretFile == "" {
		return errors.New("CONTROL_AGENTS_AUTH_SECRET_FILE cannot be empty")
	}
	if c.CookieTTL <= 0 {
		return errors.New("CONTROL_AGENTS_COOKIE_TTL_SECONDS must be positive")
	}
	if c.MaxSessions <= 0 {
		return errors.New("CONTROL_AGENTS_MAX_SESSIONS must be a positive integer")
	}
	if c.SnapshotMaxBytes <= 0 {
		return errors.New("CONTROL_AGENTS_SNAPSHOT_MAX_BYTES must be a positive integer")
	}
	return nil
}

func (c Config) ListenAddress() string {
	return net.JoinHostPort(c.BindAddr, strconv.Itoa(c.Port))
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
