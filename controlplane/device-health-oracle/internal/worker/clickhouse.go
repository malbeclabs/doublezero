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
// Tables the lake indexer owns live in their own database (lake/lake_testnet),
// apart from the telemetry tables in db.
type ClickHouseClient struct {
	conn   clickhouse.Conn
	db     string
	lakeDB string
}

func options(addr, db, user, pass string, disableTLS bool) *clickhouse.Options {
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
		// Stay below ClickHouse Cloud's ~5m server-side idle close (driver default is 1h).
		ConnMaxLifetime: 3 * time.Minute,
	}
	if !disableTLS {
		opts.TLS = &tls.Config{}
	}
	return opts
}

// NewClickHouseClient does not contact the server: the connection opens lazily,
// so an unreachable server surfaces per query, where every criterion holds. Use
// Ping to report reachability at startup.
func NewClickHouseClient(addr, db, lakeDB, user, pass string, disableTLS bool) (*ClickHouseClient, error) {
	for _, name := range []string{db, lakeDB} {
		if !validDBName.MatchString(name) {
			return nil, fmt.Errorf("invalid clickhouse database name: %q", name)
		}
	}

	conn, err := clickhouse.Open(options(addr, db, user, pass, disableTLS))
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	return &ClickHouseClient{conn: conn, db: db, lakeDB: lakeDB}, nil
}

// Ping reports whether the server is reachable.
func (c *ClickHouseClient) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
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
		c.lakeDB,
	)

	var minutesWithRecords uint64
	err := c.conn.QueryRow(ctx, query, devicePubkey, start, end).Scan(&minutesWithRecords)
	if err != nil {
		return 0, fmt.Errorf("clickhouse query: %w", err)
	}

	return int64(minutesWithRecords), nil
}

// linkStatusHardDrained is the rollup's status value for a hard-drained link.
// Hard drain sets isis passive, so isis_down reads true for as long as the link
// stays drained and says nothing about the link itself.
const linkStatusHardDrained = "hard-drained"

// LinkHealthRecentResult is a rollup bucket's health fields, returned by
// LinkHealthChecker.LinkHealthRecent.
type LinkHealthRecentResult struct {
	BucketTs time.Time
	IsisDown bool
	Status   string
	ALossPct float64
	ZLossPct float64
	ASamples uint32
	ZSamples uint32
}

// LinkHealthWindowResult summarises a recovery-window check. AllClean is true
// when no distinct bucket (deduplicated by latest ingested_at) is bad. Total
// and MaxBucketTs cover only buckets with samples from both sides, so a caller
// can require the window to be covered and recent: a frozen pipeline or a dead
// telemetry agent cannot recover a link.
type LinkHealthWindowResult struct {
	Bad         uint64
	Total       uint64
	MaxBucketTs time.Time
	AllClean    bool
}

// LinkHealthChecker queries ClickHouse for link health rollup records.
type LinkHealthChecker interface {
	// LinkHealthRecent returns the newest link_rollup_5m bucket for the given
	// link in [start, end]. Multiple rows for the same bucket are disambiguated
	// by ingested_at (most recently ingested wins). Returns found=false when
	// the link has no bucket in the range, or when the newest one is still
	// provisioning — a link being brought up should hold its current health
	// rather than be judged on an older bucket.
	LinkHealthRecent(ctx context.Context, linkPubkey string, start, end time.Time) (result LinkHealthRecentResult, found bool, err error)

	// LinkHealthWindowAllClean returns true if no distinct non-provisioning
	// bucket in [start, end] is bad: ISIS down on a link that is not hard-drained,
	// or both sides sampled with loss above the threshold on both. Late-arriving
	// rows for the same bucket are deduplicated by ingested_at so a corrected
	// re-write doesn't keep a link Impaired even after every distinct bucket
	// reads as clean. Returns found=false when the window has no bucket with
	// samples from both sides.
	LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (result LinkHealthWindowResult, found bool, err error)
}

