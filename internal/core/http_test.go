package core

import (
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

func TestValidatePayload(t *testing.T) {
	valid := MetricsPayload{
		ServerID:  "srv_01",
		Timestamp: time.Now(),
		Metrics: metrics.Values{
			CPUUsage:    10,
			MemoryUsage: 20,
			DiskUsage:   30,
			LoadAverage: 1,
		},
	}
	if err := validatePayload(valid); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	invalid := valid
	invalid.Metrics.CPUUsage = 101
	if err := validatePayload(invalid); err == nil {
		t.Fatal("usage above 100 should be rejected")
	}
}

func TestBearerToken(t *testing.T) {
	token, ok := bearerToken("Bearer secret")
	if !ok || token != "secret" {
		t.Fatalf("bearerToken returned %q, %v", token, ok)
	}
	if _, ok := bearerToken("secret"); ok {
		t.Fatal("token without scheme should be rejected")
	}
}
