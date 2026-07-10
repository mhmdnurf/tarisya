package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
	dbmigrations "github.com/mhmdnurf/tarisya/migrations"
)

func TestDatabasePragmas(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	for _, pragma := range []struct {
		name string
		want int
	}{{"foreign_keys", 1}, {"busy_timeout", 5000}} {
		var got int
		if err := store.db.QueryRowContext(ctx, "PRAGMA "+pragma.name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != pragma.want {
			t.Fatalf("%s = %d, want %d", pragma.name, got, pragma.want)
		}
	}
}

func TestAutomaticMigrationOnEmptyDatabaseCreatesMetricsIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "empty.db")
	store, err := OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var version int
	if err := store.db.QueryRowContext(ctx, "SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("schema version = %d, want 1", version)
	}
	if !indexExists(t, store.db, "metrics", "metrics_server_collected_idx") {
		t.Fatal("metrics_server_collected_idx is missing")
	}
}

func TestRestartAfterAbruptExitRecoversWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	store, err := OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestDatabaseCrashWriterProcess$")
	cmd.Env = append(os.Environ(), "TARISYA_CRASH_WRITER_DB="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash writer: %v: %s", err, output)
	}

	store, err = OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("metrics after abrupt restart = %d, want 1", count)
	}
	var integrity string
	if err := store.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

func TestDatabaseCrashWriterProcess(t *testing.T) {
	path := os.Getenv("TARISYA_CRASH_WRITER_DB")
	if path == "" {
		return
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO metrics (server_id,cpu_usage,memory_usage,disk_usage,load_average,uptime_seconds,collected_at,created_at) VALUES ('srv_test',1,2,3,4,5,6,7)`); err != nil {
		t.Fatal(err)
	}
	os.Exit(0) // Simulate a process that cannot run its shutdown handlers.
}

func TestLegacyDatabaseIsBackedUpBeforeMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := openRawDatabase(t, path)
	for _, statement := range dbmigrations.Statements(dbmigrations.InitialSchema()) {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (name,email,password_hash,created_at,updated_at) VALUES ('Legacy User','legacy@example.com','hash',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := store.db.QueryRowContext(ctx, "SELECT name FROM users WHERE email = 'legacy@example.com'").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Legacy User" {
		t.Fatalf("legacy user = %q", name)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup files = %v, %v; want one backup", backups, err)
	}
}

func TestFailedMigrationLeavesDatabaseAtPreviousVersionAndKeepsBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken-legacy.db")
	db := openRawDatabase(t, path)
	if _, err := db.Exec("CREATE TABLE servers (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err == nil {
		t.Fatal("Migrate succeeded, want failure")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("applied migration count = %d, want 0", count)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup files = %v, %v; want one backup", backups, err)
	}
}

func TestReadOnlyDatabaseFailsDuringInitialization(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "readonly.db")
	db := openRawDatabase(t, path)
	if _, err := db.Exec("CREATE TABLE legacy_data (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(ctx, "file:"+path+"?mode=ro", 0)
	if err == nil {
		t.Cleanup(store.Close)
		err = store.Migrate(ctx)
	}
	if err == nil {
		t.Fatal("database initialization succeeded for a read-only database")
	}
}

func TestMaintenanceRetention(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: time.Now().Add(-8 * 24 * time.Hour), Metrics: metrics.Values{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Maintain(ctx, MaintenanceConfig{RawRetention: 7 * 24 * time.Hour, FiveMinuteRetention: 30 * 24 * time.Hour, AggregatedRetention: 90 * 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	var raw, aggregated int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*), (SELECT COUNT(*) FROM metrics_5m) FROM metrics").Scan(&raw, &aggregated); err != nil {
		t.Fatal(err)
	}
	if raw != 0 || aggregated != 1 {
		t.Fatalf("retention counts raw=%d aggregated=%d, want 0,1", raw, aggregated)
	}
}

func openRawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func indexExists(t *testing.T, db *sql.DB, table, want string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA index_list(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			t.Fatal(err)
		}
		if name == want {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestConfiguredDatabaseSizeLimitRejectsWrites(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.maxSize = 1
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: time.Now(), Metrics: metrics.Values{}})
	if !errors.Is(err, ErrDatabaseFull) {
		t.Fatalf("SaveMetrics error = %v, want %v", err, ErrDatabaseFull)
	}
}

func TestSQLiteDiskFullIsReportedAsDatabaseFull(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	var pageCount int
	if err := store.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA max_page_count = "+strconv.Itoa(pageCount)); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10_000; i++ {
		err := store.SaveMetrics(ctx, MetricsPayload{
			ServerID:     "srv_test",
			Hostname:     "host",
			AgentVersion: "v1",
			Timestamp:    time.UnixMilli(int64(i)),
			Metrics:      metrics.Values{CPUUsage: float64(i)},
		})
		if errors.Is(err, ErrDatabaseFull) {
			return
		}
		if err != nil {
			t.Fatalf("SaveMetrics returned %v, want ErrDatabaseFull", err)
		}
	}
	t.Fatal("database did not become full")
}
