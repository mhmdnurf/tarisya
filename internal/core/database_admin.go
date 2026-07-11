package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BackupResult struct {
	Path         string
	ChecksumPath string
	SHA256       string
	Size         int64
}

type RestoreResult struct {
	DatabasePath       string
	PreRestoreBackup   string
	PreRestoreChecksum string
	ChecksumVerified   bool
}

// CheckDatabase verifies SQLite's structural integrity and foreign-key
// consistency without modifying application data.
func CheckDatabase(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	lifecycleLock, err := acquireSharedDatabaseLock(ctx, db)
	if err != nil {
		return fmt.Errorf("lock database lifecycle: %w", err)
	}
	defer lifecycleLock.release()
	return checkOpenDatabase(ctx, db)
}

func checkOpenDatabase(ctx context.Context, db *sql.DB) error {
	var hasMigrationTable bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_migrations')`).Scan(&hasMigrationTable); err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if !hasMigrationTable {
		return errors.New("not a Tarisya database: schema_migrations table is missing")
	}
	var schemaVersion sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&schemaVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if !schemaVersion.Valid || schemaVersion.Int64 != 1 {
		return fmt.Errorf("unsupported Tarisya schema version: %v", schemaVersion)
	}
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	defer rows.Close()
	checked := false
	for rows.Next() {
		checked = true
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("read integrity check: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("database integrity check failed: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read integrity check: %w", err)
	}
	if !checked {
		return errors.New("database integrity check returned no result")
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close integrity check: %w", err)
	}

	foreignKeys, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("run foreign key check: %w", err)
	}
	defer foreignKeys.Close()
	if foreignKeys.Next() {
		var table, parent string
		var rowID int64
		var foreignKeyID int
		if err := foreignKeys.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return fmt.Errorf("read foreign key check: %w", err)
		}
		return fmt.Errorf("foreign key check failed: table=%s rowid=%d parent=%s constraint=%d", table, rowID, parent, foreignKeyID)
	}
	if err := foreignKeys.Err(); err != nil {
		return fmt.Errorf("read foreign key check: %w", err)
	}
	return nil
}

// BackupDatabase creates a consistent online SQLite snapshot and its SHA-256
// sidecar. Existing output files are never overwritten.
func BackupDatabase(ctx context.Context, databaseURL, outputPath string) (BackupResult, error) {
	return backupDatabaseAt(ctx, databaseURL, outputPath, time.Now().UTC())
}

func backupDatabaseAt(ctx context.Context, databaseURL, outputPath string, now time.Time) (result BackupResult, err error) {
	return backupDatabase(ctx, databaseURL, outputPath, now, true)
}

func backupDatabase(ctx context.Context, databaseURL, outputPath string, now time.Time, acquireLock bool) (result BackupResult, err error) {
	if outputPath == "" {
		outputPath = fmt.Sprintf("tarisya-%s.db", now.UTC().Format("20060102T150405Z"))
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("resolve backup path: %w", err)
	}
	checksumPath := outputPath + ".sha256"

	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return BackupResult{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return BackupResult{}, fmt.Errorf("connect database: %w", err)
	}
	if acquireLock {
		lifecycleLock, err := acquireSharedDatabaseLock(ctx, db)
		if err != nil {
			return BackupResult{}, fmt.Errorf("lock database lifecycle: %w", err)
		}
		defer lifecycleLock.release()
	}
	if err := checkOpenDatabase(ctx, db); err != nil {
		return BackupResult{}, fmt.Errorf("check source database: %w", err)
	}
	sourcePath, err := mainDatabasePath(ctx, db)
	if err != nil {
		return BackupResult{}, fmt.Errorf("resolve source database: %w", err)
	}
	if sourcePath != "" && sourcePath != ":memory:" {
		sourcePath, err = filepath.Abs(sourcePath)
		if err != nil {
			return BackupResult{}, fmt.Errorf("resolve source database: %w", err)
		}
		if filepath.Clean(sourcePath) == filepath.Clean(outputPath) {
			return BackupResult{}, errors.New("backup output must differ from the source database")
		}
	}

	if err := reserveBackupFile(outputPath); err != nil {
		return BackupResult{}, fmt.Errorf("reserve backup output: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(outputPath)
			_ = os.Remove(checksumPath)
		}
	}()
	if err := reserveBackupFile(checksumPath); err != nil {
		return BackupResult{}, fmt.Errorf("reserve checksum output: %w", err)
	}

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", outputPath); err != nil {
		return BackupResult{}, fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err := os.Chmod(outputPath, 0o600); err != nil {
		return BackupResult{}, fmt.Errorf("secure backup permissions: %w", err)
	}
	if err := syncFile(outputPath); err != nil {
		return BackupResult{}, fmt.Errorf("sync backup: %w", err)
	}
	if err := checkDatabaseFile(ctx, outputPath); err != nil {
		return BackupResult{}, fmt.Errorf("validate backup: %w", err)
	}

	digest, size, err := checksumFile(outputPath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("checksum backup: %w", err)
	}
	checksum, err := os.OpenFile(checksumPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return BackupResult{}, fmt.Errorf("write checksum: %w", err)
	}
	if _, err = fmt.Fprintf(checksum, "%s  %s\n", digest, filepath.Base(outputPath)); err == nil {
		err = checksum.Sync()
	}
	closeErr := checksum.Close()
	if err != nil {
		return BackupResult{}, fmt.Errorf("write checksum: %w", err)
	}
	if closeErr != nil {
		return BackupResult{}, fmt.Errorf("close checksum: %w", closeErr)
	}
	if err := os.Chmod(checksumPath, 0o600); err != nil {
		return BackupResult{}, fmt.Errorf("secure checksum permissions: %w", err)
	}

	cleanup = false
	return BackupResult{Path: outputPath, ChecksumPath: checksumPath, SHA256: digest, Size: size}, nil
}

