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

// LinkHealthRecentResult is the most recent rollup bucket's health fields plus
// the bucket timestamp, returned by LinkHealthChecker.LinkHealthRecent. The
// bucket timestamp lets callers enforce a recency floor (a stale bucket means
// the telemetry pipeline is broken — neither demote nor recover on stale data).
type LinkHealthRecentResult struct {
	BucketTs time.Time
	IsisDown bool
	ALossPct float64
	ZLossPct float64
}

// LinkHealthWindowResult summarises a recovery-window check. AllClean is true
// when every distinct bucket (deduplicated by latest ingested_at) is clean.
// Bad and Total are exposed for operator diagnostics.
type LinkHealthWindowResult struct {
	Bad      uint64
	Total    uint64
	AllClean bool
}

// LinkHealthChecker queries ClickHouse for link health rollup records.
type LinkHealthChecker interface {
	// LinkHealthRecent returns the most recent non-provisioning link_rollup_5m
	// bucket for the given link. Multiple rows for the same bucket are
	// disambiguated by ingested_at (most recently ingested wins). Returns
	// found=false when there is no data for the link.
	LinkHealthRecent(ctx context.Context, linkPubkey string) (result LinkHealthRecentResult, found bool, err error)

	// LinkHealthWindowAllClean returns true if every distinct non-provisioning
	// bucket in [start, end] is clean (isis_down=false AND a_loss_pct <=
	// threshold AND z_loss_pct <= threshold). Late-arriving rows for the same
	// bucket are deduplicated by ingested_at so a corrected re-write doesn't
	// keep a link Impaired even after every distinct bucket reads as clean.
	// Returns found=false when there are no buckets in the window for this link.
	LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (result LinkHealthWindowResult, found bool, err error)
}

// LinkHealthRecent returns the latest bucket's health fields for the given
// link, ignoring buckets where provisioning=true. Multiple rows for the same
// bucket are disambiguated by selecting the most recently ingested row.
func (c *ClickHouseClient) LinkHealthRecent(ctx context.Context, linkPubkey string) (LinkHealthRecentResult, bool, error) {
	query := fmt.Sprintf(
		`SELECT bucket_ts, isis_down, a_loss_pct, z_loss_pct
		 FROM "%s".link_rollup_5m
		 WHERE link_pk = ?
		   AND provisioning = false
		 ORDER BY bucket_ts DESC, ingested_at DESC
		 LIMIT 1`,
		c.db,
	)

	var r LinkHealthRecentResult
	err := c.conn.QueryRow(ctx, query, linkPubkey).Scan(&r.BucketTs, &r.IsisDown, &r.ALossPct, &r.ZLossPct)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinkHealthRecentResult{}, false, nil
		}
		return LinkHealthRecentResult{}, false, fmt.Errorf("clickhouse query: %w", err)
	}
	return r, true, nil
}

// LinkHealthWindowAllClean returns whether every distinct bucket for the given
// link in [start, end] is clean. The inner query deduplicates late-arriving
// rows by selecting the latest ingested_at per bucket; the outer aggregate
// counts distinct dirty buckets without streaming rows.
func (c *ClickHouseClient) LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (LinkHealthWindowResult, bool, error) {
	query := fmt.Sprintf(
		`SELECT
		   countIf(isis_down = true OR a_loss_pct > ? OR z_loss_pct > ?) AS bad_buckets,
		   count() AS total_buckets
		 FROM (
		   SELECT
		     bucket_ts,
		     argMax(isis_down, ingested_at)   AS isis_down,
		     argMax(a_loss_pct, ingested_at)  AS a_loss_pct,
		     argMax(z_loss_pct, ingested_at)  AS z_loss_pct
		   FROM "%s".link_rollup_5m
		   WHERE link_pk = ?
		     AND bucket_ts >= ?
		     AND bucket_ts <= ?
		     AND provisioning = false
		   GROUP BY bucket_ts
		 )`,
		c.db,
	)

	var r LinkHealthWindowResult
	err := c.conn.QueryRow(ctx, query, lossThreshold, lossThreshold, linkPubkey, start, end).Scan(&r.Bad, &r.Total)
	if err != nil {
		return LinkHealthWindowResult{}, false, fmt.Errorf("clickhouse query: %w", err)
	}
	if r.Total == 0 {
		return LinkHealthWindowResult{}, false, nil
	}
	r.AllClean = r.Bad == 0
	return r, true, nil
}

func (c *ClickHouseClient) Close() error {
	return c.conn.Close()
}
