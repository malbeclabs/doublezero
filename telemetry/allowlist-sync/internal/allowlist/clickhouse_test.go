package allowlist

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCHConn answers the caller read with n rows and records the query.
type fakeCHConn struct {
	driver.Conn
	n     int
	query string
}

func (c *fakeCHConn) Query(_ context.Context, query string, _ ...any) (driver.Rows, error) {
	c.query = query
	return &fakeCHRows{n: c.n}, nil
}

type fakeCHRows struct {
	driver.Rows
	n, i int
}

func (r *fakeCHRows) Next() bool { r.i++; return r.i <= r.n }
func (r *fakeCHRows) Scan(dest ...any) error {
	*dest[0].(*string) = pk(byte(r.i % 200))
	*dest[1].(*string) = fmt.Sprintf("198.51.%d.%d", r.i/256%256, r.i%256)
	*dest[2].(*time.Time) = t0
	return nil
}
func (r *fakeCHRows) Err() error   { return nil }
func (r *fakeCHRows) Close() error { return nil }

func TestRecentCalls_FailsAtTheRowLimit(t *testing.T) {
	conn := &fakeCHConn{n: MaxCallerRows}
	c := &ClickHouseCallers{conn: conn, db: "testdb"}
	calls, err := c.RecentCalls(context.Background(), t0)
	require.NoError(t, err)
	assert.Len(t, calls, MaxCallerRows, "a result at the limit is whole")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(conn.query), fmt.Sprintf("LIMIT %d", MaxCallerRows+1)), conn.query)

	conn.n = MaxCallerRows + 1
	calls, err = c.RecentCalls(context.Background(), t0)
	assert.EqualError(t, err, fmt.Sprintf("caller read reached its limit of %d device and address pairs; the report is not built, and a flood of callers is the likely cause", MaxCallerRows))
	assert.Nil(t, calls)

	// A server that ignored the LIMIT is still cut at it.
	conn.n = 3 * MaxCallerRows
	_, err = c.RecentCalls(context.Background(), t0)
	assert.Error(t, err)
}