func reserveBackupFile(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func checksumFile(path string) (digest string, size int64, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err = io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func sqliteReadOnlyURL(path string) string {
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := location.Query()
	query.Set("mode", "ro")
	location.RawQuery = query.Encode()
	return location.String()
}

func sqliteExistingURL(databaseURL string) (string, error) {
	if databaseURL == "" || databaseURL == ":memory:" || strings.Contains(databaseURL, "mode=memory") {
		return "", errors.New("restore requires an existing file-backed target database")
	}
	var location *url.URL
	var err error
	if strings.HasPrefix(databaseURL, "file:") {
		location, err = url.Parse(databaseURL)
		if err != nil {
			return "", fmt.Errorf("parse target database URL: %w", err)
		}
	} else {
		location = &url.URL{Scheme: "file", Path: filepath.ToSlash(databaseURL)}
	}
	query := location.Query()
	query.Set("mode", "rw")
	location.RawQuery = query.Encode()
	return location.String(), nil
}

// RestoreDatabase validates and atomically replaces a stopped Core database.
// It always creates a verified pre-restore backup before changing the target.
func RestoreDatabase(ctx context.Context, databaseURL, backupPath, checksumPath string) (RestoreResult, error) {
	return restoreDatabaseAt(ctx, databaseURL, backupPath, checksumPath, time.Now().UTC())
}

func restoreDatabaseAt(ctx context.Context, databaseURL, backupPath, checksumPath string, now time.Time) (RestoreResult, error) {
	backupPath, err := filepath.Abs(backupPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve backup path: %w", err)
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read backup: %w", err)
	}
	if !backupInfo.Mode().IsRegular() {
		return RestoreResult{}, errors.New("backup must be a regular file")
	}
	checksumVerified, err := verifyBackupChecksum(backupPath, checksumPath)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := checkDatabaseFile(ctx, backupPath); err != nil {
		return RestoreResult{}, fmt.Errorf("validate backup: %w", err)
	}

	existingTargetURL, err := sqliteExistingURL(databaseURL)
	if err != nil {
		return RestoreResult{}, err
	}
	targetDB, err := sql.Open("sqlite", existingTargetURL)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("open target database: %w", err)
	}
	targetClosed := false
	defer func() {
		if !targetClosed {
			_ = targetDB.Close()
		}
	}()
	targetDB.SetMaxOpenConns(1)
	if err := targetDB.PingContext(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("connect target database: %w", err)
	}
	targetPath, err := mainDatabasePath(ctx, targetDB)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve target database: %w", err)
	}
	if targetPath == "" || targetPath == ":memory:" {
		return RestoreResult{}, errors.New("restore requires a file-backed target database")
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve target database: %w", err)
	}
	if filepath.Clean(targetPath) == filepath.Clean(backupPath) {
		return RestoreResult{}, errors.New("backup and target database must be different files")
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("read target database: %w", err)
	}
	if !targetInfo.Mode().IsRegular() {
		return RestoreResult{}, errors.New("target database must be a regular file")
	}

	lifecycleLock, err := acquireExclusiveDatabaseLock(ctx, targetDB)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("lock target database: %w", err)
	}
	defer lifecycleLock.release()
	if err := checkOpenDatabase(ctx, targetDB); err != nil {
		return RestoreResult{}, fmt.Errorf("validate target database: %w", err)
	}

	preRestorePath := targetPath + ".pre-restore-" + now.UTC().Format("20060102T150405.000000000Z") + ".db"
	preRestore, err := backupDatabase(ctx, databaseURL, preRestorePath, now, false)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create pre-restore backup: %w", err)
	}
	result := RestoreResult{
		DatabasePath:       targetPath,
		PreRestoreBackup:   preRestore.Path,
		PreRestoreChecksum: preRestore.ChecksumPath,
		ChecksumVerified:   checksumVerified,
	}

	var busy, logFrames, checkpointedFrames int
	if err := targetDB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint target database: %w", err)
	}
	if busy != 0 {
		return RestoreResult{}, errors.New("target database is busy; stop Tarisya Core before restoring")
	}
	if err := targetDB.Close(); err != nil {
		return RestoreResult{}, fmt.Errorf("close target database: %w", err)
	}
	targetClosed = true

	stagedPath, err := stageRestoreFile(backupPath, targetPath, targetInfo)
	if err != nil {
		return RestoreResult{}, err
	}
	removeStaged := true
	defer func() {
		if removeStaged {
			_ = os.Remove(stagedPath)
		}
	}()
	if checksumVerified {
		backupDigest, _, err := checksumFile(backupPath)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("checksum backup before restore: %w", err)
		}
		stagedDigest, _, err := checksumFile(stagedPath)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("checksum staged restore: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(stagedDigest), []byte(backupDigest)) != 1 {
			return RestoreResult{}, errors.New("staged restore checksum mismatch")
		}
	}
	if err := checkDatabaseFile(ctx, stagedPath); err != nil {
		return RestoreResult{}, fmt.Errorf("validate staged restore: %w", err)
	}
	for _, sidecar := range []string{targetPath + "-wal", targetPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			return RestoreResult{}, fmt.Errorf("remove stale SQLite sidecar %s: %w", sidecar, err)
		}
	}
	if err := replaceFileAtomically(stagedPath, targetPath); err != nil {
		return RestoreResult{}, fmt.Errorf("atomically replace target database: %w", err)
	}
	removeStaged = false
	if err := syncFile(targetPath); err != nil {
		return RestoreResult{}, fmt.Errorf("sync restored database: %w", err)
	}
	if err := checkDatabaseFile(ctx, targetPath); err != nil {
		return RestoreResult{}, fmt.Errorf("validate restored database: %w", err)
	}
	return result, nil
}

