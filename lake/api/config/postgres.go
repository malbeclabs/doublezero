package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgPool is the global PostgreSQL connection pool
var PgPool *pgxpool.Pool

// PgConfig holds the PostgreSQL configuration
type PgConfig struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
}

// pgCfg holds the parsed configuration
var pgCfg PgConfig

// LoadPostgres initializes the PostgreSQL connection pool
func LoadPostgres() error {
	pgCfg.Host = os.Getenv("POSTGRES_HOST")
	if pgCfg.Host == "" {
		pgCfg.Host = "localhost"
	}

	pgCfg.Port = os.Getenv("POSTGRES_PORT")
	if pgCfg.Port == "" {
		pgCfg.Port = "5432"
	}

	pgCfg.Database = os.Getenv("POSTGRES_DB")
	if pgCfg.Database == "" {
		pgCfg.Database = "lakedev"
	}

	pgCfg.Username = os.Getenv("POSTGRES_USER")
	if pgCfg.Username == "" {
		pgCfg.Username = "lakedev"
	}

	pgCfg.Password = os.Getenv("POSTGRES_PASSWORD")
	if pgCfg.Password == "" {
		pgCfg.Password = "lakedev"
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		pgCfg.Username, pgCfg.Password, pgCfg.Host, pgCfg.Port, pgCfg.Database,
	)

	log.Printf("Connecting to PostgreSQL: host=%s, port=%s, database=%s, username=%s",
		pgCfg.Host, pgCfg.Port, pgCfg.Database, pgCfg.Username)

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse postgres config: %w", err)
	}

	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	PgPool = pool
	log.Printf("Connected to PostgreSQL successfully")

	// Run migrations
	if err := runMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// runMigrations creates the required database tables
func runMigrations(ctx context.Context) error {
	log.Printf("Running PostgreSQL migrations...")

	// Create sessions table
	_, err := PgPool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS sessions (
			id UUID PRIMARY KEY,
			type VARCHAR(20) NOT NULL CHECK (type IN ('chat', 'query')),
			name VARCHAR(255),
			content JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	// Create index for listing sessions
	_, err = PgPool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_sessions_type_updated
		ON sessions (type, updated_at DESC)
	`)
	if err != nil {
		return fmt.Errorf("failed to create sessions index: %w", err)
	}

	// Add lock columns for cross-browser request coordination
	_, err = PgPool.Exec(ctx, `
		ALTER TABLE sessions
		ADD COLUMN IF NOT EXISTS lock_id VARCHAR(36),
		ADD COLUMN IF NOT EXISTS lock_until TIMESTAMPTZ,
		ADD COLUMN IF NOT EXISTS lock_question TEXT
	`)
	if err != nil {
		return fmt.Errorf("failed to add lock columns: %w", err)
	}

	// Create or replace the trigger function
	_, err = PgPool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ language 'plpgsql'
	`)
	if err != nil {
		return fmt.Errorf("failed to create trigger function: %w", err)
	}

	// Create trigger (drop first to avoid conflicts)
	_, _ = PgPool.Exec(ctx, `DROP TRIGGER IF EXISTS update_sessions_updated_at ON sessions`)
	_, err = PgPool.Exec(ctx, `
		CREATE TRIGGER update_sessions_updated_at
			BEFORE UPDATE ON sessions
			FOR EACH ROW
			EXECUTE FUNCTION update_updated_at_column()
	`)
	if err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	log.Printf("PostgreSQL migrations completed")
	return nil
}

// ClosePostgres closes the PostgreSQL connection pool
func ClosePostgres() {
	if PgPool != nil {
		PgPool.Close()
	}
}
