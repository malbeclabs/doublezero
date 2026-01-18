package clickhouse

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/pressly/goose/v3"

	"github.com/malbeclabs/doublezero/lake/indexer"
)

func CreateDatabase(ctx context.Context, log *slog.Logger, conn Connection, database string) error {
	log.Info("creating ClickHouse database", "database", database)
	return conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", database))
}

// MigrationConfig holds the configuration for running migrations
type MigrationConfig struct {
	Addr     string
	Database string
	Username string
	Password string
	Secure   bool
}

// slogGooseLogger adapts slog.Logger to goose.Logger interface
type slogGooseLogger struct {
	log *slog.Logger
}

func (l *slogGooseLogger) Fatalf(format string, v ...any) {
	l.log.Error(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

func (l *slogGooseLogger) Printf(format string, v ...any) {
	l.log.Info(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

// RunMigrations executes all SQL migration files using goose (alias for Up)
func RunMigrations(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	return Up(ctx, log, cfg)
}

// Up runs all pending migrations
func Up(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("running ClickHouse migrations (up)")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "db/clickhouse/migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("ClickHouse migrations completed successfully")
	return nil
}

// UpTo runs migrations up to a specific version
func UpTo(ctx context.Context, log *slog.Logger, cfg MigrationConfig, version int64) error {
	log.Info("running ClickHouse migrations up to version", "version", version)

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.UpToContext(ctx, db, "db/clickhouse/migrations", version); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("ClickHouse migrations completed successfully", "version", version)
	return nil
}

// Down rolls back the most recent migration
func Down(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("rolling back ClickHouse migration (down)")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.DownContext(ctx, db, "db/clickhouse/migrations"); err != nil {
		return fmt.Errorf("failed to roll back migration: %w", err)
	}

	log.Info("ClickHouse migration rolled back successfully")
	return nil
}

// DownTo rolls back migrations to a specific version
func DownTo(ctx context.Context, log *slog.Logger, cfg MigrationConfig, version int64) error {
	log.Info("rolling back ClickHouse migrations to version", "version", version)

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.DownToContext(ctx, db, "db/clickhouse/migrations", version); err != nil {
		return fmt.Errorf("failed to roll back migrations: %w", err)
	}

	log.Info("ClickHouse migrations rolled back successfully", "version", version)
	return nil
}

// Redo rolls back the most recent migration and re-applies it
func Redo(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("redoing ClickHouse migration (down + up)")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.RedoContext(ctx, db, "db/clickhouse/migrations"); err != nil {
		return fmt.Errorf("failed to redo migration: %w", err)
	}

	log.Info("ClickHouse migration redone successfully")
	return nil
}

// Reset rolls back all migrations
func Reset(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("resetting ClickHouse migrations (rolling back all)")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.ResetContext(ctx, db, "db/clickhouse/migrations"); err != nil {
		return fmt.Errorf("failed to reset migrations: %w", err)
	}

	log.Info("ClickHouse migrations reset successfully")
	return nil
}

// Version prints the current migration version
func Version(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.VersionContext(ctx, db, "db/clickhouse/migrations"); err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	return nil
}

// MigrationStatus returns the status of all migrations
func MigrationStatus(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("checking ClickHouse migration status")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection: %w", err)
	}
	defer db.Close()

	// Set up goose with our logger
	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.ClickHouseMigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	return goose.StatusContext(ctx, db, "db/clickhouse/migrations")
}

// newSQLDB creates a database/sql compatible connection for goose
func newSQLDB(cfg MigrationConfig) (*sql.DB, error) {
	options := &clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
	}

	if cfg.Secure {
		options.TLS = &tls.Config{}
	}

	return clickhouse.OpenDB(options), nil
}
