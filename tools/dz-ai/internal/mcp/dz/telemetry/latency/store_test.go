package dztelemlatency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"log/slog"
	"os"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
	"github.com/stretchr/testify/require"
)

type failingDB struct{}

func (f *failingDB) Close() error {
	return nil
}

func (f *failingDB) Catalog() string {
	return "main"
}

func (f *failingDB) Schema() string {
	return "default"
}

func (f *failingDB) Conn(ctx context.Context) (duck.Connection, error) {
	return &failingDBConn{db: f}, nil
}

type failingDBConn struct {
	db *failingDB
}

func (f *failingDBConn) DB() duck.DB {
	if f.db == nil {
		return &failingDB{}
	}
	return f.db
}

func (f *failingDBConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return &sql.Row{}
}

func (f *failingDBConn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("database error")
}

func (f *failingDBConn) Close() error {
	return nil
}
func (f *failingDB) ReplaceTable(tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	return errors.New("database error")
}

// testPK creates a deterministic public key string from an integer identifier
func testPK(n int) string {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(n + i)
	}
	return solana.PublicKeyFromBytes(bytes).String()
}

func TestAI_MCP_Telemetry_Store_NewStore(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				DB: &failingDB{},
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing db", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "db is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

func TestAI_MCP_Telemetry_Store_CreateTablesIfNotExists(t *testing.T) {
	t.Parallel()

	t.Run("creates all tables", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Verify tables exist by querying them
		tables := []string{"dz_device_link_circuits", "dz_device_link_latency_samples", "dz_internet_metro_latency_samples"}
		for _, table := range tables {
			var count int
			ctx := context.Background()
			conn, err := db.Conn(ctx)
			require.NoError(t, err)
			defer conn.Close()
			err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
			require.NoError(t, err, "table %s should exist", table)
		}
	})

	t.Run("returns error when database fails", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     &failingDB{},
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create table")
	})
}

