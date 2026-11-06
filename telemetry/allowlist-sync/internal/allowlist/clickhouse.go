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
// since the given time. Rows written before the controller recorded caller_ip
// carry an empty address and are left out. The query fails while the table
// has no caller_ip column at all.
func (c *ClickHouseCallers) RecentCalls(ctx context.Context, since time.Time) ([]Call, error) {
	rows, err := c.conn.Query(ctx, fmt.Sprintf(
		`SELECT device_pubkey, caller_ip, max(timestamp) AS last_seen
		 FROM "%s".controller_grpc_getconfig_success
		 WHERE timestamp >= ? AND caller_ip != ''
		 GROUP BY device_pubkey, caller_ip`, c.db), since)
	if err != nil {
		return nil, fmt.Errorf("clickhouse query: %w", err)
	}
	defer rows.Close()

	var calls []Call
	for rows.Next() {
		var (
			pubkey, callerIP string
			lastSeen         time.Time
		)
		if err := rows.Scan(&pubkey, &callerIP, &lastSeen); err != nil {
			return nil, fmt.Errorf("clickhouse scan: %w", err)
		}
		ip, err := netip.ParseAddr(callerIP)
		if err != nil {
			return nil, fmt.Errorf("device %s: caller_ip %q is not an address: %w", pubkey, callerIP, err)
		}
		calls = append(calls, Call{DevicePubkey: pubkey, CallerIP: ip.Unmap(), LastSeen: lastSeen})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse rows: %w", err)
	}
	return calls, nil
}
