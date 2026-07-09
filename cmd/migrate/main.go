package main

import (
	"flag"
	"log/slog"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/mhmdnurf/tarisya/internal/core"
)

func main() {
	_ = godotenv.Load()
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()

	databaseURL := strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL"))
	if databaseURL == "" {
		slog.Error("invalid configuration", "error", "TARISYA_DATABASE_URL is required")
		os.Exit(1)
	}
	if err := core.RunMigration(databaseURL, *direction); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migration completed", "direction", *direction)
}
