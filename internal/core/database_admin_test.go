package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
)

func TestCheckDatabase(t *testing.T) {
	store := newTestStore(t)
	path, err := mainDatabasePath(context.Background(), store.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckDatabase(context.Background(), "file:"+path); err != nil {
		t.Fatalf("CheckDatabase returned %v", err)
	}
}

func TestBackupDatabaseIncludesWALAndProducesChecksum(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "source.db")
	databaseURL := "file:" + databasePath
	store, err := OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, "Test User", "user@example.com", "hash", "srv_test", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMetrics(ctx, MetricsPayload{ServerID: "srv_test", Hostname: "host", AgentVersion: "v1", Timestamp: time.Now(), Metrics: metrics.Values{CPUUsage: 42}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(databasePath + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("WAL file is unavailable or empty: info=%v error=%v", info, err)
	}

	outputPath := filepath.Join(directory, "backup.db")
	result, err := BackupDatabase(ctx, databaseURL, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != outputPath || result.ChecksumPath != outputPath+".sha256" {
		t.Fatalf("backup result paths = %#v", result)
	}
	for _, path := range []string{result.Path, result.ChecksumPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions = %o, want 600", path, info.Mode().Perm())
		}
	}
	digest, size, err := checksumFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if result.SHA256 != digest || result.Size != size {
		t.Fatalf("backup checksum result = %#v, actual digest=%s size=%d", result, digest, size)
	}
	checksumContents, err := os.ReadFile(result.ChecksumPath)
	if err != nil {
		t.Fatal(err)
	}
	wantChecksum := result.SHA256 + "  " + filepath.Base(result.Path) + "\n"
	if string(checksumContents) != wantChecksum {
		t.Fatalf("checksum contents = %q, want %q", checksumContents, wantChecksum)
	}

	backupDB, err := sql.Open("sqlite", sqliteReadOnlyURL(result.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var count int
	if err := backupDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics WHERE server_id='srv_test'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backed-up metrics = %d, want 1", count)
	}
}

func TestBackupDatabaseRefusesToOverwrite(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databaseURL := "file:" + filepath.Join(directory, "source.db")
	store, err := OpenStore(ctx, databaseURL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()

	outputPath := filepath.Join(directory, "existing.db")
	if err := os.WriteFile(outputPath, []byte("do not overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BackupDatabase(ctx, databaseURL, outputPath); err == nil {
		t.Fatal("BackupDatabase overwrote an existing destination")
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "do not overwrite" {
		t.Fatalf("existing output was changed to %q", contents)
	}
	if _, err := os.Stat(outputPath + ".sha256"); !os.IsNotExist(err) {
		t.Fatalf("unexpected checksum file error = %v", err)
	}
}

func TestRestoreDatabaseReplacesTargetAndKeepsPreRestoreBackup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "target.db")
	backupSourcePath := filepath.Join(directory, "backup-source.db")
	targetStore := createDatabaseWithServer(t, targetPath, "srv_target", 10)
	targetStore.Close()
	if err := os.Chmod(targetPath, 0o640); err != nil {
		t.Fatal(err)
	}
	backupSource := createDatabaseWithServer(t, backupSourcePath, "srv_backup", 42)
	backupPath := filepath.Join(directory, "restore.db")
	if _, err := BackupDatabase(ctx, "file:"+backupSourcePath, backupPath); err != nil {
		t.Fatal(err)
	}
	backupSource.Close()

	result, err := RestoreDatabase(ctx, "file:"+targetPath, backupPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChecksumVerified {
		t.Fatal("restore did not verify the available checksum")
	}
	for _, path := range []string{result.PreRestoreBackup, result.PreRestoreChecksum} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("pre-restore artifact %s: %v", path, err)
		}
	}
	if info, err := os.Stat(targetPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored database permissions = %o, want 640", info.Mode().Perm())
	}

	assertDatabaseServer(t, targetPath, "srv_backup", 1)
	assertDatabaseServer(t, targetPath, "srv_target", 0)
	assertDatabaseServer(t, result.PreRestoreBackup, "srv_target", 1)
	assertDatabaseServer(t, result.PreRestoreBackup, "srv_backup", 0)
}

func TestRestoreDatabaseRefusesRunningCore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "target.db")
	targetStore := createDatabaseWithServer(t, targetPath, "srv_target", 10)
	t.Cleanup(targetStore.Close)
	backupSourcePath := filepath.Join(directory, "backup-source.db")
	backupSource := createDatabaseWithServer(t, backupSourcePath, "srv_backup", 42)
	backupPath := filepath.Join(directory, "restore.db")
	if _, err := BackupDatabase(ctx, "file:"+backupSourcePath, backupPath); err != nil {
		t.Fatal(err)
	}
	backupSource.Close()

	_, err := RestoreDatabase(ctx, "file:"+targetPath, backupPath, "")
	if !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("RestoreDatabase error = %v, want ErrDatabaseInUse", err)
	}
	assertDatabaseServer(t, targetPath, "srv_target", 1)
}

func TestRestoreDatabaseRejectsChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "target.db")
	createDatabaseWithServer(t, targetPath, "srv_target", 10).Close()
	backupSourcePath := filepath.Join(directory, "backup-source.db")
	backupSource := createDatabaseWithServer(t, backupSourcePath, "srv_backup", 42)
	backupPath := filepath.Join(directory, "restore.db")
	if _, err := BackupDatabase(ctx, "file:"+backupSourcePath, backupPath); err != nil {
		t.Fatal(err)
	}
	backupSource.Close()
	if err := os.WriteFile(backupPath+".sha256", []byte(strings.Repeat("0", 64)+"  restore.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreDatabase(ctx, "file:"+targetPath, backupPath, ""); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("RestoreDatabase error = %v, want checksum mismatch", err)
	}
	assertDatabaseServer(t, targetPath, "srv_target", 1)
	matches, err := filepath.Glob(targetPath + ".pre-restore-*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("pre-restore artifacts = %v, %v; want none", matches, err)
	}
}

func TestRestoreDatabaseRejectsInvalidBackupsBeforeChangingTarget(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(t *testing.T, path string)
	}{
		{
			name: "corrupt",
			build: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unsupported schema",
			build: func(t *testing.T, path string) {
				t.Helper()
				store := createDatabaseWithServer(t, path, "srv_backup", 42)
				if _, err := store.db.Exec("UPDATE schema_migrations SET version=2"); err != nil {
					t.Fatal(err)
				}
				store.Close()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			targetPath := filepath.Join(directory, "target.db")
			createDatabaseWithServer(t, targetPath, "srv_target", 10).Close()
			backupPath := filepath.Join(directory, "invalid-backup.db")
			test.build(t, backupPath)

			if _, err := RestoreDatabase(context.Background(), "file:"+targetPath, backupPath, ""); err == nil {
				t.Fatal("RestoreDatabase accepted an invalid backup")
			}
			assertDatabaseServer(t, targetPath, "srv_target", 1)
			matches, err := filepath.Glob(targetPath + ".pre-restore-*")
			if err != nil || len(matches) != 0 {
				t.Fatalf("pre-restore artifacts = %v, %v; want none", matches, err)
			}
		})
	}
}

func TestRestoreDatabaseDoesNotCreateMissingTarget(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	backupSourcePath := filepath.Join(directory, "backup-source.db")
	backupSource := createDatabaseWithServer(t, backupSourcePath, "srv_backup", 42)
	backupPath := filepath.Join(directory, "restore.db")
	if _, err := BackupDatabase(ctx, "file:"+backupSourcePath, backupPath); err != nil {
		t.Fatal(err)
	}
	backupSource.Close()
	targetPath := filepath.Join(directory, "missing.db")

	if _, err := RestoreDatabase(ctx, "file:"+targetPath, backupPath, ""); err == nil {
		t.Fatal("RestoreDatabase created a missing target")
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("missing target exists after restore attempt: %v", err)
	}
}

func createDatabaseWithServer(t *testing.T, path, serverID string, cpu float64) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := OpenStore(ctx, "file:"+path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, "Test User", serverID+"@example.com", "hash", serverID, "key-"+serverID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.SaveMetrics(ctx, MetricsPayload{ServerID: serverID, Hostname: "host", AgentVersion: "v1", Timestamp: time.Now(), Metrics: metrics.Values{CPUUsage: cpu}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store
}

func assertDatabaseServer(t *testing.T, path, serverID string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteReadOnlyURL(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM servers WHERE id=?", serverID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("server %s count in %s = %d, want %d", serverID, path, got, want)
	}
}

func TestCheckDatabaseDetectsForeignKeyViolation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	path, err := mainDatabasePath(ctx, store.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO metrics (server_id,cpu_usage,memory_usage,disk_usage,load_average,uptime_seconds,collected_at,created_at) VALUES ('missing',1,1,1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}

	err = CheckDatabase(ctx, "file:"+path)
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("CheckDatabase error = %v, want foreign key failure", err)
	}
}

func TestCheckDatabaseRejectsNonTarisyaDatabase(t *testing.T) {
	err := CheckDatabase(context.Background(), "file:"+t.TempDir()+"/empty.db")
	if err == nil || !strings.Contains(err.Error(), "not a Tarisya database") {
		t.Fatalf("CheckDatabase error = %v, want schema error", err)
	}
}
