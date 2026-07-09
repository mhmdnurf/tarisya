package core

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address           string
	DatabaseURL       string
	BootstrapServerID string
	BootstrapAPIKey   string
	ShutdownTimeout   time.Duration
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Address:           coreEnvOrDefault("TARISYA_CORE_ADDRESS", ":8080"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("TARISYA_DATABASE_URL")),
		BootstrapServerID: strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_SERVER_ID")),
		BootstrapAPIKey:   strings.TrimSpace(os.Getenv("TARISYA_BOOTSTRAP_API_KEY")),
		ShutdownTimeout:   10 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("TARISYA_DATABASE_URL is required")
	}
	if (cfg.BootstrapServerID == "") != (cfg.BootstrapAPIKey == "") {
		return Config{}, errors.New("TARISYA_BOOTSTRAP_SERVER_ID and TARISYA_BOOTSTRAP_API_KEY must be set together")
	}
	return cfg, nil
}

func coreEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