// LinkHealthRecent returns the newest bucket's health fields for the given link
// in [start, end], deduplicated by selecting its most recently ingested row, or
// found=false if that row has provisioning=true.
func (c *ClickHouseClient) LinkHealthRecent(ctx context.Context, linkPubkey string, start, end time.Time) (LinkHealthRecentResult, bool, error) {
	// The inner query resolves the link's newest bucket to its newest row
	// (ReplacingMergeTree keeps every version until merge, so ordering by
	// ingested_at is the dedup). The provisioning filter must sit outside it:
	// in the WHERE clause it would run before the dedup, so a bucket whose
	// latest row flips provisioning would fall back to a stale version.
	// The table is ordered by (bucket_ts, link_pk), so without the bucket_ts
	// bounds a link with no recent rows scans its whole history every tick.
	query := fmt.Sprintf(
		`SELECT bucket_ts, isis_down, status, a_loss_pct, z_loss_pct, a_samples, z_samples
		 FROM (
		   SELECT bucket_ts, isis_down, status, a_loss_pct, z_loss_pct, a_samples, z_samples, provisioning
		   FROM "%s".link_rollup_5m
		   WHERE link_pk = ?
		     AND bucket_ts >= ?
		     AND bucket_ts <= ?
		   ORDER BY bucket_ts DESC, ingested_at DESC
		   LIMIT 1
		 )
		 WHERE provisioning = false`,
		c.lakeDB,
	)

	var r LinkHealthRecentResult
	err := c.conn.QueryRow(ctx, query, linkPubkey, start, end).Scan(&r.BucketTs, &r.IsisDown, &r.Status, &r.ALossPct, &r.ZLossPct, &r.ASamples, &r.ZSamples)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinkHealthRecentResult{}, false, nil
		}
		return LinkHealthRecentResult{}, false, fmt.Errorf("clickhouse query: %w", err)
	}
	return r, true, nil
}

// LinkHealthWindowAllClean returns whether every distinct bucket for the given
// link in [start, end] is clean, aggregating in ClickHouse rather than
// streaming buckets back.
func (c *ClickHouseClient) LinkHealthWindowAllClean(ctx context.Context, linkPubkey string, start, end time.Time, lossThreshold float64) (LinkHealthWindowResult, bool, error) {
	// provisioning is carried through argMax and filtered after the GROUP BY,
	// for the reason given in LinkHealthRecent: filtered in the WHERE clause it
	// would pre-empt the dedup. The bad-bucket predicate mirrors checkImpairment.
	query := fmt.Sprintf(
		`SELECT
		   countIf((isis_down = true AND status != '%s')
		     OR (a_samples > 0 AND z_samples > 0 AND a_loss_pct > ? AND z_loss_pct > ?)) AS bad_buckets,
		   countIf(a_samples > 0 AND z_samples > 0) AS total_buckets,
		   maxIf(bucket_ts, a_samples > 0 AND z_samples > 0) AS max_bucket_ts
		 FROM (
		   SELECT
		     bucket_ts,
		     argMax(isis_down, ingested_at)     AS isis_down,
		     argMax(status, ingested_at)        AS status,
		     argMax(a_loss_pct, ingested_at)    AS a_loss_pct,
		     argMax(z_loss_pct, ingested_at)    AS z_loss_pct,
		     argMax(a_samples, ingested_at)     AS a_samples,
		     argMax(z_samples, ingested_at)     AS z_samples,
		     argMax(provisioning, ingested_at)  AS provisioning
		   FROM "%s".link_rollup_5m
		   WHERE link_pk = ?
		     AND bucket_ts >= ?
		     AND bucket_ts <= ?
		   GROUP BY bucket_ts
		 )
		 WHERE provisioning = false`,
		linkStatusHardDrained,
		c.lakeDB,
	)

	var r LinkHealthWindowResult
	err := c.conn.QueryRow(ctx, query, lossThreshold, lossThreshold, linkPubkey, start, end).Scan(&r.Bad, &r.Total, &r.MaxBucketTs)
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
