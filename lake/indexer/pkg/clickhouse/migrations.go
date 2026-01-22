package clickhouse

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

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

// RunMigrations executes all SQL migration files using goose (alias for Up)
func RunMigrations(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	return Up(ctx, log, cfg)
}

// newProvider creates a new goose provider with the given configuration.
// Using the Provider API avoids global state and is concurrent-safe.
func newProvider(db *sql.DB) (*goose.Provider, error) {
	migrationsFS, err := fs.Sub(indexer.ClickHouseMigrationsFS, "db/clickhouse/migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migrations sub-filesystem: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectClickHouse, db, migrationsFS)
	if err != nil {
		return nil, fmt.Errorf("failed to create goose provider: %w", err)
	}

	return provider, nil
}

// Up runs all pending migrations
func Up(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	log.Info("running ClickHouse migrations (up)")

	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	if _, err := provider.Up(ctx); err != nil {
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	if _, err := provider.UpTo(ctx, version); err != nil {
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	if _, err := provider.Down(ctx); err != nil {
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	if _, err := provider.DownTo(ctx, version); err != nil {
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	// Provider doesn't have Redo, so we do Down then Up
	if _, err := provider.Down(ctx); err != nil {
		return fmt.Errorf("failed to roll back migration: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("failed to re-apply migration: %w", err)
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	// Roll back to version 0 (all migrations)
	if _, err := provider.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("failed to reset migrations: %w", err)
	}

	log.Info("ClickHouse migrations reset successfully")
	return nil
}

// Version returns the current migration version
func Version(ctx context.Context, log *slog.Logger, cfg MigrationConfig) error {
	db, err := newSQLDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to create database connection for migrations: %w", err)
	}
	defer db.Close()

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	log.Info("current migration version", "version", version)
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

	provider, err := newProvider(db)
	if err != nil {
		return err
	}

	statuses, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	for _, s := range statuses {
		state := "pending"
		if s.State == goose.StateApplied {
			state = "applied"
		}
		log.Info("migration", "version", s.Source.Version, "state", state, "path", s.Source.Path)
	}

	return nil
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
