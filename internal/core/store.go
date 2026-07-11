package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mhmdnurf/tarisya/internal/metrics"
	dbmigrations "github.com/mhmdnurf/tarisya/migrations"
	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	maxSize int64
}

const sqlitePragmas = `
	PRAGMA foreign_keys = ON;
	PRAGMA journal_mode = WAL;
	PRAGMA busy_timeout = 5000;
	PRAGMA synchronous = FULL;
`

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

func OpenStore(ctx context.Context, databaseURL string, maxSize int64) (*Store, error) {
	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure database: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite has one writer; WAL still serves readers efficiently.
	if _, err = db.ExecContext(ctx, sqlitePragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure SQLite pragmas: %w", err)
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return &Store{db: db, maxSize: maxSize}, nil
}

func (s *Store) Close() { _ = s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error { return migrate(ctx, s.db, "up") }
func RunMigration(databaseURL, direction string) error {
	db, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(sqlitePragmas); err != nil {
		return err
	}
	return migrate(context.Background(), db, direction)
}
func migrate(ctx context.Context, db *sql.DB, direction string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("unsupported migration direction %q", direction)
	}
	if direction == "down" {
		for _, statement := range dbmigrations.Statements(dbmigrations.InitialSchemaDown()) {
			if strings.TrimSpace(statement) != "" {
				if _, err := db.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = 1)").Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	if err := backupBeforeMigration(ctx, db); err != nil {
		return fmt.Errorf("backup database before migration: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range dbmigrations.Statements(dbmigrations.InitialSchema()) {
		if strings.TrimSpace(statement) != "" {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("run migration: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (1)"); err != nil {
		return err
	}
	return tx.Commit()
}

// backupBeforeMigration creates a consistent SQLite snapshot only when an
// existing on-disk database has application objects that are about to be
// migrated. Fresh and in-memory databases do not need a backup.
func backupBeforeMigration(ctx context.Context, db *sql.DB) error {
	var hasApplicationObjects bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM sqlite_master
		WHERE type IN ('table', 'index', 'trigger', 'view')
		AND name NOT LIKE 'sqlite_%'
		AND name != 'schema_migrations'
	)`).Scan(&hasApplicationObjects); err != nil {
		return err
	}
	if !hasApplicationObjects {
		return nil
	}

	path, err := mainDatabasePath(ctx, db)
	if err != nil {
		return err
	}
	if path == "" || path == ":memory:" {
		return nil
	}
	backupPath := filepath.Clean(path) + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", backupPath); err != nil {
		return err
	}
	return nil
}

func mainDatabasePath(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int
		var name, path string
		if err := rows.Scan(&sequence, &name, &path); err != nil {
			return "", err
		}
		if name == "main" {
			return path, rows.Err()
		}
	}
	return "", rows.Err()
}

func (s *Store) Bootstrap(ctx context.Context, name, email, passwordHash, serverID, apiKey string) error {
	if email == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start bootstrap transaction: %w", err)
	}
	defer tx.Rollback()
	now := unixMillis(time.Now())
	var userID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users (name,email,password_hash,created_at,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(email) DO UPDATE SET name=excluded.name,password_hash=excluded.password_hash,updated_at=excluded.updated_at RETURNING id`, name, email, passwordHash, now, now).Scan(&userID)
	if err != nil {
		return fmt.Errorf("bootstrap user: %w", err)
	}
	if serverID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO servers (id,user_id,name,created_at,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id`, serverID, userID, serverID, now, now); err != nil {
			return fmt.Errorf("bootstrap server: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO server_api_keys (server_id,api_key_hash,created_at) VALUES (?,?,?) ON CONFLICT(api_key_hash) DO NOTHING`, serverID, hashAPIKey(apiKey), now); err != nil {
			return fmt.Errorf("bootstrap API key: %w", err)
		}
	}
	return tx.Commit()
}
func (s *Store) CreateUser(ctx context.Context, name, email, passwordHash string) (User, error) {
	var u User
	var created int64
	now := unixMillis(time.Now())
	err := s.db.QueryRowContext(ctx, `INSERT INTO users(name,email,password_hash,created_at,updated_at) VALUES(?,?,?,?,?) RETURNING id,name,email,role,password_hash,created_at`, name, email, passwordHash, now, now).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.PasswordHash, &created)
	u.CreatedAt = fromUnixMillis(created)
	return u, err
}
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return s.user(ctx, `SELECT id,name,email,role,password_hash,created_at FROM users WHERE email=?`, email)
}
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.user(ctx, `SELECT id,name,email,role,password_hash,created_at FROM users WHERE id=?`, id)
}

