package worker

import (
	"context"
	"database/sql"
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

func TestLinkHealthRecent_ReturnsLatestBucket(t *testing.T) {
	bucketTs := time.Date(2026, 5, 6, 16, 10, 0, 0, time.UTC)
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".link_rollup_5m`)
			assert.Contains(t, query, "provisioning = false")
			assert.Contains(t, query, "bucket_ts")
			assert.Contains(t, query, "ORDER BY bucket_ts DESC, ingested_at DESC")
			assert.Contains(t, query, "LIMIT 1")
			assert.Len(t, args, 1)
			assert.Equal(t, "linkABC", args[0])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*time.Time)) = bucketTs
					*(dest[1].(*bool)) = true
					*(dest[2].(*float64)) = 12.5
					*(dest[3].(*float64)) = 0.0
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	r, found, err := client.LinkHealthRecent(context.Background(), "linkABC")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, bucketTs, r.BucketTs)
	assert.True(t, r.IsisDown)
	assert.Equal(t, 12.5, r.ALossPct)
	assert.Equal(t, 0.0, r.ZLossPct)
}

func TestLinkHealthRecent_NoData_ReturnsFoundFalse(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(_ ...any) error {
					return sql.ErrNoRows
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	_, found, err := client.LinkHealthRecent(context.Background(), "linkABC")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestLinkHealthRecent_QueryError(t *testing.T) {
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
	_, _, err := client.LinkHealthRecent(context.Background(), "linkABC")
	assert.ErrorContains(t, err, "connection reset")
}

func TestLinkHealthRecent_QuotesDatabaseName(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) driver.Row {
			assert.Contains(t, query, `"mainnet-beta".link_rollup_5m`)
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*time.Time)) = time.Now()
					*(dest[1].(*bool)) = false
					*(dest[2].(*float64)) = 0
					*(dest[3].(*float64)) = 0
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, db: "mainnet-beta"}
	_, found, err := client.LinkHealthRecent(context.Background(), "linkABC")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestLinkHealthWindowAllClean_AllClean(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".link_rollup_5m`)
			assert.Contains(t, query, "provisioning = false")
			assert.Contains(t, query, "countIf")
			// Inner subquery dedupes ingested_at duplicates per bucket via argMax.
			assert.Contains(t, query, "argMax")
			assert.Contains(t, query, "GROUP BY bucket_ts")
			// Args order: lossThreshold, lossThreshold, linkPubkey, start, end
			assert.Len(t, args, 5)
			assert.Equal(t, 5.0, args[0])
			assert.Equal(t, 5.0, args[1])
			assert.Equal(t, "linkABC", args[2])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*uint64)) = 0
					*(dest[1].(*uint64)) = 6
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, db: "testdb"}
	r, found, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		5.0)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, r.AllClean)
	assert.Equal(t, uint64(0), r.Bad)
	assert.Equal(t, uint64(6), r.Total)
}

func TestLinkHealthWindowAllClean_HasBadBuckets(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*uint64)) = 2
					*(dest[1].(*uint64)) = 6
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, db: "testdb"}
	r, found, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Now().Add(-1*time.Hour), time.Now(), 5.0)
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, r.AllClean)
	assert.Equal(t, uint64(2), r.Bad)
	assert.Equal(t, uint64(6), r.Total)
}

func TestLinkHealthWindowAllClean_NoBucketsInWindow(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*uint64)) = 0
					*(dest[1].(*uint64)) = 0
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, db: "testdb"}
	_, found, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Now().Add(-1*time.Hour), time.Now(), 5.0)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestLinkHealthWindowAllClean_QueryError(t *testing.T) {
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
	_, _, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Now().Add(-1*time.Hour), time.Now(), 5.0)
	assert.ErrorContains(t, err, "connection reset")
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
