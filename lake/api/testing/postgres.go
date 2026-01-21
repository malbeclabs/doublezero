package apitesting

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// DBConfig holds the PostgreSQL test container configuration.
type DBConfig struct {
	Database       string
	Username       string
	Password       string
	ContainerImage string
}

// DB represents a PostgreSQL test container.
type DB struct {
	log       *slog.Logger
	cfg       *DBConfig
	connStr   string
	container *tcpostgres.PostgresContainer
}

// ConnStr returns the PostgreSQL connection string.
func (db *DB) ConnStr() string {
	return db.connStr
}

// Close terminates the PostgreSQL container.
func (db *DB) Close() {
	terminateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.container.Terminate(terminateCtx); err != nil {
		db.log.Error("failed to terminate PostgreSQL container", "error", err)
	}
}

func (cfg *DBConfig) Validate() error {
	if cfg.Database == "" {
		cfg.Database = "test"
	}
	if cfg.Username == "" {
		cfg.Username = "test"
	}
	if cfg.Password == "" {
		cfg.Password = "test"
	}
	if cfg.ContainerImage == "" {
		cfg.ContainerImage = "postgres:16-alpine"
	}
	return nil
}

// NewDB creates a new PostgreSQL testcontainer.
func NewDB(ctx context.Context, log *slog.Logger, cfg *DBConfig) (*DB, error) {
	if cfg == nil {
		cfg = &DBConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate DB config: %w", err)
	}

	// Retry container start up to 3 times for retryable errors
	var container *tcpostgres.PostgresContainer
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		container, err = tcpostgres.Run(ctx,
			cfg.ContainerImage,
			tcpostgres.WithDatabase(cfg.Database),
			tcpostgres.WithUsername(cfg.Username),
			tcpostgres.WithPassword(cfg.Password),
			tcpostgres.BasicWaitStrategies(),
			tcpostgres.WithSQLDriver("pgx"),
		)
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < 3 {
				time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("failed to start PostgreSQL container after retries: %w", lastErr)
		}
		break
	}

	if container == nil {
		return nil, fmt.Errorf("failed to start PostgreSQL container after retries: %w", lastErr)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get PostgreSQL connection string: %w", err)
	}

	db := &DB{
		log:       log,
		cfg:       cfg,
		connStr:   connStr,
		container: container,
	}

	return db, nil
}

// SetupTestDB sets up a test database with migrations and configures config.PgPool.
// Returns a cleanup function that should be called when done.
func SetupTestDB(t *testing.T, db *DB) {
	ctx := t.Context()

	// Run migrations
	goose.SetBaseFS(config.EmbedMigrations)
	sqlDB, err := sql.Open("pgx", db.connStr)
	require.NoError(t, err, "failed to open database for migrations")

	err = goose.SetDialect("postgres")
	require.NoError(t, err, "failed to set goose dialect")

	err = goose.Up(sqlDB, "migrations")
	require.NoError(t, err, "failed to run migrations")
	sqlDB.Close()

	// Create pgxpool and set config.PgPool
	poolConfig, err := pgxpool.ParseConfig(db.connStr)
	require.NoError(t, err, "failed to parse pool config")

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err, "failed to create pool")

	// Save old pool and restore on cleanup
	oldPool := config.PgPool
	config.PgPool = pool

	t.Cleanup(func() {
		pool.Close()
		config.PgPool = oldPool
	})
}

// NewTestPool creates a new pgxpool connected to the test container.
func NewTestPool(t *testing.T, db *DB) *pgxpool.Pool {
	ctx := t.Context()

	poolConfig, err := pgxpool.ParseConfig(db.connStr)
	require.NoError(t, err, "failed to parse pool config")

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err, "failed to create pool")

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func isRetryableContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http://%2Fvar%2Frun%2Fdocker.sock")
}

// WaitForPostgres is a convenience wait strategy for PostgreSQL.
func WaitForPostgres() *wait.LogStrategy {
	return wait.ForLog("database system is ready to accept connections").
		WithOccurrence(2).
		WithStartupTimeout(60 * time.Second)
}
