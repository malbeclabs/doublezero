package duck

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
)

type DB interface {
	Catalog() string
	Schema() string
	Close() error
	Conn(ctx context.Context) (Connection, error)
}

type Connection interface {
	DB() DB
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	Close() error
}

type duckDB struct {
	dbPath  string
	db      *sql.DB
	catalog string
	schema  string
}

type duckDBConn struct {
	conn    *sql.Conn
	db      *duckDB
	writeMu sync.Mutex // serializes all write operations
}

func (d *duckDB) Conn(ctx context.Context) (Connection, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "USE "+d.catalog); err != nil {
		return nil, fmt.Errorf("failed to use database: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "SET schema = "+d.schema); err != nil {
		return nil, fmt.Errorf("failed to set schema: %w", err)
	}

	return &duckDBConn{
		conn: conn,
		db:   d,
	}, nil
}

func NewDB(ctx context.Context, dbPath string, log *slog.Logger) (*duckDB, error) {
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	row := db.QueryRowContext(ctx, "SELECT current_database() AS catalog, current_schema() AS schema")
	var catalog, schema string
	err = row.Scan(&catalog, &schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get current database and schema: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("USE %s", catalog))
	if err != nil {
		return nil, fmt.Errorf("failed to use database: %w", err)
	}

	return &duckDB{
		dbPath:  dbPath,
		db:      db,
		catalog: catalog,
		schema:  schema,
	}, nil
}

func (d *duckDB) Catalog() string {
	return d.catalog
}

func (d *duckDB) Schema() string {
	return d.schema
}

func (d *duckDB) Close() error {
	return d.db.Close()
}

func (c *duckDBConn) DB() DB {
	return c.db
}

func (c *duckDBConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.conn.ExecContext(ctx, query, args...)
}

func (c *duckDBConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c *duckDBConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

func (c *duckDBConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.conn.BeginTx(ctx, opts)
}

func (c *duckDBConn) Close() error {
	return c.conn.Close()
}
