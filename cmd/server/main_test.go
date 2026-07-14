package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"control-agents/internal/server"
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

func TestStartupReconciliationLogOmitsLifecycleErrorCanary(t *testing.T) {
	const canary = "STARTUP-RECONCILIATION-CANARY-/private/state/session-alpha"
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	logServerCreationFailure(logger, errors.Join(server.ErrStartupReconciliation, errors.New(canary)))

	logged := logs.String()
	if strings.Contains(logged, canary) || strings.Contains(logged, `"error"`) {
		t.Fatalf("startup reconciliation log leaked lifecycle error: %s", logged)
	}
	if !strings.Contains(logged, `"reason_code":"reconciliation_failure"`) {
		t.Fatalf("startup reconciliation log lacks approved reason code: %s", logged)
	}
}
