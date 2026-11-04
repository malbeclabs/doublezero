package worker

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
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

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
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

	client := &ClickHouseClient{conn: conn, lakeDB: "mainnet-beta"}
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	minutes, err := client.InterfaceCountersCoverage(context.Background(), "device123", start, end)
	require.NoError(t, err)
	assert.Equal(t, int64(0), minutes)
}

func TestLinkHealthRecent_ReturnsLatestBucket(t *testing.T) {
	bucketTs := time.Date(2026, 5, 6, 16, 10, 0, 0, time.UTC)
	start, end := bucketTs.Add(-15*time.Minute), bucketTs
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".link_rollup_5m`)
			assert.Contains(t, query, "bucket_ts")
			assert.Contains(t, query, "ORDER BY bucket_ts DESC, ingested_at DESC")
			assert.Contains(t, query, "LIMIT 1")
			// The bounds keep a link with no recent rows from scanning all history.
			assert.Contains(t, query, "bucket_ts >= ?")
			assert.Contains(t, query, "bucket_ts <= ?")
			// The provisioning filter must sit after the ingested_at dedup, or
			// a bucket whose newest row flips provisioning gets filtered out
			// and the dedup falls back to a stale version of it.
			assert.Greater(t,
				strings.Index(query, "provisioning = false"),
				strings.Index(query, "ORDER BY bucket_ts DESC, ingested_at DESC"),
				"provisioning must be filtered after the dedup, not in the inner WHERE")
			assert.Len(t, args, 3)
			assert.Equal(t, "linkABC", args[0])
			assert.Equal(t, start, args[1])
			assert.Equal(t, end, args[2])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*time.Time)) = bucketTs
					*(dest[1].(*bool)) = true
					*(dest[2].(*string)) = "activated"
					*(dest[3].(*float64)) = 12.5
					*(dest[4].(*float64)) = 0.0
					*(dest[5].(*uint32)) = 30
					*(dest[6].(*uint32)) = 28
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
	r, found, err := client.LinkHealthRecent(context.Background(), "linkABC", start, end)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, bucketTs, r.BucketTs)
	assert.True(t, r.IsisDown)
	assert.Equal(t, "activated", r.Status)
	assert.Equal(t, 12.5, r.ALossPct)
	assert.Equal(t, 0.0, r.ZLossPct)
	assert.Equal(t, uint32(30), r.ASamples)
	assert.Equal(t, uint32(28), r.ZSamples)
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

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
	_, found, err := client.LinkHealthRecent(context.Background(), "linkABC", time.Now().Add(-time.Hour), time.Now())
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

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
	_, _, err := client.LinkHealthRecent(context.Background(), "linkABC", time.Now().Add(-time.Hour), time.Now())
	assert.ErrorContains(t, err, "connection reset")
}

func TestLinkHealthRecent_QuotesDatabaseName(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) driver.Row {
			assert.Contains(t, query, `"mainnet-beta".link_rollup_5m`)
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*time.Time)) = time.Now()
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, lakeDB: "mainnet-beta"}
	_, found, err := client.LinkHealthRecent(context.Background(), "linkABC", time.Now().Add(-time.Hour), time.Now())
	require.NoError(t, err)
	assert.True(t, found)
}

