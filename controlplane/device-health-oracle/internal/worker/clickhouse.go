package worker

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
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

// ClickHouseClient wraps a connection for reading device health data from ClickHouse.
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

// LinkHealthChecker queries ClickHouse for link health rollup records.
type LinkHealthChecker interface {
	// LinkHealthRecent returns the most recent non-provisioning link_rollup_5m
	// bucket for the given link. Multiple rows for the same bucket are
	// disambiguated by ingested_at (most recently ingested wins). Returns
	// found=false when there is no data for the link.
	LinkHealthRecent(ctx context.Context, linkPubkey string) (isisDown bool, aLossPct, zLossPct float64, found bool, err error)

	// LinkHealthWindowAllClean returns true if every non-provisioning bucket in
	// [start, end] is clean (isis_down=false AND a_loss_pct <= threshold AND
	// z_loss_pct <= threshold). The threshold filter is pushed into the query
	// so the result is a single bool and we do not stream rows. Returns
	// found=false when there are no buckets in the window for this link.
	LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (allClean bool, found bool, err error)
}

// LinkHealthRecent returns the latest closed bucket's health fields for the
// given link, ignoring buckets where provisioning=true.
func (c *ClickHouseClient) LinkHealthRecent(ctx context.Context, linkPubkey string) (bool, float64, float64, bool, error) {
	query := fmt.Sprintf(
		`SELECT isis_down, a_loss_pct, z_loss_pct
		 FROM "%s".link_rollup_5m
		 WHERE link_pk = ?
		   AND provisioning = false
		 ORDER BY bucket_ts DESC, ingested_at DESC
		 LIMIT 1`,
		c.db,
	)

	var (
		isisDown bool
		aLossPct float64
		zLossPct float64
	)
	err := c.conn.QueryRow(ctx, query, linkPubkey).Scan(&isisDown, &aLossPct, &zLossPct)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, 0, false, nil
		}
		return false, 0, 0, false, fmt.Errorf("clickhouse query: %w", err)
	}
	return isisDown, aLossPct, zLossPct, true, nil
}

// LinkHealthWindowAllClean returns true when every non-provisioning bucket for
// the given link in [start, end] is clean. The "any bad row" sentinel pattern
// keeps the query result a single integer regardless of window length.
func (c *ClickHouseClient) LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (bool, bool, error) {
	query := fmt.Sprintf(
		`SELECT
		   countIf(isis_down = true OR a_loss_pct > ? OR z_loss_pct > ?) AS bad_buckets,
		   count() AS total_buckets
		 FROM "%s".link_rollup_5m
		 WHERE link_pk = ?
		   AND bucket_ts >= ?
		   AND bucket_ts <= ?
		   AND provisioning = false`,
		c.db,
	)

	var bad, total uint64
	err := c.conn.QueryRow(ctx, query, lossThreshold, lossThreshold, linkPubkey, start, end).Scan(&bad, &total)
	if err != nil {
		return false, false, fmt.Errorf("clickhouse query: %w", err)
	}
	if total == 0 {
		return false, false, nil
	}
	return bad == 0, true, nil
}

func (c *ClickHouseClient) Close() error {
	return c.conn.Close()
}
