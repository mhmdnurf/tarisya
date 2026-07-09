package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/core"
)

func main() {
	_ = godotenv.Load()

	cfg, err := core.LoadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := core.OpenStore(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("could not initialize database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		slog.Error("could not migrate database", "error", err)
		os.Exit(1)
	}
	if err := store.BootstrapServer(ctx, cfg.BootstrapServerID, cfg.BootstrapAPIKey); err != nil {
		slog.Error("could not bootstrap server", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           core.NewHandler(store),
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
