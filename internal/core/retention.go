package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrDatabaseFull = errors.New("database size limit reached")

type MaintenanceConfig struct {
	RawRetention        time.Duration
	FiveMinuteRetention time.Duration
	AggregatedRetention time.Duration
}

const (
	rawMetricsBucket        = 5 * time.Minute
	aggregatedMetricsBucket = time.Hour
)

// Maintain moves complete old buckets to lower-resolution tables, then deletes
// expired data. Every move and its source deletion use one transaction.
func (s *Store) Maintain(ctx context.Context, cfg MaintenanceConfig) error {
	return s.maintainAt(ctx, cfg, time.Now().UTC())
}

func (s *Store) maintainAt(ctx context.Context, cfg MaintenanceConfig, now time.Time) error {
	if cfg.RawRetention <= 0 || cfg.FiveMinuteRetention <= cfg.RawRetention || cfg.AggregatedRetention <= cfg.FiveMinuteRetention {
		return errors.New("retention periods must satisfy 0 < raw < 5m < aggregated")
	}
	rawCutoff := completedBucketCutoff(now.Add(-cfg.RawRetention), rawMetricsBucket)
	if err := s.downsampleRaw(ctx, rawCutoff); err != nil {
		return err
	}
	fiveMinuteCutoff := completedBucketCutoff(now.Add(-cfg.FiveMinuteRetention), aggregatedMetricsBucket)
	if err := s.downsample5m(ctx, fiveMinuteCutoff); err != nil {
		return err
	}
	aggregatedCutoff := completedBucketCutoff(now.Add(-cfg.AggregatedRetention), aggregatedMetricsBucket)
	_, err := s.db.ExecContext(ctx, "DELETE FROM metrics_1h WHERE bucket_start < ?", aggregatedCutoff)
	return err
}

func completedBucketCutoff(at time.Time, bucket time.Duration) int64 {
	return unixMillis(at.UTC().Truncate(bucket))
}

func (s *Store) downsampleRaw(ctx context.Context, cutoff int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const size = int64(rawMetricsBucket / time.Millisecond)
	q := `INSERT INTO metrics_5m (server_id,bucket_start,cpu_avg,cpu_min,cpu_max,memory_avg,memory_min,memory_max,disk_avg,disk_min,disk_max,load_avg,load_min,load_max,uptime_max,sample_count)
	SELECT server_id,(collected_at / ?) * ?,AVG(cpu_usage),MIN(cpu_usage),MAX(cpu_usage),AVG(memory_usage),MIN(memory_usage),MAX(memory_usage),AVG(disk_usage),MIN(disk_usage),MAX(disk_usage),AVG(load_average),MIN(load_average),MAX(load_average),MAX(uptime_seconds),COUNT(*)
	FROM metrics WHERE collected_at < ? GROUP BY server_id,(collected_at / ?) * ?
	ON CONFLICT(server_id,bucket_start) DO UPDATE SET cpu_avg=excluded.cpu_avg,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,memory_avg=excluded.memory_avg,memory_min=excluded.memory_min,memory_max=excluded.memory_max,disk_avg=excluded.disk_avg,disk_min=excluded.disk_min,disk_max=excluded.disk_max,load_avg=excluded.load_avg,load_min=excluded.load_min,load_max=excluded.load_max,uptime_max=excluded.uptime_max,sample_count=excluded.sample_count`
	if _, err = tx.ExecContext(ctx, q, size, size, cutoff, size, size); err != nil {
		return fmt.Errorf("aggregate raw metrics: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM metrics WHERE collected_at < ?", cutoff); err != nil {
		return fmt.Errorf("delete aggregated raw metrics: %w", err)
	}
	return tx.Commit()
}

func (s *Store) downsample5m(ctx context.Context, cutoff int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const size = int64(aggregatedMetricsBucket / time.Millisecond)
	q := `INSERT INTO metrics_1h (server_id,bucket_start,cpu_avg,cpu_min,cpu_max,memory_avg,memory_min,memory_max,disk_avg,disk_min,disk_max,load_avg,load_min,load_max,uptime_max,sample_count)
	SELECT server_id,(bucket_start / ?) * ?,SUM(cpu_avg*sample_count)/SUM(sample_count),MIN(cpu_min),MAX(cpu_max),SUM(memory_avg*sample_count)/SUM(sample_count),MIN(memory_min),MAX(memory_max),SUM(disk_avg*sample_count)/SUM(sample_count),MIN(disk_min),MAX(disk_max),SUM(load_avg*sample_count)/SUM(sample_count),MIN(load_min),MAX(load_max),MAX(uptime_max),SUM(sample_count)
	FROM metrics_5m WHERE bucket_start < ? GROUP BY server_id,(bucket_start / ?) * ?
	ON CONFLICT(server_id,bucket_start) DO UPDATE SET cpu_avg=excluded.cpu_avg,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,memory_avg=excluded.memory_avg,memory_min=excluded.memory_min,memory_max=excluded.memory_max,disk_avg=excluded.disk_avg,disk_min=excluded.disk_min,disk_max=excluded.disk_max,load_avg=excluded.load_avg,load_min=excluded.load_min,load_max=excluded.load_max,uptime_max=excluded.uptime_max,sample_count=excluded.sample_count`
	if _, err = tx.ExecContext(ctx, q, size, size, cutoff, size, size); err != nil {
		return fmt.Errorf("aggregate 5m metrics: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM metrics_5m WHERE bucket_start < ?", cutoff); err != nil {
		return fmt.Errorf("delete aggregated 5m metrics: %w", err)
	}
	return tx.Commit()
}

// DatabaseSize includes SQLite's WAL and shared-memory sidecar files.
func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var seq int
	var name, path string
	for rows.Next() {
		if err := rows.Scan(&seq, &name, &path); err != nil {
			return 0, err
		}
		if name == "main" {
			break
		}
	}
	if path == "" || path == ":memory:" {
		return 0, nil
	}
	var total int64
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(file)
		if err == nil {
			total += info.Size()
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	return total, rows.Err()
}

func (s *Store) DatabaseUsage(ctx context.Context) (size int64, ratio float64, err error) {
	size, err = s.DatabaseSize(ctx)
	if err != nil || s.maxSize <= 0 {
		return size, 0, err
	}
	return size, float64(size) / float64(s.maxSize), nil
}

func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA incremental_vacuum")
	return err
}
