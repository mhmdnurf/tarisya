package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/agent"
	"github.com/mhmdnurf/tarisya/internal/config"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := agent.New(cfg, nil)
	slog.Info("Tarisya Agent started",
		"server_id", cfg.ServerID,
		"endpoint", cfg.Endpoint,
		"interval", cfg.Interval,
	)

	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}

	slog.Info("Tarisya Agent stopped")
}
