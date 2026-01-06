package dztelemlatency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
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

func TestLake_TelemetryLatency_Store_NewStore(t *testing.T) {
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

func TestLake_TelemetryLatency_Store_CreateTablesIfNotExists(t *testing.T) {
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
		tables := []string{"dz_device_link_latency_samples_raw", "dz_internet_metro_latency_samples_raw"}
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
		require.Contains(t, err.Error(), "failed to create")
	})
}

func TestLake_TelemetryLatency_Store_AppendDeviceLinkLatencySamples(t *testing.T) {
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
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 6000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Append more samples
		moreSamples := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  testPK(1),
				TargetDevicePK:  testPK(2),
				LinkPK:          testPK(3),
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 7000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify the data
		var originDevicePK, targetDevicePK, linkPK string
		var epoch uint64
		var sampleIndex int
		var sampleTime time.Time
		var rttUs uint64
		err = conn.QueryRowContext(ctx, "SELECT origin_device_pk, target_device_pk, link_pk, epoch, sample_index, time, rtt_us FROM dz_device_link_latency_samples_raw WHERE sample_index = 2").Scan(&originDevicePK, &targetDevicePK, &linkPK, &epoch, &sampleIndex, &sampleTime, &rttUs)
		require.NoError(t, err)
		require.Equal(t, testPK(1), originDevicePK)
		require.Equal(t, testPK(2), targetDevicePK)
		require.Equal(t, testPK(3), linkPK)
		require.Equal(t, uint64(100), epoch)
		require.Equal(t, 2, sampleIndex)
		require.WithinDuration(t, time.Unix(3, 0), sampleTime, time.Second)
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
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_device_link_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestLake_TelemetryLatency_Store_AppendInternetMetroLatencySamples(t *testing.T) {
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
				OriginMetroPK:   testPK(1),
				TargetMetroPK:   testPK(2),
				DataProvider:    "provider1",
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 10000,
			},
			{
				OriginMetroPK:   testPK(1),
				TargetMetroPK:   testPK(2),
				DataProvider:    "provider1",
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 11000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var count int
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Append more samples
		moreSamples := []InternetMetroLatencySample{
			{
				OriginMetroPK:   testPK(1),
				TargetMetroPK:   testPK(2),
				DataProvider:    "provider1",
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 12000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify the data
		var originMetroPK, targetMetroPK, dataProvider string
		var epoch uint64
		var sampleIndex int
		var sampleTime time.Time
		var rttUs uint64
		err = conn.QueryRowContext(ctx, "SELECT origin_metro_pk, target_metro_pk, data_provider, epoch, sample_index, time, rtt_us FROM dz_internet_metro_latency_samples_raw WHERE sample_index = 2").Scan(&originMetroPK, &targetMetroPK, &dataProvider, &epoch, &sampleIndex, &sampleTime, &rttUs)
		require.NoError(t, err)
		require.Equal(t, testPK(1), originMetroPK)
		require.Equal(t, testPK(2), targetMetroPK)
		require.Equal(t, "provider1", dataProvider)
		require.Equal(t, uint64(100), epoch)
		require.Equal(t, 2, sampleIndex)
		require.WithinDuration(t, time.Unix(3, 0), sampleTime, time.Second)
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
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM dz_internet_metro_latency_samples_raw").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestLake_TelemetryLatency_Store_GetExistingMaxSampleIndices(t *testing.T) {
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

		// Insert samples for different device/link pairs and epochs
		samples := []DeviceLinkLatencySample{
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 0, Time: time.Unix(1, 0), RTTMicroseconds: 5000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 1, Time: time.Unix(2, 0), RTTMicroseconds: 6000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 2, Time: time.Unix(3, 0), RTTMicroseconds: 7000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 101, SampleIndex: 0, Time: time.Unix(4, 0), RTTMicroseconds: 8000},
			{OriginDevicePK: testPK(4), TargetDevicePK: testPK(5), LinkPK: testPK(6), Epoch: 100, SampleIndex: 0, Time: time.Unix(5, 0), RTTMicroseconds: 9000},
			{OriginDevicePK: testPK(4), TargetDevicePK: testPK(5), LinkPK: testPK(6), Epoch: 100, SampleIndex: 1, Time: time.Unix(6, 0), RTTMicroseconds: 10000},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		indices, err := store.GetExistingMaxSampleIndices()
		require.NoError(t, err)
		require.Len(t, indices, 3)

		require.Equal(t, 2, indices[fmt.Sprintf("%s:%s:%s:100", testPK(1), testPK(2), testPK(3))])
		require.Equal(t, 0, indices[fmt.Sprintf("%s:%s:%s:101", testPK(1), testPK(2), testPK(3))])
		require.Equal(t, 1, indices[fmt.Sprintf("%s:%s:%s:100", testPK(4), testPK(5), testPK(6))])
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

func TestLake_TelemetryLatency_Store_GetExistingInternetMaxSampleIndices(t *testing.T) {
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

		// Insert samples for different metro pairs, data providers, and epochs
		samples := []InternetMetroLatencySample{
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 0, Time: time.Unix(1, 0), RTTMicroseconds: 10000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 1, Time: time.Unix(2, 0), RTTMicroseconds: 11000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 2, Time: time.Unix(3, 0), RTTMicroseconds: 12000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 101, SampleIndex: 0, Time: time.Unix(4, 0), RTTMicroseconds: 13000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider2", Epoch: 100, SampleIndex: 0, Time: time.Unix(5, 0), RTTMicroseconds: 14000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider2", Epoch: 100, SampleIndex: 1, Time: time.Unix(6, 0), RTTMicroseconds: 15000},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		indices, err := store.GetExistingInternetMaxSampleIndices()
		require.NoError(t, err)
		require.Len(t, indices, 3)

		require.Equal(t, 2, indices[fmt.Sprintf("%s:%s:provider1:100", testPK(1), testPK(2))])
		require.Equal(t, 0, indices[fmt.Sprintf("%s:%s:provider1:101", testPK(1), testPK(2))])
		require.Equal(t, 1, indices[fmt.Sprintf("%s:%s:provider2:100", testPK(1), testPK(2))])
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
