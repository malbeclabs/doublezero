package qa

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

type ClickhouseConfig struct {
	Addr       string
	DB         string
	User       string
	Pass       string
	TLSDisable bool
}

// ClickhouseConfigFromEnv reads ClickHouse connection settings from environment variables.
// Returns nil if CLICKHOUSE_ADDR is not set, which disables ClickHouse publishing.
//
// Schema changes to the tables created by PublishToClickhouse require manual ALTER TABLE
// or DROP TABLE on the ClickHouse side — there is no migration tooling (same as the controller).
func ClickhouseConfigFromEnv() *ClickhouseConfig {
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		return nil
	}

	db := os.Getenv("CLICKHOUSE_DB")
	if db == "" {
		db = "default"
	}

	user := os.Getenv("CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}

	return &ClickhouseConfig{
		Addr:       addr,
		DB:         db,
		User:       user,
		Pass:       os.Getenv("CLICKHOUSE_PASS"),
		TLSDisable: os.Getenv("CLICKHOUSE_TLS_DISABLED") == "true",
	}
}

// buildClickhouseOptions constructs clickhouse.Options for the HTTP protocol.
// When disableTLS is false (default), TLS is enabled (HTTPS).
// When disableTLS is true, TLS is not set (plain HTTP, for local dev only).
func buildClickhouseOptions(addr, db, user, pass string, disableTLS bool) *clickhouse.Options {
	// Strip URL scheme if present — clickhouse-go expects host:port only
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "http://")

	opts := &clickhouse.Options{
		Protocol: clickhouse.HTTP,
		Addr:     []string{addr},
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: pass,
		},
	}
	if !disableTLS {
		opts.TLS = &tls.Config{}
	}
	return opts
}

// PublishToClickhouse writes per-device results and a summary row to ClickHouse.
// Both tables are created automatically on first use (CREATE TABLE IF NOT EXISTS).
// If cfg is nil, publishing is skipped silently.
func PublishToClickhouse(ctx context.Context, log *slog.Logger, cfg *ClickhouseConfig, env string, results []DeviceTestResult, duration time.Duration) error {
	if cfg == nil {
		log.Debug("ClickHouse publishing skipped: no configuration")
		return nil
	}

	opts := buildClickhouseOptions(cfg.Addr, cfg.DB, cfg.User, cfg.Pass, cfg.TLSDisable)
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open clickhouse connection: %w", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	if err := createQATables(ctx, conn, cfg.DB); err != nil {
		return err
	}

	if err := insertResults(ctx, conn, cfg.DB, env, results); err != nil {
		return err
	}

	if err := insertMetadata(ctx, conn, cfg.DB, env, results, duration); err != nil {
		return err
	}

	log.Debug("published QA results to ClickHouse", "devices", len(results))
	return nil
}

func createQATables(ctx context.Context, conn clickhouse.Conn, db string) error {
	queries := []string{
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS "%s".qa_alldevices_results (
				timestamp    DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),
				env          LowCardinality(String),
				device_code  LowCardinality(String),
				device_pubkey String,
				success      Bool
			) ENGINE = MergeTree
			PARTITION BY toYYYYMM(timestamp)
			ORDER BY (env, device_code, timestamp)
			TTL toDateTime(timestamp) + INTERVAL 90 DAY
		`, db),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS "%s".qa_alldevices_metadata (
				timestamp       DateTime64(3) CODEC(DoubleDelta, ZSTD(1)),
				env             LowCardinality(String),
				devices_tested  UInt32,
				devices_success UInt32,
				devices_failed  UInt32,
				duration_s      Float64
			) ENGINE = MergeTree
			PARTITION BY toYYYYMM(timestamp)
			ORDER BY (env, timestamp)
			TTL toDateTime(timestamp) + INTERVAL 90 DAY
		`, db),
	}

	for _, q := range queries {
		if err := conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("failed to create QA table: %w", err)
		}
	}
	return nil
}

func insertResults(ctx context.Context, conn clickhouse.Conn, db, env string, results []DeviceTestResult) error {
	if len(results) == 0 {
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".qa_alldevices_results (timestamp, env, device_code, device_pubkey, success)`, db,
	))
	if err != nil {
		return fmt.Errorf("failed to prepare results batch: %w", err)
	}

	now := time.Now()
	for _, r := range results {
		if err := batch.Append(now, env, r.DeviceCode, r.DevicePubkey, r.Success); err != nil {
			return fmt.Errorf("failed to append result row: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		_ = batch.Close()
		return fmt.Errorf("failed to send results batch: %w", err)
	}
	return batch.Close()
}

func insertMetadata(ctx context.Context, conn clickhouse.Conn, db, env string, results []DeviceTestResult, duration time.Duration) error {
	var successCount, failedCount uint32
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	batch, err := conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".qa_alldevices_metadata (timestamp, env, devices_tested, devices_success, devices_failed, duration_s)`, db,
	))
	if err != nil {
		return fmt.Errorf("failed to prepare metadata batch: %w", err)
	}

	if err := batch.Append(time.Now(), env, uint32(len(results)), successCount, failedCount, duration.Seconds()); err != nil {
		return fmt.Errorf("failed to append metadata row: %w", err)
	}

	if err := batch.Send(); err != nil {
		_ = batch.Close()
		return fmt.Errorf("failed to send metadata batch: %w", err)
	}
	return batch.Close()
}
