package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

var ErrDatabaseInUse = errors.New("database is in use")

type databaseLifecycleLock struct {
	file *flock.Flock
}

func acquireSharedDatabaseLock(ctx context.Context, db *sql.DB) (*databaseLifecycleLock, error) {
	return acquireDatabaseLock(ctx, db, false)
}

func acquireExclusiveDatabaseLock(ctx context.Context, db *sql.DB) (*databaseLifecycleLock, error) {
	return acquireDatabaseLock(ctx, db, true)
}

func acquireDatabaseLock(ctx context.Context, db *sql.DB, exclusive bool) (*databaseLifecycleLock, error) {
	path, err := mainDatabasePath(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("resolve database lock path: %w", err)
	}
	if path == "" || path == ":memory:" {
		return nil, nil
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database lock path: %w", err)
	}
	fileLock := flock.New(absolutePath+".lock", flock.SetPermissions(0o600))
	var locked bool
	if exclusive {
		locked, err = fileLock.TryLock()
	} else {
		locked, err = fileLock.TryRLock()
	}
	if err != nil {
		return nil, fmt.Errorf("lock database: %w", err)
	}
	if !locked {
		_ = fileLock.Close()
		return nil, ErrDatabaseInUse
	}
	return &databaseLifecycleLock{file: fileLock}, nil
}

func (l *databaseLifecycleLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = l.file.Unlock()
	_ = l.file.Close()
}
