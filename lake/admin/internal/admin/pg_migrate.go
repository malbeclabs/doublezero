package admin

import (
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx driver with database/sql
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/pressly/goose/v3"
)

// PgMigrateConfig holds configuration for PostgreSQL migrations
type PgMigrateConfig struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
	SSLMode  string
}

// PgMigrateUp runs all pending PostgreSQL migrations
func PgMigrateUp(log *slog.Logger, cfg PgMigrateConfig) error {
	db, err := openPgDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(config.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	log.Info("running PostgreSQL migrations (up)")
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Info("PostgreSQL migrations completed")
	return nil
}

// PgMigrateDown rolls back the last PostgreSQL migration
func PgMigrateDown(log *slog.Logger, cfg PgMigrateConfig) error {
	db, err := openPgDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(config.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	log.Info("rolling back PostgreSQL migration (down)")
	if err := goose.Down(db, "migrations"); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	log.Info("PostgreSQL migration rollback completed")
	return nil
}

// PgMigrateStatus shows the status of all PostgreSQL migrations
func PgMigrateStatus(log *slog.Logger, cfg PgMigrateConfig) error {
	db, err := openPgDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(config.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	log.Info("PostgreSQL migration status")
	if err := goose.Status(db, "migrations"); err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	return nil
}

func openPgDB(cfg PgMigrateConfig) (*sql.DB, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, sslMode,
	)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
