package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const DefaultDatabase = "default"

// ContextWithSyncInsert returns a context configured for synchronous inserts.
// Use this when you need to read data immediately after inserting.
func ContextWithSyncInsert(ctx context.Context) context.Context {
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"async_insert":                           0,
		"wait_for_async_insert":                  1, // Wait for insert to complete even if async
		"async_insert_use_adaptive_busy_timeout": 0, // Disable adaptive timeout that can override async settings (24.3+)
		"insert_deduplicate":                     0, // Disable deduplication to avoid silent drops
		"select_sequential_consistency":          1, // Ensure reads see latest writes in replicated setups
	}))
}

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
func NewClient(ctx context.Context, log *slog.Logger, addr string, database string, username string, password string, secure bool) (Client, error) {
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

	// Enable TLS for ClickHouse Cloud (port 9440)
	if secure {
		options.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("failed to open ClickHouse connection: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	log.Info("ClickHouse client initialized", "addr", addr, "database", database, "secure", secure)

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
