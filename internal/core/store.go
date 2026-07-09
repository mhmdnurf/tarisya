package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mhmdnurf/tarisya/internal/metrics"
	dbmigrations "github.com/mhmdnurf/tarisya/migrations"
)

type Store struct {
	pool *pgxpool.Pool
}

type MetricRecord struct {
	ID          int64          `json:"id,omitempty"`
	ServerID    string         `json:"server_id"`
	CollectedAt time.Time      `json:"collected_at"`
	Metrics     metrics.Values `json:"metrics"`
}

type MetricSummary struct {
	Average float64 `json:"average"`
	Peak    float64 `json:"peak"`
}

type MetricStatistics struct {
	CPU         MetricSummary `json:"cpu"`
	Memory      MetricSummary `json:"memory"`
	Disk        MetricSummary `json:"disk"`
	LoadAverage MetricSummary `json:"load_average"`
}

type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type ServerRecord struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	OverallStatus      string     `json:"overall_status"`
	ConnectivityStatus string     `json:"connectivity_status"`
	HealthStatus       string     `json:"health_status"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
	CreatedAt          time.Time  `json:"created_at"`
}

type ServerDetail struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Hostname           string     `json:"hostname"`
	OverallStatus      string     `json:"overall_status"`
	ConnectivityStatus string     `json:"connectivity_status"`
	HealthStatus       string     `json:"health_status"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
	AgentVersion       string     `json:"agent_version"`
	UptimeSeconds      uint64     `json:"uptime_seconds"`
	CreatedAt          time.Time  `json:"created_at"`
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
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := iofs.New(dbmigrations.Files, ".")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	db := stdlib.OpenDB(*s.pool.Config().ConnConfig)
	defer db.Close()
	driver, err := migratepostgres.WithInstance(db, &migratepostgres.Config{})
	if err != nil {
		return fmt.Errorf("initialize migration database: %w", err)
	}
	runner, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}
	defer runner.Close()

	if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func RunMigration(databaseURL, direction string) error {
	source, err := iofs.New(dbmigrations.Files, ".")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	driver, err := migratepostgres.WithInstance(db, &migratepostgres.Config{})
	if err != nil {
		return fmt.Errorf("initialize migration database: %w", err)
	}
	runner, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}
	defer runner.Close()

	switch direction {
	case "up":
		err = runner.Up()
	case "down":
		err = runner.Steps(-1)
	default:
		return fmt.Errorf("unsupported migration direction %q", direction)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

func (s *Store) Bootstrap(
	ctx context.Context,
	name, email, passwordHash, serverID, apiKey string,
) error {
	if email == "" {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (name, email, password_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE
		SET name = EXCLUDED.name, password_hash = EXCLUDED.password_hash, updated_at = NOW()
		RETURNING id
	`, name, email, passwordHash).Scan(&userID); err != nil {
		return fmt.Errorf("bootstrap user: %w", err)
	}
	if serverID == "" {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO servers (id, user_id, name)
		VALUES ($1, $2, $1)
		ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id
	`, serverID, userID); err != nil {
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

func (s *Store) CreateUser(ctx context.Context, name, email, passwordHash string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (name, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, name, email, role, password_hash, created_at
	`, name, email, passwordHash).Scan(
		&user.ID, &user.Name, &user.Email, &user.Role, &user.PasswordHash, &user.CreatedAt,
	)
	return user, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, role, password_hash, created_at
		FROM users WHERE email = $1
	`, email).Scan(
		&user.ID, &user.Name, &user.Email, &user.Role, &user.PasswordHash, &user.CreatedAt,
	)
	return user, err
}

func (s *Store) UserByID(ctx context.Context, userID int64) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, role, password_hash, created_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&user.ID, &user.Name, &user.Email, &user.Role, &user.PasswordHash, &user.CreatedAt,
	)
	return user, err
}

func (s *Store) UserOwnsServer(ctx context.Context, userID int64, serverID string) (bool, error) {
	var owns bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM servers WHERE id = $1 AND user_id = $2)
	`, serverID, userID).Scan(&owns)
	return owns, err
}

func (s *Store) ServersByUser(
	ctx context.Context,
	userID int64,
	offlineThreshold time.Duration,
	warningThreshold, criticalThreshold float64,
) ([]ServerRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id,
		       s.name,
		       CASE
		           WHEN s.last_seen_at IS NULL
		               OR s.last_seen_at < NOW() - make_interval(secs => $2)
		           THEN 'offline'
		           ELSE 'online'
		       END AS connectivity_status,
		       CASE
		           WHEN s.last_seen_at IS NULL
		               OR s.last_seen_at < NOW() - make_interval(secs => $2)
		               OR latest.id IS NULL
		           THEN 'unknown'
		           WHEN GREATEST(latest.cpu_usage, latest.memory_usage, latest.disk_usage) >= $4
		           THEN 'critical'
		           WHEN GREATEST(latest.cpu_usage, latest.memory_usage, latest.disk_usage) >= $3
		           THEN 'warning'
		           ELSE 'healthy'
		       END AS health_status,
		       s.last_seen_at,
		       s.created_at
		FROM servers s
		LEFT JOIN LATERAL (
			SELECT id, cpu_usage, memory_usage, disk_usage
			FROM metrics
			WHERE server_id = s.id
			ORDER BY collected_at DESC, id DESC
			LIMIT 1
		) latest ON TRUE
		WHERE s.user_id = $1
		ORDER BY s.created_at DESC
	`, userID, int(offlineThreshold.Seconds()), warningThreshold, criticalThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := make([]ServerRecord, 0)
	for rows.Next() {
		var server ServerRecord
		if err := rows.Scan(
			&server.ID,
			&server.Name,
			&server.ConnectivityStatus,
			&server.HealthStatus,
			&server.LastSeenAt,
			&server.CreatedAt,
		); err != nil {
			return nil, err
		}
		server.OverallStatus = overallStatus(server.ConnectivityStatus, server.HealthStatus)
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *Store) ServerByUser(
	ctx context.Context,
	userID int64,
	serverID string,
	offlineThreshold time.Duration,
	warningThreshold, criticalThreshold float64,
) (ServerDetail, error) {
	var server ServerDetail
	err := s.pool.QueryRow(ctx, `
		SELECT s.id,
		       s.name,
		       COALESCE(s.hostname, ''),
		       CASE
		           WHEN s.last_seen_at IS NULL
		               OR s.last_seen_at < NOW() - make_interval(secs => $3)
		           THEN 'offline'
		           ELSE 'online'
		       END AS connectivity_status,
		       CASE
		           WHEN s.last_seen_at IS NULL
		               OR s.last_seen_at < NOW() - make_interval(secs => $3)
		               OR latest.id IS NULL
		           THEN 'unknown'
		           WHEN GREATEST(latest.cpu_usage, latest.memory_usage, latest.disk_usage) >= $5
		           THEN 'critical'
		           WHEN GREATEST(latest.cpu_usage, latest.memory_usage, latest.disk_usage) >= $4
		           THEN 'warning'
		           ELSE 'healthy'
		       END AS health_status,
		       s.last_seen_at,
		       COALESCE(s.agent_version, ''),
		       COALESCE(latest.uptime_seconds, 0),
		       s.created_at
		FROM servers s
		LEFT JOIN LATERAL (
			SELECT id, cpu_usage, memory_usage, disk_usage, uptime_seconds
			FROM metrics
			WHERE server_id = s.id
			ORDER BY collected_at DESC, id DESC
			LIMIT 1
		) latest ON TRUE
		WHERE s.id = $1 AND s.user_id = $2
	`, serverID, userID, int(offlineThreshold.Seconds()), warningThreshold, criticalThreshold).Scan(
		&server.ID,
		&server.Name,
		&server.Hostname,
		&server.ConnectivityStatus,
		&server.HealthStatus,
		&server.LastSeenAt,
		&server.AgentVersion,
		&server.UptimeSeconds,
		&server.CreatedAt,
	)
	server.OverallStatus = overallStatus(server.ConnectivityStatus, server.HealthStatus)
	return server, err
}

func (s *Store) MetricsChart(
	ctx context.Context,
	serverID string,
	start, end time.Time,
	bucket string,
) ([]MetricRecord, MetricStatistics, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT date_bin($4::interval, collected_at, TIMESTAMPTZ '2000-01-01') AS bucket,
		       AVG(cpu_usage),
		       AVG(memory_usage),
		       AVG(disk_usage),
		       AVG(load_average),
		       MAX(uptime_seconds)
		FROM metrics
		WHERE server_id = $1
		  AND collected_at >= $2
		  AND collected_at <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`, serverID, start, end, bucket)
	if err != nil {
		return nil, MetricStatistics{}, err
	}

	records := make([]MetricRecord, 0)
	for rows.Next() {
		var record MetricRecord
		record.ServerID = serverID
		if err := rows.Scan(
			&record.CollectedAt,
			&record.Metrics.CPUUsage,
			&record.Metrics.MemoryUsage,
			&record.Metrics.DiskUsage,
			&record.Metrics.LoadAverage,
			&record.Metrics.UptimeSeconds,
		); err != nil {
			rows.Close()
			return nil, MetricStatistics{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, MetricStatistics{}, err
	}
	rows.Close()

	var statistics MetricStatistics
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(cpu_usage), 0), COALESCE(MAX(cpu_usage), 0),
		       COALESCE(AVG(memory_usage), 0), COALESCE(MAX(memory_usage), 0),
		       COALESCE(AVG(disk_usage), 0), COALESCE(MAX(disk_usage), 0),
		       COALESCE(AVG(load_average), 0), COALESCE(MAX(load_average), 0)
		FROM metrics
		WHERE server_id = $1
		  AND collected_at >= $2
		  AND collected_at <= $3
	`, serverID, start, end).Scan(
		&statistics.CPU.Average,
		&statistics.CPU.Peak,
		&statistics.Memory.Average,
		&statistics.Memory.Peak,
		&statistics.Disk.Average,
		&statistics.Disk.Peak,
		&statistics.LoadAverage.Average,
		&statistics.LoadAverage.Peak,
	)
	if err != nil {
		return nil, MetricStatistics{}, err
	}
	return records, statistics, nil
}

func overallStatus(connectivity, health string) string {
	if connectivity == "offline" {
		return "offline"
	}
	return health
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
		SET hostname = $2,
		    agent_version = $3,
		    last_seen_at = $4,
		    status = 'healthy',
		    updated_at = NOW()
		WHERE id = $1
	`, payload.ServerID, payload.Hostname, payload.AgentVersion, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) LatestMetrics(ctx context.Context, serverID string) (MetricRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, server_id, cpu_usage, memory_usage, disk_usage,
		       load_average, uptime_seconds, collected_at
		FROM metrics
		WHERE server_id = $1
		ORDER BY collected_at DESC, id DESC
		LIMIT 1
	`, serverID)

	return scanMetric(row.Scan)
}

func (s *Store) MetricsHistory(
	ctx context.Context,
	serverID string,
	limit int,
	before time.Time,
) ([]MetricRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, server_id, cpu_usage, memory_usage, disk_usage,
		       load_average, uptime_seconds, collected_at
		FROM metrics
		WHERE server_id = $1
		  AND ($2::timestamptz IS NULL OR collected_at < $2)
		ORDER BY collected_at DESC, id DESC
		LIMIT $3
	`, serverID, nullableTime(before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]MetricRecord, 0, limit)
	for rows.Next() {
		record, err := scanMetric(rows.Scan)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) Healthy(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

type scanFunc func(dest ...any) error

func scanMetric(scan scanFunc) (MetricRecord, error) {
	var record MetricRecord
	err := scan(
		&record.ID,
		&record.ServerID,
		&record.Metrics.CPUUsage,
		&record.Metrics.MemoryUsage,
		&record.Metrics.DiskUsage,
		&record.Metrics.LoadAverage,
		&record.Metrics.UptimeSeconds,
		&record.CollectedAt,
	)
	return record, err
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func hashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
