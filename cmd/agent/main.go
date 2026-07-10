package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/agent"
	"github.com/mhmdnurf/tarisya/internal/buildinfo"
	"github.com/mhmdnurf/tarisya/internal/config"
)

func main() {
	if buildinfo.IsVersionCommand(os.Args[1:]) {
		fmt.Fprintln(os.Stdout, buildinfo.String("tarisya-agent"))
		return
	}

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
