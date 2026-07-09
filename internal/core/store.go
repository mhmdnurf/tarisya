package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE TABLE IF NOT EXISTS servers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'offline',
	last_seen_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS server_api_keys (
	id BIGSERIAL PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	api_key_hash CHAR(64) NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS metrics (
	id BIGSERIAL PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	cpu_usage DOUBLE PRECISION NOT NULL,
	memory_usage DOUBLE PRECISION NOT NULL,
	disk_usage DOUBLE PRECISION NOT NULL,
	load_average DOUBLE PRECISION NOT NULL,
	uptime_seconds BIGINT NOT NULL,
	collected_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS metrics_server_collected_idx
	ON metrics (server_id, collected_at DESC);
`

type Store struct {
	pool *pgxpool.Pool
}

func OpenStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func (s *Store) BootstrapServer(ctx context.Context, serverID, apiKey string) error {
	if serverID == "" {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO servers (id, name)
		VALUES ($1, $1)
		ON CONFLICT (id) DO NOTHING
	`, serverID); err != nil {
		return fmt.Errorf("bootstrap server: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO server_api_keys (server_id, api_key_hash)
		VALUES ($1, $2)
		ON CONFLICT (api_key_hash) DO NOTHING
	`, serverID, hashAPIKey(apiKey)); err != nil {
		return fmt.Errorf("bootstrap API key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap transaction: %w", err)
	}
	return nil
}

func (s *Store) APIKeyValid(ctx context.Context, serverID, apiKey string) (bool, error) {
	var valid bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM server_api_keys
			WHERE server_id = $1 AND api_key_hash = $2 AND revoked_at IS NULL
		)
	`, serverID, hashAPIKey(apiKey)).Scan(&valid)
	return valid, err
}

func (s *Store) SaveMetrics(ctx context.Context, payload MetricsPayload) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	m := payload.Metrics
	if _, err := tx.Exec(ctx, `
		INSERT INTO metrics (
			server_id, cpu_usage, memory_usage, disk_usage,
			load_average, uptime_seconds, collected_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, payload.ServerID, m.CPUUsage, m.MemoryUsage, m.DiskUsage,
		m.LoadAverage, m.UptimeSeconds, payload.Timestamp); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE servers
		SET last_seen_at = $2, status = 'healthy', updated_at = NOW()
		WHERE id = $1
	`, payload.ServerID, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Healthy(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func hashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
