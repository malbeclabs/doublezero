package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Client represents a ClickHouse database connection
type Client interface {
	Conn(ctx context.Context) (Connection, error)
	Close() error
}

// Connection represents a ClickHouse connection
type Connection interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
	AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error
	PrepareBatch(ctx context.Context, query string) (driver.Batch, error)
	Close() error
}

type client struct {
	conn driver.Conn
	log  *slog.Logger
}

type connection struct {
	conn driver.Conn
}

// NewClient creates a new ClickHouse client
func NewClient(ctx context.Context, log *slog.Logger, addr string, database string, username string, password string) (Client, error) {
	options := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: 5 * time.Second,
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("failed to open ClickHouse connection: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	log.Info("ClickHouse client initialized", "addr", addr, "database", database)

	return &client{
		conn: conn,
		log:  log,
	}, nil
}

func (c *client) Conn(ctx context.Context) (Connection, error) {
	return &connection{conn: c.conn}, nil
}

func (c *client) Close() error {
	return c.conn.Close()
}

func (c *connection) Exec(ctx context.Context, query string, args ...any) error {
	return c.conn.Exec(ctx, query, args...)
}

func (c *connection) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

func (c *connection) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	return c.conn.AsyncInsert(ctx, query, wait, args...)
}

func (c *connection) PrepareBatch(ctx context.Context, query string) (driver.Batch, error) {
	return c.conn.PrepareBatch(ctx, query)
}

func (c *connection) Close() error {
	// Connection is shared, don't close it
	return nil
}
