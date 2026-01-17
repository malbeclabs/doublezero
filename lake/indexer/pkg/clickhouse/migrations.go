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

// RunMigrations executes all SQL migration files using goose
func RunMigrations(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("running ClickHouse migrations with goose")

	// Create a database/sql connection for goose
	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	// Set up goose with our logger
	goose.SetLogger(&slogGooseLogger{log: log})
	goose.SetBaseFS(indexer.MigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	// Run migrations
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("ClickHouse migrations completed successfully")
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
	goose.SetBaseFS(indexer.MigrationsFS)

	if err := goose.SetDialect("clickhouse"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	return goose.StatusContext(ctx, db, "migrations")
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
