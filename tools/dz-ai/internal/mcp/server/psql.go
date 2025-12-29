package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
)

// PostgresDBForQuerier implements duck.DB interface using PostgreSQL connection to querier server
type PostgresDBForQuerier struct {
	db      *sql.DB
	catalog string
	schema  string
}

// PostgresConnForQuerier implements duck.Connection interface
type PostgresConnForQuerier struct {
	conn *sql.Conn
	db   *PostgresDBForQuerier
	mu   sync.Mutex
}

func (p *PostgresDBForQuerier) Catalog() string {
	return p.catalog
}

func (p *PostgresDBForQuerier) Schema() string {
	return p.schema
}

func (p *PostgresDBForQuerier) Close() error {
	return p.db.Close()
}

func (p *PostgresDBForQuerier) Conn(ctx context.Context) (duck.Connection, error) {
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	return &PostgresConnForQuerier{
		conn: conn,
		db:   p,
	}, nil
}

func (c *PostgresConnForQuerier) DB() duck.DB {
	return c.db
}

func (c *PostgresConnForQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.ExecContext(ctx, query, args...)
}

func (c *PostgresConnForQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c *PostgresConnForQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

func (c *PostgresConnForQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.conn.BeginTx(ctx, opts)
}

func (c *PostgresConnForQuerier) Close() error {
	return c.conn.Close()
}

// NewPostgresDBForQuerier creates a PostgreSQL-backed duck.DB that connects to the querier server
func NewPostgresDBForQuerier(ctx context.Context, dsn string, log *slog.Logger) (duck.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Configure connection pool settings to prevent hanging connections
	// Set reasonable timeouts to avoid long delays
	db.SetMaxOpenConns(25)                 // Maximum number of open connections
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Maximum connection lifetime
	db.SetConnMaxIdleTime(1 * time.Minute) // Maximum idle time before closing

	// Test the connection with SELECT 1
	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to test postgres connection: %w", err)
	}
	if result != 1 {
		db.Close()
		return nil, fmt.Errorf("unexpected result from connection test: got %d, expected 1", result)
	}

	// Get current database and schema
	row := db.QueryRowContext(ctx, "SELECT current_database(), current_schema()")
	var catalog, schema string
	err = row.Scan(&catalog, &schema)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get current database and schema: %w", err)
	}

	return &PostgresDBForQuerier{
		db:      db,
		catalog: catalog,
		schema:  schema,
	}, nil
}