func (s *Store) UpdatePasswordHash(ctx context.Context, userID int64, previousHash, newHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=? AND password_hash=?`, newHash, unixMillis(time.Now()), userID, previousHash)
	return err
}

func (s *Store) user(ctx context.Context, q string, arg any) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, q, arg).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.PasswordHash, &created)
	u.CreatedAt = fromUnixMillis(created)
	return u, err
}
func (s *Store) UserOwnsServer(ctx context.Context, userID int64, serverID string) (bool, error) {
	var v bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id=? AND user_id=?)`, serverID, userID).Scan(&v)
	return v, err
}
func (s *Store) CreateServer(ctx context.Context, userID int64, serverID, name, apiKey string) (ServerRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ServerRecord{}, err
	}
	defer tx.Rollback()
	now := unixMillis(time.Now())
	var record ServerRecord
	var created int64
	err = tx.QueryRowContext(ctx, `INSERT INTO servers(id,user_id,name,created_at,updated_at) VALUES(?,?,?,?,?) RETURNING id,name,last_seen_at,created_at`, serverID, userID, name, now, now).Scan(&record.ID, &record.Name, new(sql.NullInt64), &created)
	if err != nil {
		return record, err
	}
	record.CreatedAt = fromUnixMillis(created)
	if _, err = tx.ExecContext(ctx, `INSERT INTO server_api_keys(server_id,api_key_hash,created_at) VALUES(?,?,?)`, serverID, hashAPIKey(apiKey), now); err != nil {
		return record, err
	}
	err = tx.Commit()
	record.ConnectivityStatus = "pending"
	record.HealthStatus = "unknown"
	record.OverallStatus = "pending"
	return record, err
}
func (s *Store) DeleteServer(ctx context.Context, userID int64, serverID string) (bool, error) {
	r, e := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=? AND user_id=?`, serverID, userID)
	if e != nil {
		return false, e
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
func (s *Store) RotateAPIKey(ctx context.Context, userID int64, serverID, apiKey string) (bool, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id=? AND user_id=?)`, serverID, userID).Scan(&exists); e != nil || !exists {
		return exists, e
	}
	now := unixMillis(time.Now())
	if _, e = tx.ExecContext(ctx, `UPDATE server_api_keys SET revoked_at=? WHERE server_id=? AND revoked_at IS NULL`, now, serverID); e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO server_api_keys(server_id,api_key_hash,created_at) VALUES(?,?,?)`, serverID, hashAPIKey(apiKey), now); e != nil {
		return false, e
	}
	return true, tx.Commit()
}

// RevokeAPIKey invalidates every active key for an owned server. It is
// intentionally idempotent: an owned server with no active key still succeeds.
func (s *Store) RevokeAPIKey(ctx context.Context, userID int64, serverID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id=? AND user_id=?)`, serverID, userID).Scan(&exists); err != nil || !exists {
		return exists, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE server_api_keys SET revoked_at=? WHERE server_id=? AND revoked_at IS NULL`, unixMillis(time.Now()), serverID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) ServersByUser(ctx context.Context, userID int64, offline time.Duration, warn, critical float64) ([]ServerRecord, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT s.id,s.name,s.last_seen_at,s.created_at,latest.cpu_usage,latest.memory_usage,latest.disk_usage FROM servers s LEFT JOIN metrics latest ON latest.id=(SELECT id FROM metrics WHERE server_id=s.id ORDER BY collected_at DESC,id DESC LIMIT 1) WHERE s.user_id=? ORDER BY s.created_at DESC`, userID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ServerRecord{}
	for rows.Next() {
		var x ServerRecord
		var seen sql.NullInt64
		var created int64
		var a, b, c sql.NullFloat64
		if e = rows.Scan(&x.ID, &x.Name, &seen, &created, &a, &b, &c); e != nil {
			return nil, e
		}
		x.LastSeenAt = nullTime(seen)
		x.CreatedAt = fromUnixMillis(created)
		x.ConnectivityStatus, x.HealthStatus = status(x.LastSeenAt, a, b, c, offline, warn, critical)
		x.OverallStatus = overallStatus(x.ConnectivityStatus, x.HealthStatus)
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) ServerByUser(ctx context.Context, userID int64, serverID string, offline time.Duration, warn, critical float64) (ServerDetail, error) {
	var x ServerDetail
	var seen sql.NullInt64
	var created int64
	var uptime sql.NullInt64
	var a, b, c sql.NullFloat64
	e := s.db.QueryRowContext(ctx, `SELECT s.id,s.name,COALESCE(s.hostname,''),s.last_seen_at,COALESCE(s.agent_version,''),s.created_at,latest.uptime_seconds,latest.cpu_usage,latest.memory_usage,latest.disk_usage FROM servers s LEFT JOIN metrics latest ON latest.id=(SELECT id FROM metrics WHERE server_id=s.id ORDER BY collected_at DESC,id DESC LIMIT 1) WHERE s.id=? AND s.user_id=?`, serverID, userID).Scan(&x.ID, &x.Name, &x.Hostname, &seen, &x.AgentVersion, &created, &uptime, &a, &b, &c)
	if e != nil {
		return x, e
	}
	x.LastSeenAt = nullTime(seen)
	x.CreatedAt = fromUnixMillis(created)
	if uptime.Valid {
		x.UptimeSeconds = uint64(uptime.Int64)
	}
	x.ConnectivityStatus, x.HealthStatus = status(x.LastSeenAt, a, b, c, offline, warn, critical)
	x.OverallStatus = overallStatus(x.ConnectivityStatus, x.HealthStatus)
	return x, nil
}

func status(seen *time.Time, a, b, c sql.NullFloat64, offline time.Duration, warn, critical float64) (string, string) {
	if seen == nil {
		return "pending", "unknown"
	}
	if seen.Before(time.Now().UTC().Add(-offline)) {
		return "offline", "unknown"
	}
	if !a.Valid {
		return "online", "unknown"
	}
	peak := max(a.Float64, b.Float64, c.Float64)
	if peak >= critical {
		return "online", "critical"
	}
	if peak >= warn {
		return "online", "warning"
	}
	return "online", "healthy"
}
func max(a, b, c float64) float64 {
	if a < b {
		a = b
	}
	if a < c {
		a = c
	}
	return a
}

func overallStatus(connectivity, health string) string {
	if connectivity != "online" {
		return connectivity
	}
	return health
}

func (s *Store) MetricsChart(ctx context.Context, serverID string, start, end time.Time, bucket string) ([]MetricRecord, MetricStatistics, error) {
	table := "metrics"
	if start.Before(time.Now().Add(-30 * 24 * time.Hour)) {
		table = "metrics_1h"
	} else if start.Before(time.Now().Add(-7 * 24 * time.Hour)) {
		table = "metrics_5m"
	}
	if table == "metrics" {
		return s.rawChart(ctx, serverID, start, end, bucket)
	}
	return s.aggregateChart(ctx, table, serverID, start, end)
}
func (s *Store) rawChart(ctx context.Context, serverID string, start, end time.Time, bucket string) ([]MetricRecord, MetricStatistics, error) {
	seconds, err := bucketMillis(bucket)
	if err != nil {
		return nil, MetricStatistics{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT (collected_at / ?) * ? AS bucket,AVG(cpu_usage),AVG(memory_usage),AVG(disk_usage),AVG(load_average),MAX(uptime_seconds) FROM metrics WHERE server_id=? AND collected_at>=? AND collected_at<=? GROUP BY (collected_at / ?) * ? ORDER BY bucket`, seconds, seconds, serverID, unixMillis(start), unixMillis(end), seconds, seconds)
	if err != nil {
		return nil, MetricStatistics{}, err
	}
	defer rows.Close()
	records := []MetricRecord{}
	for rows.Next() {
		var ms int64
		var r MetricRecord
		r.ServerID = serverID
		if err = rows.Scan(&ms, &r.Metrics.CPUUsage, &r.Metrics.MemoryUsage, &r.Metrics.DiskUsage, &r.Metrics.LoadAverage, &r.Metrics.UptimeSeconds); err != nil {
			return nil, MetricStatistics{}, err
		}
		r.CollectedAt = fromUnixMillis(ms)
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		return nil, MetricStatistics{}, err
	}
	stats, err := s.statistics(ctx, "metrics", serverID, start, end)
	return records, stats, err
}
func (s *Store) aggregateChart(ctx context.Context, table, serverID string, start, end time.Time) ([]MetricRecord, MetricStatistics, error) {
	q := fmt.Sprintf(`SELECT bucket_start,cpu_avg,memory_avg,disk_avg,load_avg,uptime_max FROM %s WHERE server_id=? AND bucket_start>=? AND bucket_start<=? ORDER BY bucket_start`, table)
	rows, err := s.db.QueryContext(ctx, q, serverID, unixMillis(start), unixMillis(end))
	if err != nil {
		return nil, MetricStatistics{}, err
	}
	defer rows.Close()
	records := []MetricRecord{}
	for rows.Next() {
		var ms int64
		var r MetricRecord
		r.ServerID = serverID
		if err = rows.Scan(&ms, &r.Metrics.CPUUsage, &r.Metrics.MemoryUsage, &r.Metrics.DiskUsage, &r.Metrics.LoadAverage, &r.Metrics.UptimeSeconds); err != nil {
			return nil, MetricStatistics{}, err
		}
		r.CollectedAt = fromUnixMillis(ms)
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		return nil, MetricStatistics{}, err
	}
	stats, err := s.statistics(ctx, table, serverID, start, end)
	return records, stats, err
}
func (s *Store) statistics(ctx context.Context, table, serverID string, start, end time.Time) (MetricStatistics, error) {
	var x MetricStatistics
	var q string
	if table == "metrics" {
		q = `SELECT COALESCE(AVG(cpu_usage),0),COALESCE(MAX(cpu_usage),0),COALESCE(AVG(memory_usage),0),COALESCE(MAX(memory_usage),0),COALESCE(AVG(disk_usage),0),COALESCE(MAX(disk_usage),0),COALESCE(AVG(load_average),0),COALESCE(MAX(load_average),0) FROM metrics WHERE server_id=? AND collected_at>=? AND collected_at<=?`
	} else {
		q = fmt.Sprintf(`SELECT COALESCE(AVG(cpu_avg),0),COALESCE(MAX(cpu_max),0),COALESCE(AVG(memory_avg),0),COALESCE(MAX(memory_max),0),COALESCE(AVG(disk_avg),0),COALESCE(MAX(disk_max),0),COALESCE(AVG(load_avg),0),COALESCE(MAX(load_max),0) FROM %s WHERE server_id=? AND bucket_start>=? AND bucket_start<=?`, table)
	}
	err := s.db.QueryRowContext(ctx, q, serverID, unixMillis(start), unixMillis(end)).Scan(&x.CPU.Average, &x.CPU.Peak, &x.Memory.Average, &x.Memory.Peak, &x.Disk.Average, &x.Disk.Peak, &x.LoadAverage.Average, &x.LoadAverage.Peak)
	return x, err
}

func (s *Store) APIKeyValid(ctx context.Context, serverID, apiKey string) (bool, error) {
	var v bool
	e := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_api_keys WHERE server_id=? AND api_key_hash=? AND revoked_at IS NULL)`, serverID, hashAPIKey(apiKey)).Scan(&v)
	return v, e
}
func (s *Store) SaveMetrics(ctx context.Context, p MetricsPayload) error {
	if s.maxSize > 0 {
		size, e := s.DatabaseSize(ctx)
		if e != nil {
			return e
		}
		if size >= s.maxSize {
			return ErrDatabaseFull
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return mapDatabaseFullError(e)
	}
	defer tx.Rollback()
	now := unixMillis(time.Now())
	m := p.Metrics
	if _, e = tx.ExecContext(ctx, `INSERT INTO metrics(server_id,cpu_usage,memory_usage,disk_usage,load_average,uptime_seconds,collected_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, p.ServerID, m.CPUUsage, m.MemoryUsage, m.DiskUsage, m.LoadAverage, m.UptimeSeconds, unixMillis(p.Timestamp), now); e != nil {
		return mapDatabaseFullError(e)
	}
	if _, e = tx.ExecContext(ctx, `UPDATE servers SET hostname=?,agent_version=?,last_seen_at=?,status='healthy',updated_at=? WHERE id=?`, p.Hostname, p.AgentVersion, now, now, p.ServerID); e != nil {
		return mapDatabaseFullError(e)
	}
	return mapDatabaseFullError(tx.Commit())
}

func mapDatabaseFullError(err error) error {
	if err == nil || errors.Is(err, ErrDatabaseFull) {
		return err
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&0xff == 13 { // SQLITE_FULL
		return fmt.Errorf("%w: %v", ErrDatabaseFull, err)
	}
	return err
}
func (s *Store) LatestMetrics(ctx context.Context, serverID string) (MetricRecord, error) {
	return scanMetric(s.db.QueryRowContext(ctx, `SELECT id,server_id,cpu_usage,memory_usage,disk_usage,load_average,uptime_seconds,collected_at FROM metrics WHERE server_id=? ORDER BY collected_at DESC,id DESC LIMIT 1`, serverID).Scan)
}
func (s *Store) MetricsHistory(ctx context.Context, serverID string, limit int, before time.Time) ([]MetricRecord, error) {
	q := `SELECT id,server_id,cpu_usage,memory_usage,disk_usage,load_average,uptime_seconds,collected_at FROM metrics WHERE server_id=? AND (? IS NULL OR collected_at<?) ORDER BY collected_at DESC,id DESC LIMIT ?`
	var b any
	if !before.IsZero() {
		b = unixMillis(before)
	}
	rows, e := s.db.QueryContext(ctx, q, serverID, b, b, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []MetricRecord{}
	for rows.Next() {
		r, e := scanMetric(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Healthy(ctx context.Context) error { return s.db.PingContext(ctx) }

type scanFunc func(...any) error

func scanMetric(scan scanFunc) (MetricRecord, error) {
	var r MetricRecord
	var t int64
	err := scan(&r.ID, &r.ServerID, &r.Metrics.CPUUsage, &r.Metrics.MemoryUsage, &r.Metrics.DiskUsage, &r.Metrics.LoadAverage, &r.Metrics.UptimeSeconds, &t)
	r.CollectedAt = fromUnixMillis(t)
	return r, err
}
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func unixMillis(t time.Time) int64     { return t.UTC().UnixMilli() }
func fromUnixMillis(v int64) time.Time { return time.UnixMilli(v).UTC() }
func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	x := fromUnixMillis(v.Int64)
	return &x
}
func bucketMillis(bucket string) (int64, error) {
	switch bucket {
	case "15 seconds":
		return 15_000, nil
	case "1 minute":
		return 60_000, nil
	case "5 minutes":
		return 300_000, nil
	case "15 minutes":
		return 900_000, nil
	}
	return 0, errors.New("unsupported chart bucket")
}
