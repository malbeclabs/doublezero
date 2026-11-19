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

// fakeCHConn answers the caller read with n rows, each a second older than
// the one before, as the query's ORDER BY returns them, and records the query.
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
	*dest[2].(*time.Time) = t0.Add(-time.Duration(r.i) * time.Second)
	return nil
}
func (r *fakeCHRows) Err() error   { return nil }
func (r *fakeCHRows) Close() error { return nil }

func TestRecentCalls_KeepsTheNewestAtTheRowLimit(t *testing.T) {
	conn := &fakeCHConn{n: MaxCallerRows}
	c := &ClickHouseCallers{conn: conn, db: "testdb"}
	calls, cut, err := c.RecentCalls(context.Background(), t0)
	require.NoError(t, err)
	assert.False(t, cut, "a result at the limit is whole")
	assert.Len(t, calls, MaxCallerRows)
	// The newest pairs are the ones kept, so the order must come before the limit.
	q := strings.Join(strings.Fields(conn.query), " ")
	assert.True(t, strings.HasSuffix(q, fmt.Sprintf("ORDER BY last_seen DESC LIMIT %d", MaxCallerRows+1)), q)

	conn.n = MaxCallerRows + 1
	calls, cut, err = c.RecentCalls(context.Background(), t0)
	require.NoError(t, err, "a cut read still builds the report")
	assert.True(t, cut)
	require.Len(t, calls, MaxCallerRows)
	assert.Equal(t, t0.Add(-time.Second), calls[0].LastSeen)
	assert.Equal(t, t0.Add(-MaxCallerRows*time.Second), calls[len(calls)-1].LastSeen, "the oldest row is the one dropped")

	// A server that ignored the LIMIT is still cut at it.
	conn.n = 3 * MaxCallerRows
	calls, cut, err = c.RecentCalls(context.Background(), t0)
	require.NoError(t, err)
	assert.True(t, cut)
	assert.Len(t, calls, MaxCallerRows)
}