func TestLinkHealthWindowAllClean_AllClean(t *testing.T) {
	maxBucketTs := time.Date(2026, 1, 1, 0, 55, 0, 0, time.UTC)
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, args ...any) driver.Row {
			assert.Contains(t, query, `"testdb".link_rollup_5m`)
			assert.Contains(t, query, "countIf")
			assert.Contains(t, query, "argMax")
			assert.Contains(t, query, "GROUP BY bucket_ts")
			assert.Contains(t, query, "maxIf(bucket_ts, a_samples > 0 AND z_samples > 0)")
			// Bad-bucket predicate: must match checkImpairment.
			assert.Contains(t, query, "isis_down = true AND status != 'hard-drained'")
			assert.Contains(t, query, "a_samples > 0 AND z_samples > 0 AND a_loss_pct > ? AND z_loss_pct > ?")
			assert.Contains(t, query, "countIf(a_samples > 0 AND z_samples > 0) AS total_buckets")
			assert.Contains(t, query, "argMax(status, ingested_at)")
			// provisioning is deduped alongside the health columns and filtered
			// afterwards, so a flip on the newest row can't resurrect a stale one.
			assert.Contains(t, query, "argMax(provisioning, ingested_at)")
			assert.Greater(t,
				strings.Index(query, "provisioning = false"),
				strings.Index(query, "GROUP BY bucket_ts"),
				"provisioning must be filtered after the GROUP BY dedup")
			// Args order: lossThreshold, lossThreshold, linkPubkey, start, end
			assert.Len(t, args, 5)
			assert.Equal(t, 5.0, args[0])
			assert.Equal(t, 5.0, args[1])
			assert.Equal(t, "linkABC", args[2])
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*uint64)) = 0
					*(dest[1].(*uint64)) = 6
					*(dest[2].(*time.Time)) = maxBucketTs
					return nil
				},
			}
		},
	}

	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
	r, found, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		5.0)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, r.AllClean)
	assert.Equal(t, uint64(0), r.Bad)
	assert.Equal(t, uint64(6), r.Total)
	assert.Equal(t, maxBucketTs, r.MaxBucketTs)
}

func TestLinkHealthWindowAllClean_HasBadBuckets(t *testing.T) {
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, _ string, _ ...any) driver.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					*(dest[0].(*uint64)) = 2
					*(dest[1].(*uint64)) = 6
					*(dest[2].(*time.Time)) = time.Now()
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
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
					*(dest[2].(*time.Time)) = time.Time{}
					return nil
				},
			}
		},
	}
	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
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
	client := &ClickHouseClient{conn: conn, lakeDB: "testdb"}
	_, _, err := client.LinkHealthWindowAllClean(context.Background(), "linkABC",
		time.Now().Add(-1*time.Hour), time.Now(), 5.0)
	assert.ErrorContains(t, err, "connection reset")
}

// Links and interface counters read lake tables, controller calls the telemetry
// database, so each must qualify its table with the right one.
func TestClickHouseClient_RoutesTablesToTheirDatabase(t *testing.T) {
	var queries []string
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) driver.Row {
			queries = append(queries, query)
			return &mockRow{scanFunc: func(_ ...any) error { return errors.New("stop") }}
		},
	}
	client := &ClickHouseClient{conn: conn, db: "default", lakeDB: "lake"}
	ctx := context.Background()
	now := time.Now()

	_, _ = client.ControllerCallCoverage(ctx, "d", now, now)
	_, _ = client.InterfaceCountersCoverage(ctx, "d", now, now)
	_, _, _ = client.LinkHealthRecent(ctx, "l", now, now)
	_, _, _ = client.LinkHealthWindowAllClean(ctx, "l", now, now, 5.0)

	require.Len(t, queries, 4)
	assert.Contains(t, queries[0], `"default".controller_grpc_getconfig_success`)
	assert.Contains(t, queries[1], `"lake".fact_dz_device_interface_counters`)
	assert.Contains(t, queries[2], `"lake".link_rollup_5m`)
	assert.Contains(t, queries[3], `"lake".link_rollup_5m`)
}

// Building the client must not need a reachable server: a startup blip would
// otherwise drop every ClickHouse criterion for the life of the process.
func TestNewClickHouseClient_UnreachableServer_StillBuilds(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"plain host:port", "127.0.0.1:1"},
		{"https prefix", "https://127.0.0.1:1"},
		{"http prefix", "http://127.0.0.1:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClickHouseClient(tt.addr, "default", "lake", "default", "", true)
			require.NoError(t, err)
			defer client.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			assert.Error(t, client.Ping(ctx))
		})
	}
}

func TestNewClickHouseClient_InvalidDatabaseName(t *testing.T) {
	_, err := NewClickHouseClient("127.0.0.1:1", "bad;db", "lake", "default", "", true)
	assert.ErrorContains(t, err, "invalid clickhouse database name")
	_, err = NewClickHouseClient("127.0.0.1:1", "default", "bad;db", "default", "", true)
	assert.ErrorContains(t, err, "invalid clickhouse database name")
}

func TestOptions_ConnMaxLifetime(t *testing.T) {
	opts := options("localhost:8123", "default", "default", "", true)
	assert.Equal(t, 3*time.Minute, opts.ConnMaxLifetime)
}
