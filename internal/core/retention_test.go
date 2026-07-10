package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(context.Background(), "file:"+filepath.Join(t.TempDir(), "tarisya.db")+"?_pragma=foreign_keys(1)", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestMaintenanceDownsamplesAndExpiresMetrics(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, timestamp := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(-8*24*time.Hour + 15*time.Second), now.Add(-31 * 24 * time.Hour)} {
		err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: timestamp, Metrics: metrics.Values{CPUUsage: 10, MemoryUsage: 20, DiskUsage: 30, LoadAverage: 1, UptimeSeconds: 5}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Maintain(ctx, MaintenanceConfig{RawRetention: 7 * 24 * time.Hour, FiveMinuteRetention: 30 * 24 * time.Hour, AggregatedRetention: 90 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	var raw, fiveMinute, oneHour int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM metrics_5m").Scan(&fiveMinute); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM metrics_1h").Scan(&oneHour); err != nil {
		t.Fatal(err)
	}
	if raw != 0 || fiveMinute != 1 || oneHour != 1 {
		t.Fatalf("tier counts raw=%d 5m=%d 1h=%d, want 0,1,1", raw, fiveMinute, oneHour)
	}
}

func TestSaveMetricsRejectsFullDatabase(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.maxSize = 1
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: time.Now(), Metrics: metrics.Values{}})
	if err != ErrDatabaseFull {
		t.Fatalf("SaveMetrics error = %v, want %v", err, ErrDatabaseFull)
	}
}