func verifyBackupChecksum(backupPath, checksumPath string) (bool, error) {
	checksumPath = checksumPathForDigest(backupPath, checksumPath)
	contents, err := os.ReadFile(checksumPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read backup checksum: %w", err)
	}
	fields := strings.Fields(string(contents))
	if len(fields) == 0 {
		return false, errors.New("backup checksum is empty")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return false, errors.New("backup checksum is invalid")
	}
	if len(fields) > 1 && filepath.Base(strings.TrimPrefix(fields[1], "*")) != filepath.Base(backupPath) {
		return false, errors.New("backup checksum references a different file")
	}
	digest, _, err := checksumFile(backupPath)
	if err != nil {
		return false, fmt.Errorf("checksum backup: %w", err)
	}
	actual, _ := hex.DecodeString(digest)
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return false, errors.New("backup checksum mismatch")
	}
	return true, nil
}

func checksumPathForDigest(backupPath, checksumPath string) string {
	if checksumPath != "" {
		return checksumPath
	}
	return backupPath + ".sha256"
}

func stageRestoreFile(backupPath, targetPath string, targetInfo os.FileInfo) (path string, err error) {
	staged, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".restore-*")
	if err != nil {
		return "", fmt.Errorf("create staged restore: %w", err)
	}
	path = staged.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	source, err := os.Open(backupPath)
	if err != nil {
		_ = staged.Close()
		return "", fmt.Errorf("open backup: %w", err)
	}
	if _, err = io.Copy(staged, source); err == nil {
		err = staged.Chmod(targetInfo.Mode().Perm())
	}
	if err == nil {
		err = preserveFileOwnership(path, targetInfo)
	}
	if err == nil {
		err = staged.Sync()
	}
	sourceCloseErr := source.Close()
	stagedCloseErr := staged.Close()
	if err != nil {
		return "", fmt.Errorf("stage restore: %w", err)
	}
	if sourceCloseErr != nil {
		return "", fmt.Errorf("close backup: %w", sourceCloseErr)
	}
	if stagedCloseErr != nil {
		return "", fmt.Errorf("close staged restore: %w", stagedCloseErr)
	}
	remove = false
	return path, nil
}

func checkDatabaseFile(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", sqliteReadOnlyURL(path))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	return checkOpenDatabase(ctx, db)
}
