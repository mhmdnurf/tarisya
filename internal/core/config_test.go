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
