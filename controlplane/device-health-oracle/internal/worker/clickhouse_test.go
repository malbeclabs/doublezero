package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRow implements driver.Row for testing.
type mockRow struct {
	scanFunc func(dest ...any) error
}

func (r *mockRow) Err() error             { return nil }
func (r *mockRow) Scan(dest ...any) error { return r.scanFunc(dest...) }
func (r *mockRow) ScanStruct(_ any) error { return nil }

// mockConn implements the subset of driver.Conn used by ClickHouseClient.
type mockConn struct {
	driver.Conn
	queryRowFunc func(ctx context.Context, query string, args ...any) driver.Row
}

func (c *mockConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return c.queryRowFunc(ctx, query, args...)
}

func TestControllerCallCoverage_ReturnsCount(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".controller_grpc_getconfig_success`)
			assert.Len(t, args, 3)
			assert.Equal(t, "device123", args[0])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					p := dest[0].(*uint64)
					*p = 42
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Hour)

	minutes, err := client.ControllerCallCoverage(context.Background(), "device123", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(42), minutes)
}

func TestControllerCallCoverage_QueryError(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(_ ...any) error {
					return errors.New("connection reset")
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	_, err := client.ControllerCallCoverage(context.Background(), "device123", start, end)
	assert.ErrorContains(t, err, "connection reset")
}

func TestControllerCallCoverage_QuotesDatabaseName(t *testing.T) {
	// Verify that database names with hyphens (mainnet-beta) are quoted.
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) driver.Row {
			assert.Contains(t, query, `"mainnet-beta".controller_grpc_getconfig_success`)
			return &mockRow{
				scanFunc: func(dest ...any) error {
					p := dest[0].(*uint64)
					*p = 0
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "mainnet-beta"}
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	minutes, err := client.ControllerCallCoverage(context.Background(), "device123", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(0), minutes)
}

func TestInterfaceCountersCoverage_ReturnsCount(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".fact_dz_device_interface_counters`)
			assert.Contains(t, query, "event_ts")
			assert.Contains(t, query, "device_pk")
			assert.Len(t, args, 3)
			assert.Equal(t, "device123", args[0])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					p := dest[0].(*uint64)
					*p = 55
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Hour)

	minutes, err := client.InterfaceCountersCoverage(context.Background(), "device123", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(55), minutes)
}

func TestInterfaceCountersCoverage_QueryError(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(_ ...any) error {
					return errors.New("connection reset")
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	_, err := client.InterfaceCountersCoverage(context.Background(), "device123", start, end)
	assert.ErrorContains(t, err, "connection reset")
}

func TestInterfaceCountersCoverage_QuotesDatabaseName(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) driver.Row {
			assert.Contains(t, query, `"mainnet-beta".fact_dz_device_interface_counters`)
			return &mockRow{
				scanFunc: func(dest ...any) error {
					p := dest[0].(*uint64)
					*p = 0
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "mainnet-beta"}
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	minutes, err := client.InterfaceCountersCoverage(context.Background(), "device123", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(0), minutes)
}

func TestNewClickHouseClient_StripsScheme(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"plain host:port", "localhost:8123"},
		{"https prefix", "https://clickhouse.example.com:8443"},
		{"http prefix", "http://localhost:8123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClickHouseClient(tt.addr, "default", "default", "", true)
			// Connection will fail (no server) — verify no panic and an error is returned.
			assert.Error(t, err)
		})
	}
}
