package allowlist

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

var validDBName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// MaxCallerRows bounds the caller read. Any address may send a device's
// pubkey, so a flood of distinct callers could otherwise load an unbounded
// result before the per-device cap applies. About 100 devices calling from 1
// to 3 addresses each make about 300 rows, and the report keeps at most
// MaxOffchainCallersPerDevice (8) offchain callers per device, about 800 in
// all, so 10,000 is over ten times what the report can use and about 1 MB.
// The read keeps the newest pairs and drops the rest. Failing the read
// instead would let a flood of callers freeze the report for as long as the
// flood stays in the caller window. A cut keeps every device that called in
// the last few minutes, since a device calls every 5 seconds, and keeps the
// flood's own callers, which the report shows as unlisted. What it drops is
// the oldest calls, so a device silent for longer than the cut reaches back
// reads as having made no call.
const MaxCallerRows = 10_000

// ClickHouseCallers reads controller_grpc_getconfig_success, which the
// controller writes for every successful GetConfig. It needs only SELECT.
type ClickHouseCallers struct {
	conn clickhouse.Conn
	db   string
}

// NewClickHouseCallers does not contact the server: the connection opens on
// the first query.
func NewClickHouseCallers(addr, db, user, pass string, disableTLS bool) (*ClickHouseCallers, error) {
	if !validDBName.MatchString(db) {
		return nil, fmt.Errorf("invalid clickhouse database name: %q", db)
	}
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "https://"), "http://")
	opts := &clickhouse.Options{
		Protocol: clickhouse.HTTP,
		Addr:     []string{addr},
		Auth:     clickhouse.Auth{Database: db, Username: user, Password: pass},
		// One query per pass.
		MaxOpenConns: 2,
		DialTimeout:  30 * time.Second,
		// Stay below ClickHouse Cloud's ~5m server-side idle close.
		ConnMaxLifetime: 3 * time.Minute,
	}
	if !disableTLS {
		opts.TLS = &tls.Config{}
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	return &ClickHouseCallers{conn: conn, db: db}, nil
}

func (c *ClickHouseCallers) Close() error { return c.conn.Close() }

// RecentCalls returns, per device pubkey and caller address, the newest call
// since the given time, newest first and at most MaxCallerRows of them. cut
// reports that there were more. Rows written before the controller recorded
// caller_ip carry an empty address and are left out. The query fails while
// the table has no caller_ip column at all.
func (c *ClickHouseCallers) RecentCalls(ctx context.Context, since time.Time) (calls []Call, cut bool, err error) {
	// One row past the limit tells a full result from one that was cut.
	rows, err := c.conn.Query(ctx, fmt.Sprintf(
		`SELECT device_pubkey, caller_ip, max(timestamp) AS last_seen
		 FROM "%s".controller_grpc_getconfig_success
		 WHERE timestamp >= ? AND caller_ip != ''
		 GROUP BY device_pubkey, caller_ip
		 ORDER BY last_seen DESC
		 LIMIT %d`, c.db, MaxCallerRows+1), since)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		if len(calls) == MaxCallerRows {
			return calls, true, nil
		}
		var (
			pubkey, callerIP string
			lastSeen         time.Time
		)
		if err := rows.Scan(&pubkey, &callerIP, &lastSeen); err != nil {
			return nil, false, fmt.Errorf("clickhouse scan: %w", err)
		}
		ip, err := netip.ParseAddr(callerIP)
		if err != nil {
			return nil, false, fmt.Errorf("device %s: caller_ip %q is not an address: %w", pubkey, callerIP, err)
		}
		calls = append(calls, Call{DevicePubkey: pubkey, CallerIP: ip.Unmap(), LastSeen: lastSeen})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("clickhouse rows: %w", err)
	}
	return calls, false, nil
}
