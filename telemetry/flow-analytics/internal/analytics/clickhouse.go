package analytics

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	defaultDialTimeout      = 10 * time.Second
	defaultMaxExecutionTime = 60
)

// Querier defines the interface for executing ClickHouse queries.
type Querier interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
	Ping(ctx context.Context) error
}

// ClickHouseClient implements Querier using the ClickHouse driver.
type ClickHouseClient struct {
	conn   driver.Conn
	logger *slog.Logger
}

// ClickHouseOption configures the ClickHouseClient.
type ClickHouseOption func(*clickHouseConfig)

type clickHouseConfig struct {
	addr     string
	database string
	username string
	password string
	secure   bool
	logger   *slog.Logger
}

// WithAddr sets the ClickHouse address.
func WithAddr(addr string) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.addr = addr
	}
}

// WithDatabase sets the ClickHouse database.
func WithDatabase(database string) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.database = database
	}
}

// WithUser sets the ClickHouse username.
func WithUser(username string) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.username = username
	}
}

// WithPassword sets the ClickHouse password.
func WithPassword(password string) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.password = password
	}
}

// WithSecure enables TLS for the connection.
func WithSecure(secure bool) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.secure = secure
	}
}

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) ClickHouseOption {
	return func(c *clickHouseConfig) {
		c.logger = logger
	}
}

// NewClickHouseClient creates a new ClickHouse client.
func NewClickHouseClient(opts ...ClickHouseOption) (*ClickHouseClient, error) {
	cfg := &clickHouseConfig{
		addr:     "localhost:9000",
		database: "default",
		username: "default",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}

	chOpts := &clickhouse.Options{
		Addr: []string{cfg.addr},
		Auth: clickhouse.Auth{
			Database: cfg.database,
			Username: cfg.username,
			Password: cfg.password,
		},
		Debug: false,
		Settings: clickhouse.Settings{
			"max_execution_time": defaultMaxExecutionTime,
		},
		DialTimeout: defaultDialTimeout,
	}

	if cfg.secure {
		chOpts.TLS = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	return &ClickHouseClient{
		conn:   conn,
		logger: cfg.logger,
	}, nil
}

// Query executes a query and returns the result rows.
func (c *ClickHouseClient) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

// Ping tests the connection to ClickHouse.
func (c *ClickHouseClient) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}
