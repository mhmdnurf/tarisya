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

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/buildinfo"
	"github.com/mhmdnurf/tarisya/internal/core"
)

func main() {
	if buildinfo.IsVersionCommand(os.Args[1:]) {
		fmt.Fprintln(os.Stdout, buildinfo.String("tarisya-core"))
		return
	}

	_ = godotenv.Load()

	cfg, err := core.LoadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := core.OpenStore(ctx, cfg.DatabaseURL, cfg.MaxDatabaseSize)
	if err != nil {
		slog.Error("could not initialize database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		slog.Error("could not migrate database", "error", err)
		os.Exit(1)
	}
	maintenanceCfg := core.MaintenanceConfig{
		RawRetention: cfg.RetentionRaw, FiveMinuteRetention: cfg.Retention5m,
		AggregatedRetention: cfg.RetentionAggregated,
	}
	runMaintenance := func() {
		if err := store.Maintain(ctx, maintenanceCfg); err != nil {
			slog.Error("database maintenance failed", "error", err)
			return
		}
		size, ratio, err := store.DatabaseUsage(ctx)
		if err != nil {
			slog.Error("could not read database size", "error", err)
			return
		}
		if ratio >= cfg.DatabaseWarningThreshold {
			slog.Warn("database size is nearing its limit", "size_bytes", size, "usage_ratio", ratio)
		}
	}
	runMaintenance()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runMaintenance()
			}
		}
	}()
	if cfg.BootstrapEmail != "" {
		passwordHash, err := core.HashPassword(cfg.BootstrapPassword)
		if err != nil {
			slog.Error("could not hash bootstrap password", "error", err)
			os.Exit(1)
		}
		if err := store.Bootstrap(
			ctx,
			cfg.BootstrapName,
			cfg.BootstrapEmail,
			passwordHash,
			cfg.BootstrapServerID,
			cfg.BootstrapAPIKey,
		); err != nil {
			slog.Error("could not bootstrap development data", "error", err)
			os.Exit(1)
		}
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           core.NewHandler(store, cfg),
		ReadHeaderTimeout: cfg.ShutdownTimeout,
	}
	go func() {
		slog.Info("Tarisya Core started", "address", cfg.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("could not gracefully stop HTTP server", "error", err)
	}
	slog.Info("Tarisya Core stopped")
}
