package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"terminal-mirror/internal/cert"
	"terminal-mirror/internal/config"
	"terminal-mirror/internal/server"
	"terminal-mirror/internal/version"
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
		logger.Error("failed to create server", "error", err)
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
	}

	errCh := make(chan error, 1)
	go func() {
		build := version.Current()
		logger.Info("terminal mirror listening", "scheme", "https", "addr", cfg.ListenAddress(), "state_dir", cfg.StateDir, "tls_cert", cfg.TLSCertFile, "version", build.Version, "commit", build.Commit, "build_date", build.BuildDate)
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
