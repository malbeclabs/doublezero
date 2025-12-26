package duck

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/duckdb/duckdb-go/v2"
)

type LocalLake struct {
	log     *slog.Logger
	db      *sql.DB
	catalog string
	schema  string
}

type LocalLakeConnection struct {
	conn *sql.Conn
	db   *LocalLake
}

func (c *LocalLakeConnection) DB() DB {
	return c.db
}

func (c *LocalLakeConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c *LocalLakeConnection) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c *LocalLakeConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

func (c *LocalLakeConnection) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.conn.BeginTx(ctx, opts)
}

func (c *LocalLakeConnection) Close() error {
	return c.conn.Close()
}

func NewLocalLake(ctx context.Context, log *slog.Logger, catalogName, catalogPath, dataDir string) (*LocalLake, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	_, err = db.Exec(`
		INSTALL 'sqlite';
		INSTALL 'ducklake';
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to install extensions: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("ATTACH 'ducklake:sqlite:%s' AS %s (DATA_PATH '%s')", catalogPath, catalogName, dataDir))
	if err != nil {
		return nil, fmt.Errorf("failed to attach ducklake: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("USE %s", catalogName))
	if err != nil {
		return nil, fmt.Errorf("failed to use catalog: %w", err)
	}

	row := db.QueryRowContext(ctx, "SELECT current_database() AS catalog, current_schema() AS schema")
	var catalog, schema string
	err = row.Scan(&catalog, &schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get current database and schema: %w", err)
	}

	return &LocalLake{
		log:     log,
		db:      db,
		catalog: catalogName,
		schema:  schema,
	}, nil
}

func (l *LocalLake) Catalog() string {
	return l.catalog
}

func (l *LocalLake) Schema() string {
	return l.schema
}

func (l *LocalLake) Close() error {
	return l.db.Close()
}

func (l *LocalLake) Conn(ctx context.Context) (Connection, error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "USE "+l.catalog); err != nil {
		return nil, fmt.Errorf("USE failed: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "SET schema = "+l.schema); err != nil {
		return nil, fmt.Errorf("SET schema failed: %w", err)
	}
	return &LocalLakeConnection{
		conn: conn,
		db:   l,
	}, nil
}