func TestAI_MCP_Telemetry_Store_ReplaceDeviceLinkCircuits(t *testing.T) {
	t.Parallel()

	t.Run("saves circuits to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		circuits := []DeviceLinkCircuit{
			{
				Code:            "DEV1 → DEV2 (abc1234)",
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				LinkCode:        "LINK1",
				LinkType:        "WAN",
				ContributorCode: "CONTRIB1",
				CommittedRTT:    1000.5,
				CommittedJitter: 50.2,
			},
		}

		err = store.ReplaceDeviceLinkCircuits(context.Background(), circuits)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_circuits").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var code, originDevicePK, targetDevicePK, linkPK, linkCode, linkType, contributorCode string
		var committedRTT, committedJitter float64
		err = conn.QueryRowContext(ctx, "SELECT code, origin_device_pk, target_device_pk, link_pk, link_code, link_type, contributor_code, committed_rtt, committed_jitter FROM dz_device_link_circuits LIMIT 1").Scan(&code, &originDevicePK, &targetDevicePK, &linkPK, &linkCode, &linkType, &contributorCode, &committedRTT, &committedJitter)
		require.NoError(t, err)
		require.Equal(t, "DEV1 → DEV2 (abc1234)", code)
		require.Equal(t, testPK(1), originDevicePK)
		require.Equal(t, testPK(2), targetDevicePK)
		require.Equal(t, testPK(3), linkPK)
		require.Equal(t, "LINK1", linkCode)
		require.Equal(t, "WAN", linkType)
		require.Equal(t, "CONTRIB1", contributorCode)
		require.InDelta(t, 1000.5, committedRTT, 0.01)
		require.InDelta(t, 50.2, committedJitter, 0.01)
	})

	t.Run("replaces existing circuits", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		circuits1 := []DeviceLinkCircuit{
			{
				Code:            "DEV1 → DEV2 (abc1234)",
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				LinkCode:        "LINK1",
				LinkType:        "WAN",
				ContributorCode: "CONTRIB1",
				CommittedRTT:    1000.5,
				CommittedJitter: 50.2,
			},
		}

		err = store.ReplaceDeviceLinkCircuits(context.Background(), circuits1)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_circuits").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		circuits2 := []DeviceLinkCircuit{
			{
				Code:            "DEV3 → DEV4 (def5678)",
				OriginDevicePK:  testPK(4),
				TargetDevicePK:  testPK(5),
				LinkPK:          testPK(6),
				LinkCode:        "LINK2",
				LinkType:        "LAN",
				ContributorCode: "CONTRIB2",
				CommittedRTT:    2000.0,
				CommittedJitter: 100.0,
			},
		}

		err = store.ReplaceDeviceLinkCircuits(context.Background(), circuits2)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_circuits").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var code string
		err = conn.QueryRowContext(ctx, "SELECT code FROM dz_device_link_circuits LIMIT 1").Scan(&code)
		require.NoError(t, err)
		require.Equal(t, "DEV3 → DEV4 (def5678)", code)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// First insert some data
		circuits := []DeviceLinkCircuit{
			{
				Code:            "DEV1 → DEV2 (abc1234)",
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				LinkCode:        "LINK1",
				LinkType:        "WAN",
				ContributorCode: "CONTRIB1",
				CommittedRTT:    1000.5,
				CommittedJitter: 50.2,
			},
		}
		err = store.ReplaceDeviceLinkCircuits(context.Background(), circuits)
		require.NoError(t, err)

		// Then replace with empty slice
		err = store.ReplaceDeviceLinkCircuits(context.Background(), []DeviceLinkCircuit{})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_circuits").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestAI_MCP_Telemetry_Store_AppendDeviceLinkLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("appends samples to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		samples := []DeviceLinkLatencySample{
			{
				CircuitCode:           "CIRCUIT1",
				Epoch:                 100,
				SampleIndex:           0,
				TimestampMicroseconds: 1000000,
				RTTMicroseconds:       5000,
			},
			{
				CircuitCode:           "CIRCUIT1",
				Epoch:                 100,
				SampleIndex:           1,
				TimestampMicroseconds: 2000000,
				RTTMicroseconds:       6000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Append more samples
		moreSamples := []DeviceLinkLatencySample{
			{
				CircuitCode:           "CIRCUIT1",
				Epoch:                 100,
				SampleIndex:           2,
				TimestampMicroseconds: 3000000,
				RTTMicroseconds:       7000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify the data
		var circuitCode string
		var epoch uint64
		var sampleIndex int
		var timestampUs, rttUs uint64
		err = conn.QueryRowContext(ctx, "SELECT circuit_code, epoch, sample_index, timestamp_us, rtt_us FROM dz_device_link_latency_samples WHERE sample_index = 2").Scan(&circuitCode, &epoch, &sampleIndex, &timestampUs, &rttUs)
		require.NoError(t, err)
		require.Equal(t, "CIRCUIT1", circuitCode)
		require.Equal(t, uint64(100), epoch)
		require.Equal(t, 2, sampleIndex)
		require.Equal(t, uint64(3000000), timestampUs)
		require.Equal(t, uint64(7000), rttUs)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		err = store.AppendDeviceLinkLatencySamples(context.Background(), []DeviceLinkLatencySample{})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestAI_MCP_Telemetry_Store_AppendInternetMetroLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("appends samples to database", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		samples := []InternetMetroLatencySample{
			{
				CircuitCode:           "NYC → LAX",
				DataProvider:          "provider1",
				Epoch:                 100,
				SampleIndex:           0,
				TimestampMicroseconds: 1000000,
				RTTMicroseconds:       10000,
			},
			{
				CircuitCode:           "NYC → LAX",
				DataProvider:          "provider1",
				Epoch:                 100,
				SampleIndex:           1,
				TimestampMicroseconds: 2000000,
				RTTMicroseconds:       11000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Append more samples
		moreSamples := []InternetMetroLatencySample{
			{
				CircuitCode:           "NYC → LAX",
				DataProvider:          "provider1",
				Epoch:                 100,
				SampleIndex:           2,
				TimestampMicroseconds: 3000000,
				RTTMicroseconds:       12000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify the data
		var circuitCode, dataProvider string
		var epoch uint64
		var sampleIndex int
		var timestampUs, rttUs uint64
		err = conn.QueryRowContext(ctx, "SELECT circuit_code, data_provider, epoch, sample_index, timestamp_us, rtt_us FROM dz_internet_metro_latency_samples WHERE sample_index = 2").Scan(&circuitCode, &dataProvider, &epoch, &sampleIndex, &timestampUs, &rttUs)
		require.NoError(t, err)
		require.Equal(t, "NYC → LAX", circuitCode)
		require.Equal(t, "provider1", dataProvider)
		require.Equal(t, uint64(100), epoch)
		require.Equal(t, 2, sampleIndex)
		require.Equal(t, uint64(3000000), timestampUs)
		require.Equal(t, uint64(12000), rttUs)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		err = store.AppendInternetMetroLatencySamples(context.Background(), []InternetMetroLatencySample{})
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestAI_MCP_Telemetry_Store_GetExistingMaxSampleIndices(t *testing.T) {
	t.Parallel()

	t.Run("returns max sample indices for each circuit and epoch", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Insert samples for different circuits and epochs
		samples := []DeviceLinkLatencySample{
			{CircuitCode: "CIRCUIT1", Epoch: 100, SampleIndex: 0, TimestampMicroseconds: 1000000, RTTMicroseconds: 5000},
			{CircuitCode: "CIRCUIT1", Epoch: 100, SampleIndex: 1, TimestampMicroseconds: 2000000, RTTMicroseconds: 6000},
			{CircuitCode: "CIRCUIT1", Epoch: 100, SampleIndex: 2, TimestampMicroseconds: 3000000, RTTMicroseconds: 7000},
			{CircuitCode: "CIRCUIT1", Epoch: 101, SampleIndex: 0, TimestampMicroseconds: 4000000, RTTMicroseconds: 8000},
			{CircuitCode: "CIRCUIT2", Epoch: 100, SampleIndex: 0, TimestampMicroseconds: 5000000, RTTMicroseconds: 9000},
			{CircuitCode: "CIRCUIT2", Epoch: 100, SampleIndex: 1, TimestampMicroseconds: 6000000, RTTMicroseconds: 10000},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		indices, err := store.GetExistingMaxSampleIndices()
		require.NoError(t, err)
		require.Len(t, indices, 3)

		require.Equal(t, 2, indices["CIRCUIT1:100"])
		require.Equal(t, 0, indices["CIRCUIT1:101"])
		require.Equal(t, 1, indices["CIRCUIT2:100"])
	})

	t.Run("returns empty map when no samples exist", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		indices, err := store.GetExistingMaxSampleIndices()
		require.NoError(t, err)
		require.Empty(t, indices)
	})
}

func TestAI_MCP_Telemetry_Store_GetExistingInternetMaxSampleIndices(t *testing.T) {
	t.Parallel()

	t.Run("returns max sample indices for each circuit, data provider, and epoch", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		// Insert samples for different circuits, data providers, and epochs
		samples := []InternetMetroLatencySample{
			{CircuitCode: "NYC → LAX", DataProvider: "provider1", Epoch: 100, SampleIndex: 0, TimestampMicroseconds: 1000000, RTTMicroseconds: 10000},
			{CircuitCode: "NYC → LAX", DataProvider: "provider1", Epoch: 100, SampleIndex: 1, TimestampMicroseconds: 2000000, RTTMicroseconds: 11000},
			{CircuitCode: "NYC → LAX", DataProvider: "provider1", Epoch: 100, SampleIndex: 2, TimestampMicroseconds: 3000000, RTTMicroseconds: 12000},
			{CircuitCode: "NYC → LAX", DataProvider: "provider1", Epoch: 101, SampleIndex: 0, TimestampMicroseconds: 4000000, RTTMicroseconds: 13000},
			{CircuitCode: "NYC → LAX", DataProvider: "provider2", Epoch: 100, SampleIndex: 0, TimestampMicroseconds: 5000000, RTTMicroseconds: 14000},
			{CircuitCode: "NYC → LAX", DataProvider: "provider2", Epoch: 100, SampleIndex: 1, TimestampMicroseconds: 6000000, RTTMicroseconds: 15000},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		indices, err := store.GetExistingInternetMaxSampleIndices()
		require.NoError(t, err)
		require.Len(t, indices, 3)

		require.Equal(t, 2, indices["NYC → LAX:provider1:100"])
		require.Equal(t, 0, indices["NYC → LAX:provider1:101"])
		require.Equal(t, 1, indices["NYC → LAX:provider2:100"])
	})

	t.Run("returns empty map when no samples exist", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		store, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		err = store.CreateTablesIfNotExists()
		require.NoError(t, err)

		indices, err := store.GetExistingInternetMaxSampleIndices()
		require.NoError(t, err)
		require.Empty(t, indices)
	})
}
