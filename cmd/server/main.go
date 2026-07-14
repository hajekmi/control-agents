package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"control-agents/internal/cert"
	"control-agents/internal/config"
	"control-agents/internal/server"
	"control-agents/internal/version"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("control-agents-server %s\n", version.String())
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.LoadFromEnv()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(2)
	}

	app, err := server.New(cfg, logger)
	if err != nil {
		logServerCreationFailure(logger, err)
		os.Exit(1)
	}
	if err := cert.EnsureSelfSignedECC(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.BindAddr); err != nil {
		logger.Error("failed to prepare TLS certificate", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         tls13OnlyConfig(),
	}

	errCh := make(chan error, 1)
	go func() {
		build := version.Current()
		logger.Info("control agents listening", "scheme", "https", "addr", cfg.ListenAddress(), "state_dir", cfg.StateDir, "tls_cert", cfg.TLSCertFile, "version", build.Version, "commit", build.Commit, "build_date", build.BuildDate)
		errCh <- httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown failed", "error", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}

func logServerCreationFailure(logger *slog.Logger, err error) {
	logger.Error("failed to create server", "reason_code", server.StartupFailureReason(err))
}

func tls13OnlyConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
	}
}
