package core

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const currentSchemaVersion int64 = 1

// DatabaseDiagnostics contains independent readiness results for a Tarisya
// SQLite database. A field-specific error does not prevent the remaining
// checks from running.
type DatabaseDiagnostics struct {
	JournalMode      string
	JournalModeErr   error
	ForeignKeys      bool
	ForeignKeysErr   error
	StorageWritable  bool
	StorageErr       error
	SchemaVersion    int64
	MigrationCurrent bool
	MigrationErr     error
	ActiveAPIKeys    int64
	APIKeysErr       error
}

// DiagnoseDatabase checks runtime SQLite settings and application readiness
// without changing application data.
func DiagnoseDatabase(ctx context.Context, databaseURL string) (DatabaseDiagnostics, error) {
	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return DatabaseDiagnostics{}, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		return DatabaseDiagnostics{}, fmt.Errorf("connect database: %w", err)
	}
	lock, err := acquireSharedDatabaseLock(ctx, db)
	if err != nil {
		return DatabaseDiagnostics{}, fmt.Errorf("lock database lifecycle: %w", err)
	}
	defer lock.release()

	result := DatabaseDiagnostics{}
	result.JournalModeErr = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&result.JournalMode)
	result.JournalMode = strings.ToLower(result.JournalMode)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		result.ForeignKeysErr = err
	} else {
		var enabled int
		result.ForeignKeysErr = db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled)
		result.ForeignKeys = enabled == 1
	}

	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		result.StorageErr = err
	} else if _, err := db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		result.StorageErr = err
	} else if _, err := db.ExecContext(ctx, "ROLLBACK"); err != nil {
		result.StorageErr = err
	} else {
		result.StorageWritable = true
	}

	result.MigrationErr = db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&result.SchemaVersion)
	result.MigrationCurrent = result.MigrationErr == nil && result.SchemaVersion == currentSchemaVersion

	result.APIKeysErr = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_api_keys WHERE revoked_at IS NULL").Scan(&result.ActiveAPIKeys)
	return result, nil
}
