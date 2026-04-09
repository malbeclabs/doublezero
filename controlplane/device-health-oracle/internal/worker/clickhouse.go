package worker

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// ControllerCallChecker queries ClickHouse for controller call records.
type ControllerCallChecker interface {
	ControllerCallCoverage(ctx context.Context, devicePubkey string, start, end time.Time) (minutesWithCalls int64, err error)
	Close() error
}

// ClickHouseClient wraps a ClickHouse connection for reading controller call data.
type ClickHouseClient struct {
	conn clickhouse.Conn
	db   string
}

// NewClickHouseClient creates a new ClickHouse client connection.
func NewClickHouseClient(addr, db, user, pass string, disableTLS bool) (*ClickHouseClient, error) {
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "http://")

	opts := &clickhouse.Options{
		Protocol: clickhouse.HTTP,
		Addr:     []string{addr},
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: pass,
		},
		MaxOpenConns: 5,
		DialTimeout:  30 * time.Second,
	}
	if !disableTLS {
		opts.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	return &ClickHouseClient{conn: conn, db: db}, nil
}

// ControllerCallCoverage returns the number of distinct minutes in [start, end] that have
// at least one controller_grpc_getconfig_success record for the given device.
func (c *ClickHouseClient) ControllerCallCoverage(ctx context.Context, devicePubkey string, start, end time.Time) (int64, error) {
	query := fmt.Sprintf(
		`SELECT count(DISTINCT toStartOfMinute(timestamp)) AS minutes_with_calls
		 FROM "%s".controller_grpc_getconfig_success
		 WHERE device_pubkey = {pubkey:String}
		   AND timestamp >= {start:DateTime64(3)}
		   AND timestamp <= {end:DateTime64(3)}`,
		c.db,
	)

	var minutesWithCalls int64
	err := c.conn.QueryRow(ctx, query,
		clickhouse.Named("pubkey", devicePubkey),
		clickhouse.Named("start", start),
		clickhouse.Named("end", end),
	).Scan(&minutesWithCalls)
	if err != nil {
		return 0, fmt.Errorf("clickhouse query: %w", err)
	}

	return minutesWithCalls, nil
}

// Close closes the ClickHouse connection.
func (c *ClickHouseClient) Close() error {
	return c.conn.Close()
}
