package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint = "http://localhost:8081/api/v1/metrics"
	defaultInterval = 15 * time.Second
	defaultTimeout  = 10 * time.Second
)

type Config struct {
	ServerID string
	APIKey   string
	Endpoint string
	Interval time.Duration
	Timeout  time.Duration
	DiskPath string
}

func Load() (Config, error) {
	cfg := Config{
		ServerID: strings.TrimSpace(os.Getenv("TARISYA_SERVER_ID")),
		APIKey:   strings.TrimSpace(os.Getenv("TARISYA_API_KEY")),
		Endpoint: envOrDefault("TARISYA_CORE_URL", defaultEndpoint),
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
		DiskPath: envOrDefault("TARISYA_DISK_PATH", "/"),
	}

	var err error
	if value := os.Getenv("TARISYA_INTERVAL"); value != "" {
		cfg.Interval, err = parseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("TARISYA_INTERVAL: %w", err)
		}
	}
	if value := os.Getenv("TARISYA_HTTP_TIMEOUT"); value != "" {
		cfg.Timeout, err = parseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("TARISYA_HTTP_TIMEOUT: %w", err)
		}
	}

	if cfg.ServerID == "" {
		return Config{}, errors.New("TARISYA_SERVER_ID is required")
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("TARISYA_API_KEY is required")
	}
	if cfg.Interval < time.Second {
		return Config{}, errors.New("TARISYA_INTERVAL must be at least 1s")
	}
	if cfg.Timeout <= 0 {
		return Config{}, errors.New("TARISYA_HTTP_TIMEOUT must be greater than zero")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseDuration(value string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return time.ParseDuration(value)
}
