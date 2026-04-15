package worker

import (
	"context"
	"crypto/tls"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

var validDBName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

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

func NewClickHouseClient(addr, db, user, pass string, disableTLS bool) (*ClickHouseClient, error) {
	if !validDBName.MatchString(db) {
		return nil, fmt.Errorf("invalid clickhouse database name: %q", db)
	}

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
		 WHERE device_pubkey = ?
		   AND timestamp >= ?
		   AND timestamp <= ?`,
		c.db,
	)

	var minutesWithCalls uint64
	err := c.conn.QueryRow(ctx, query, devicePubkey, start, end).Scan(&minutesWithCalls)
	if err != nil {
		return 0, fmt.Errorf("clickhouse query: %w", err)
	}

	return int64(minutesWithCalls), nil
}

// InterfaceCountersChecker queries ClickHouse for device interface counter records.
type InterfaceCountersChecker interface {
	InterfaceCountersCoverage(ctx context.Context, devicePubkey string, start, end time.Time) (minutesWithRecords int64, err error)
}

// InterfaceCountersCoverage returns the number of distinct minutes in [start, end] that have
// at least one fact_dz_device_interface_counters record for the given device.
func (c *ClickHouseClient) InterfaceCountersCoverage(ctx context.Context, devicePubkey string, start, end time.Time) (int64, error) {
	query := fmt.Sprintf(
		`SELECT count(DISTINCT toStartOfMinute(event_ts)) AS minutes_with_records
		 FROM "%s".fact_dz_device_interface_counters
		 WHERE device_pk = ?
		   AND event_ts >= ?
		   AND event_ts <= ?`,
		c.db,
	)

	var minutesWithRecords uint64
	err := c.conn.QueryRow(ctx, query, devicePubkey, start, end).Scan(&minutesWithRecords)
	if err != nil {
		return 0, fmt.Errorf("clickhouse query: %w", err)
	}

	return int64(minutesWithRecords), nil
}

func (c *ClickHouseClient) Close() error {
	return c.conn.Close()
}
