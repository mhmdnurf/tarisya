package core

import (
	"testing"
	"time"
)

func TestDurationEnvSupportsDays(t *testing.T) {
	t.Setenv("TARISYA_RETENTION_RAW", "7d")
	got, err := durationEnv("TARISYA_RETENTION_RAW", time.Hour)
	if err != nil || got != 7*24*time.Hour {
		t.Fatalf("durationEnv = %v, %v", got, err)
	}
}

func TestParseByteSize(t *testing.T) {
	got, err := parseByteSize("5GB")
	if err != nil || got != 5<<30 {
		t.Fatalf("parseByteSize = %d, %v", got, err)
	}
}

func TestRateLimitConfiguration(t *testing.T) {
	t.Setenv("TARISYA_DATABASE_URL", "file:test.db")
	t.Setenv("TARISYA_JWT_SECRET", "test-secret-that-is-at-least-32-characters")
	t.Setenv("TARISYA_AUTH_RATE_LIMIT_PER_MINUTE", "20")
	t.Setenv("TARISYA_AUTH_RATE_LIMIT_BURST", "7")
	t.Setenv("TARISYA_METRICS_RATE_LIMIT_PER_MINUTE", "900")
	t.Setenv("TARISYA_METRICS_RATE_LIMIT_BURST", "150")
	t.Setenv("TARISYA_ACTION_RATE_LIMIT_PER_MINUTE", "40")
	t.Setenv("TARISYA_ACTION_RATE_LIMIT_BURST", "12")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthRateLimitPerMinute != 20 || cfg.AuthRateLimitBurst != 7 ||
		cfg.MetricsRateLimitPerMinute != 900 || cfg.MetricsRateLimitBurst != 150 ||
		cfg.ActionRateLimitPerMinute != 40 || cfg.ActionRateLimitBurst != 12 {
		t.Fatalf("unexpected rate limit configuration: %+v", cfg)
	}
}

func TestRateLimitConfigurationRejectsNonPositiveValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TARISYA_AUTH_RATE_LIMIT_PER_MINUTE", value)
			if _, err := positiveIntEnv("TARISYA_AUTH_RATE_LIMIT_PER_MINUTE", 10); err == nil {
				t.Fatalf("positiveIntEnv accepted %q", value)
			}
		})
	}
}
