package core

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

var (
	testMaintenanceNow = time.Date(2026, 7, 11, 12, 2, 30, 0, time.UTC)
	testMaintenanceCfg = MaintenanceConfig{
		RawRetention:        7 * 24 * time.Hour,
		FiveMinuteRetention: 30 * 24 * time.Hour,
		AggregatedRetention: 90 * 24 * time.Hour,
	}
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
	now := testMaintenanceNow
	for _, timestamp := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(-8*24*time.Hour + 15*time.Second), now.Add(-31 * 24 * time.Hour)} {
		err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: timestamp, Metrics: metrics.Values{CPUUsage: 10, MemoryUsage: 20, DiskUsage: 30, LoadAverage: 1, UptimeSeconds: 5}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.maintainAt(ctx, testMaintenanceCfg, now); err != nil {
		t.Fatal(err)
	}
	if err := store.maintainAt(ctx, testMaintenanceCfg, now); err != nil {
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
	var fiveMinuteSamples, oneHourSamples int
	if err := store.db.QueryRow("SELECT sample_count FROM metrics_5m").Scan(&fiveMinuteSamples); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT sample_count FROM metrics_1h").Scan(&oneHourSamples); err != nil {
		t.Fatal(err)
	}
	if fiveMinuteSamples != 2 || oneHourSamples != 1 {
		t.Fatalf("sample counts 5m=%d 1h=%d, want 2,1", fiveMinuteSamples, oneHourSamples)
	}
}

func TestMaintenanceProcessesOnlyCompleteBuckets(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}

	rawCutoff := time.UnixMilli(completedBucketCutoff(testMaintenanceNow.Add(-testMaintenanceCfg.RawRetention), rawMetricsBucket)).UTC()
	for _, timestamp := range []time.Time{rawCutoff.Add(-time.Second), rawCutoff.Add(time.Second)} {
		if err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: timestamp, Metrics: metrics.Values{CPUUsage: 10}}); err != nil {
			t.Fatal(err)
		}
	}

	fiveMinuteCutoff := completedBucketCutoff(testMaintenanceNow.Add(-testMaintenanceCfg.FiveMinuteRetention), aggregatedMetricsBucket)
	insertAggregate(t, store, "metrics_5m", fiveMinuteCutoff-int64(5*time.Minute/time.Millisecond))
	insertAggregate(t, store, "metrics_5m", fiveMinuteCutoff)
	aggregatedCutoff := completedBucketCutoff(testMaintenanceNow.Add(-testMaintenanceCfg.AggregatedRetention), aggregatedMetricsBucket)
	insertAggregate(t, store, "metrics_1h", aggregatedCutoff-int64(time.Hour/time.Millisecond))
	insertAggregate(t, store, "metrics_1h", aggregatedCutoff)

	if err := store.maintainAt(ctx, testMaintenanceCfg, testMaintenanceNow); err != nil {
		t.Fatal(err)
	}

	assertRowCount(t, store, "metrics", "collected_at >= ?", unixMillis(rawCutoff), 1)
	assertRowCount(t, store, "metrics_5m", "bucket_start = ?", fiveMinuteCutoff, 1)
	assertRowCount(t, store, "metrics_1h", "bucket_start = ?", aggregatedCutoff, 1)
	assertRowCount(t, store, "metrics_1h", "bucket_start = ?", aggregatedCutoff-int64(time.Hour/time.Millisecond), 0)
}

func TestMaintenanceRollsBackAggregationWhenSourceDeletionFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	rawCutoff := time.UnixMilli(completedBucketCutoff(testMaintenanceNow.Add(-testMaintenanceCfg.RawRetention), rawMetricsBucket)).UTC()
	if err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: rawCutoff.Add(-time.Second), Metrics: metrics.Values{CPUUsage: 10}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_raw_metric_delete BEFORE DELETE ON metrics BEGIN SELECT RAISE(ABORT, 'forced delete failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := store.maintainAt(ctx, testMaintenanceCfg, testMaintenanceNow); err == nil {
		t.Fatal("maintenance succeeded despite forced source deletion failure")
	}
	assertRowCount(t, store, "metrics", "1=1", nil, 1)
	assertRowCount(t, store, "metrics_5m", "1=1", nil, 0)
}

func insertAggregate(t *testing.T, store *Store, table string, bucketStart int64) {
	t.Helper()
	query := fmt.Sprintf(`INSERT INTO %s (
		server_id,bucket_start,cpu_avg,cpu_min,cpu_max,memory_avg,memory_min,memory_max,
		disk_avg,disk_min,disk_max,load_avg,load_min,load_max,uptime_max,sample_count
	) VALUES (?, ?, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)`, table)
	if _, err := store.db.Exec(query, "srv_test", bucketStart); err != nil {
		t.Fatal(err)
	}
}

func assertRowCount(t *testing.T, store *Store, table, condition string, argument any, want int) {
	t.Helper()
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", table, condition)
	var row *sql.Row
	if argument == nil {
		row = store.db.QueryRow(query)
	} else {
		row = store.db.QueryRow(query, argument)
	}
	var got int
	if err := row.Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s rows matching %q = %d, want %d", table, condition, got, want)
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
