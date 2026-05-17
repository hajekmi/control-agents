package main

import (
	"crypto/tls"
	"testing"
)

func TestTLSConfigAllowsOnlyTLS13(t *testing.T) {
	cfg := tls13OnlyConfig()
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %x, want TLS 1.3", cfg.MinVersion)
	}
	if cfg.MaxVersion != tls.VersionTLS13 {
		t.Fatalf("MaxVersion = %x, want TLS 1.3", cfg.MaxVersion)
	}
}
