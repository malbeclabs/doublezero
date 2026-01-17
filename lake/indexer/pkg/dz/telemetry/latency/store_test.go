package dztelemlatency

import (
	"context"
	"fmt"
	"testing"
	"time"

	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"
)

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
				ClickHouse: nil,
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing clickhouse", func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(StoreConfig{
				Logger: laketesting.NewLogger(),
			})
			require.Error(t, err)
			require.Nil(t, store)
			require.Contains(t, err.Error(), "clickhouse connection is required")
		})
	})

	t.Run("returns store when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)
		require.NotNil(t, store)
	})
}

// CreateTablesIfNotExists was removed - tables are created via migrations
// Tests for this method are obsolete

func TestLake_TelemetryLatency_Store_AppendDeviceLinkLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("appends samples to database", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(1)
		targetPK := testPK(2)
		linkPK := testPK(3)

		samples := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 6000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Verify first batch was inserted
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have inserted 2 samples in first batch")

		// Append more samples
		moreSamples := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 7000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		rows, err = conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(3), count, "should have inserted 3 latency samples")
		conn.Close()
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		err = store.AppendDeviceLinkLatencySamples(context.Background(), []DeviceLinkLatencySample{})
		require.NoError(t, err)

		// Verify no data was inserted
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(), "SELECT count() FROM fact_dz_device_link_latency")
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(0), count, "should have inserted no samples for empty slice")
		conn.Close()
	})

	t.Run("calculates IPDV from previous RTTs in database", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(10)
		targetPK := testPK(11)
		linkPK := testPK(12)

		// First batch - no previous RTTs, so IPDV should be NULL
		firstBatch := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 5000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), firstBatch)
		require.NoError(t, err)

		// Verify first sample has NULL IPDV
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 0",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt1 int64
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&rtt1, &ipdv1))
		rows.Close()
		require.Equal(t, int64(5000), rtt1)
		require.Nil(t, ipdv1, "first sample should have NULL IPDV")

		// Second batch - should calculate IPDV from previous RTT (5000 -> 6000 = 1000)
		secondBatch := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 6000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), secondBatch)
		require.NoError(t, err)

		// Verify second sample has correct IPDV
		rows, err = conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 1",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt2 int64
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&rtt2, &ipdv2))
		rows.Close()
		require.Equal(t, int64(6000), rtt2)
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(1000), *ipdv2, "IPDV should be 6000 - 5000 = 1000")

		// Third batch - RTT decreased, IPDV should be absolute difference (6000 -> 5500 = 500)
		thirdBatch := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 5500,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), thirdBatch)
		require.NoError(t, err)

		// Verify third sample has correct IPDV
		rows, err = conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 2",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt3 int64
		var ipdv3 *int64
		require.NoError(t, rows.Scan(&rtt3, &ipdv3))
		rows.Close()
		require.Equal(t, int64(5500), rtt3)
		require.NotNil(t, ipdv3)
		require.Equal(t, int64(500), *ipdv3, "IPDV should be |5500 - 6000| = 500")
		conn.Close()
	})

	t.Run("calculates IPDV within batch using previous samples", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(20)
		targetPK := testPK(21)
		linkPK := testPK(22)

		// Single batch with multiple samples - IPDV should be calculated from previous sample in batch
		samples := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 6000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 5500,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Verify IPDV values
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT sample_index, rtt_us, ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? ORDER BY sample_index",
			originPK, targetPK, linkPK)
		require.NoError(t, err)

		// First sample: NULL IPDV (no previous)
		require.True(t, rows.Next())
		var idx0 int32
		var rtt0 int64
		var ipdv0 *int64
		require.NoError(t, rows.Scan(&idx0, &rtt0, &ipdv0))
		require.Equal(t, int32(0), idx0)
		require.Equal(t, int64(5000), rtt0)
		require.Nil(t, ipdv0)

		// Second sample: IPDV = 6000 - 5000 = 1000
		require.True(t, rows.Next())
		var idx1 int32
		var rtt1 int64
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&idx1, &rtt1, &ipdv1))
		require.Equal(t, int32(1), idx1)
		require.Equal(t, int64(6000), rtt1)
		require.NotNil(t, ipdv1)
		require.Equal(t, int64(1000), *ipdv1)

		// Third sample: IPDV = |5500 - 6000| = 500
		require.True(t, rows.Next())
		var idx2 int32
		var rtt2 int64
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&idx2, &rtt2, &ipdv2))
		require.Equal(t, int32(2), idx2)
		require.Equal(t, int64(5500), rtt2)
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(500), *ipdv2)

		require.False(t, rows.Next())
		rows.Close()
		conn.Close()
	})

	t.Run("handles multiple circuits in batch query", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Insert samples for multiple circuits
		circuit1 := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  testPK(30),
				TargetDevicePK:  testPK(31),
				LinkPK:          testPK(32),
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 5000,
			},
		}
		circuit2 := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  testPK(40),
				TargetDevicePK:  testPK(41),
				LinkPK:          testPK(42),
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 8000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), circuit1)
		require.NoError(t, err)
		err = store.AppendDeviceLinkLatencySamples(context.Background(), circuit2)
		require.NoError(t, err)

		// Now append new samples for both circuits - should query previous RTTs for both
		bothCircuits := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  testPK(30),
				TargetDevicePK:  testPK(31),
				LinkPK:          testPK(32),
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 6000,
			},
			{
				OriginDevicePK:  testPK(40),
				TargetDevicePK:  testPK(41),
				LinkPK:          testPK(42),
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 9000,
			},
		}

		err = store.AppendDeviceLinkLatencySamples(context.Background(), bothCircuits)
		require.NoError(t, err)

		// Verify IPDV was calculated correctly for both circuits
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)

		// Circuit 1: 6000 - 5000 = 1000
		rows, err := conn.Query(context.Background(),
			"SELECT ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 1",
			testPK(30), testPK(31), testPK(32))
		require.NoError(t, err)
		require.True(t, rows.Next())
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&ipdv1))
		rows.Close()
		require.NotNil(t, ipdv1)
		require.Equal(t, int64(1000), *ipdv1)

		// Circuit 2: 9000 - 8000 = 1000
		rows, err = conn.Query(context.Background(),
			"SELECT ipdv_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 1",
			testPK(40), testPK(41), testPK(42))
		require.NoError(t, err)
		require.True(t, rows.Next())
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&ipdv2))
		rows.Close()
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(1000), *ipdv2)

		conn.Close()
	})
}

func TestLake_TelemetryLatency_Store_AppendInternetMetroLatencySamples(t *testing.T) {
	t.Parallel()

	t.Run("appends samples to database", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(1)
		targetPK := testPK(2)
		dataProvider := "provider1"

		samples := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 10000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 11000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have inserted 2 latency samples")

		// Append more samples
		moreSamples := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 12000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), moreSamples)
		require.NoError(t, err)

		// Verify data was inserted by querying the database
		rows, err = conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(3), count, "should have inserted 3 latency samples")
		conn.Close()
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		err = store.AppendInternetMetroLatencySamples(context.Background(), []InternetMetroLatencySample{})
		require.NoError(t, err)

		// Verify no data was inserted
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(), "SELECT count() FROM fact_dz_internet_metro_latency")
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(0), count, "should have inserted no samples for empty slice")
		conn.Close()
	})

	t.Run("calculates IPDV from previous RTTs in database", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(50)
		targetPK := testPK(51)
		dataProvider := "test-provider"

		// First batch - no previous RTTs, so IPDV should be NULL
		firstBatch := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 10000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), firstBatch)
		require.NoError(t, err)

		// Verify first sample has NULL IPDV
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 0",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt1 int64
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&rtt1, &ipdv1))
		rows.Close()
		require.Equal(t, int64(10000), rtt1)
		require.Nil(t, ipdv1, "first sample should have NULL IPDV")

		// Second batch - should calculate IPDV from previous RTT (10000 -> 11000 = 1000)
		secondBatch := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 11000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), secondBatch)
		require.NoError(t, err)

		// Verify second sample has correct IPDV
		rows, err = conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 1",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt2 int64
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&rtt2, &ipdv2))
		rows.Close()
		require.Equal(t, int64(11000), rtt2)
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(1000), *ipdv2, "IPDV should be 11000 - 10000 = 1000")

		// Third batch - RTT decreased, IPDV should be absolute difference (11000 -> 10500 = 500)
		thirdBatch := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 10500,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), thirdBatch)
		require.NoError(t, err)

		// Verify third sample has correct IPDV
		rows, err = conn.Query(context.Background(),
			"SELECT rtt_us, ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 2",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt3 int64
		var ipdv3 *int64
		require.NoError(t, rows.Scan(&rtt3, &ipdv3))
		rows.Close()
		require.Equal(t, int64(10500), rtt3)
		require.NotNil(t, ipdv3)
		require.Equal(t, int64(500), *ipdv3, "IPDV should be |10500 - 11000| = 500")
		conn.Close()
	})

	t.Run("calculates IPDV within batch using previous samples", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(60)
		targetPK := testPK(61)
		dataProvider := "test-provider-2"

		// Single batch with multiple samples - IPDV should be calculated from previous sample in batch
		samples := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 10000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 11000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(3, 0),
				RTTMicroseconds: 10500,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Verify IPDV values
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		rows, err := conn.Query(context.Background(),
			"SELECT sample_index, rtt_us, ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? ORDER BY sample_index",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)

		// First sample: NULL IPDV (no previous)
		require.True(t, rows.Next())
		var idx0 int32
		var rtt0 int64
		var ipdv0 *int64
		require.NoError(t, rows.Scan(&idx0, &rtt0, &ipdv0))
		require.Equal(t, int32(0), idx0)
		require.Equal(t, int64(10000), rtt0)
		require.Nil(t, ipdv0)

		// Second sample: IPDV = 11000 - 10000 = 1000
		require.True(t, rows.Next())
		var idx1 int32
		var rtt1 int64
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&idx1, &rtt1, &ipdv1))
		require.Equal(t, int32(1), idx1)
		require.Equal(t, int64(11000), rtt1)
		require.NotNil(t, ipdv1)
		require.Equal(t, int64(1000), *ipdv1)

		// Third sample: IPDV = |10500 - 11000| = 500
		require.True(t, rows.Next())
		var idx2 int32
		var rtt2 int64
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&idx2, &rtt2, &ipdv2))
		require.Equal(t, int32(2), idx2)
		require.Equal(t, int64(10500), rtt2)
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(500), *ipdv2)

		require.False(t, rows.Next())
		rows.Close()
		conn.Close()
	})

	t.Run("handles multiple circuits in batch query", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Insert samples for multiple circuits
		circuit1 := []InternetMetroLatencySample{
			{
				OriginMetroPK:   testPK(70),
				TargetMetroPK:   testPK(71),
				DataProvider:    "provider1",
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 10000,
			},
		}
		circuit2 := []InternetMetroLatencySample{
			{
				OriginMetroPK:   testPK(80),
				TargetMetroPK:   testPK(81),
				DataProvider:    "provider2",
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1, 0),
				RTTMicroseconds: 15000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), circuit1)
		require.NoError(t, err)
		err = store.AppendInternetMetroLatencySamples(context.Background(), circuit2)
		require.NoError(t, err)

		// Now append new samples for both circuits - should query previous RTTs for both
		bothCircuits := []InternetMetroLatencySample{
			{
				OriginMetroPK:   testPK(70),
				TargetMetroPK:   testPK(71),
				DataProvider:    "provider1",
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 11000,
			},
			{
				OriginMetroPK:   testPK(80),
				TargetMetroPK:   testPK(81),
				DataProvider:    "provider2",
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(2, 0),
				RTTMicroseconds: 16000,
			},
		}

		err = store.AppendInternetMetroLatencySamples(context.Background(), bothCircuits)
		require.NoError(t, err)

		// Verify IPDV was calculated correctly for both circuits
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)

		// Circuit 1: 11000 - 10000 = 1000
		rows, err := conn.Query(context.Background(),
			"SELECT ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 1",
			testPK(70), testPK(71), "provider1")
		require.NoError(t, err)
		require.True(t, rows.Next())
		var ipdv1 *int64
		require.NoError(t, rows.Scan(&ipdv1))
		rows.Close()
		require.NotNil(t, ipdv1)
		require.Equal(t, int64(1000), *ipdv1)

		// Circuit 2: 16000 - 15000 = 1000
		rows, err = conn.Query(context.Background(),
			"SELECT ipdv_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 1",
			testPK(80), testPK(81), "provider2")
		require.NoError(t, err)
		require.True(t, rows.Next())
		var ipdv2 *int64
		require.NoError(t, rows.Scan(&ipdv2))
		rows.Close()
		require.NotNil(t, ipdv2)
		require.Equal(t, int64(1000), *ipdv2)

		conn.Close()
	})
}

func TestLake_TelemetryLatency_Store_GetExistingMaxSampleIndices(t *testing.T) {
	t.Parallel()

	t.Run("returns max sample indices for each circuit and epoch", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Insert samples for different device/link pairs and epochs
		// Use recent timestamps (within last 4 days) so GetExistingMaxSampleIndices can find them
		now := time.Now()
		samples := []DeviceLinkLatencySample{
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 0, Time: now.Add(-1 * time.Hour), RTTMicroseconds: 5000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 1, Time: now.Add(-2 * time.Hour), RTTMicroseconds: 6000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 100, SampleIndex: 2, Time: now.Add(-3 * time.Hour), RTTMicroseconds: 7000},
			{OriginDevicePK: testPK(1), TargetDevicePK: testPK(2), LinkPK: testPK(3), Epoch: 101, SampleIndex: 0, Time: now.Add(-4 * time.Hour), RTTMicroseconds: 8000},
			{OriginDevicePK: testPK(4), TargetDevicePK: testPK(5), LinkPK: testPK(6), Epoch: 100, SampleIndex: 0, Time: now.Add(-5 * time.Hour), RTTMicroseconds: 9000},
			{OriginDevicePK: testPK(4), TargetDevicePK: testPK(5), LinkPK: testPK(6), Epoch: 100, SampleIndex: 1, Time: now.Add(-6 * time.Hour), RTTMicroseconds: 10000},
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

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		indices, err := store.GetExistingMaxSampleIndices()
		require.NoError(t, err)
		require.Empty(t, indices)
	})
}

func TestLake_TelemetryLatency_Store_AppendDeviceLinkLatencySamples_Idempotency(t *testing.T) {
	t.Parallel()

	t.Run("inserting same data twice is idempotent", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(100)
		targetPK := testPK(101)
		linkPK := testPK(102)

		samples := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1001, 0),
				RTTMicroseconds: 6000,
			},
		}

		// Insert first time
		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Insert second time (same data)
		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_device_link_latency FINAL")
		require.NoError(t, err)

		// Count should be 2 (not 4) due to ReplacingMergeTree deduplication
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have exactly 2 samples after deduplication, not 4")
		conn.Close()
	})

	t.Run("inserting same data with different ingested_at keeps latest version", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(110)
		targetPK := testPK(111)
		linkPK := testPK(112)

		// First insert with RTT 5000
		samples1 := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 5000,
			},
		}
		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples1)
		require.NoError(t, err)

		// Wait a moment to ensure different ingested_at
		time.Sleep(10 * time.Millisecond)

		// Second insert with same key but different RTT (simulating re-ingestion with updated value)
		samples2 := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 5500, // Different value
			},
		}
		err = store.AppendDeviceLinkLatencySamples(context.Background(), samples2)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_device_link_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 1 row with the latest RTT value
		rows, err := conn.Query(context.Background(),
			"SELECT rtt_us FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? AND sample_index = 0",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt int64
		require.NoError(t, rows.Scan(&rtt))
		rows.Close()
		require.Equal(t, int64(5500), rtt, "should have the latest RTT value after deduplication")
		conn.Close()
	})
}

func TestLake_TelemetryLatency_Store_AppendInternetMetroLatencySamples_Idempotency(t *testing.T) {
	t.Parallel()

	t.Run("inserting same data twice is idempotent", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(200)
		targetPK := testPK(201)
		dataProvider := "test-provider"

		samples := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 10000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1001, 0),
				RTTMicroseconds: 11000,
			},
		}

		// Insert first time
		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Insert second time (same data)
		err = store.AppendInternetMetroLatencySamples(context.Background(), samples)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Count should be 2 (not 4) due to ReplacingMergeTree deduplication
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have exactly 2 samples after deduplication, not 4")
		conn.Close()
	})

	t.Run("inserting same data with different ingested_at keeps latest version", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(210)
		targetPK := testPK(211)
		dataProvider := "test-provider-2"

		// First insert with RTT 10000
		samples1 := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 10000,
			},
		}
		err = store.AppendInternetMetroLatencySamples(context.Background(), samples1)
		require.NoError(t, err)

		// Wait a moment to ensure different ingested_at
		time.Sleep(10 * time.Millisecond)

		// Second insert with same key but different RTT (simulating re-ingestion with updated value)
		samples2 := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000, 0),
				RTTMicroseconds: 10500, // Different value
			},
		}
		err = store.AppendInternetMetroLatencySamples(context.Background(), samples2)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 1 row with the latest RTT value
		rows, err := conn.Query(context.Background(),
			"SELECT rtt_us FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? AND sample_index = 0",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var rtt int64
		require.NoError(t, rows.Scan(&rtt))
		rows.Close()
		require.Equal(t, int64(10500), rtt, "should have the latest RTT value after deduplication")
		conn.Close()
	})
}

func TestLake_TelemetryLatency_Store_GetExistingInternetMaxSampleIndices(t *testing.T) {
	t.Parallel()

	t.Run("returns max sample indices for each circuit, data provider, and epoch", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Insert samples for different metro pairs, data providers, and epochs
		// Use recent timestamps (within last 4 days) so GetExistingInternetMaxSampleIndices can find them
		now := time.Now()
		samples := []InternetMetroLatencySample{
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 0, Time: now.Add(-1 * time.Hour), RTTMicroseconds: 10000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 1, Time: now.Add(-2 * time.Hour), RTTMicroseconds: 11000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 100, SampleIndex: 2, Time: now.Add(-3 * time.Hour), RTTMicroseconds: 12000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider1", Epoch: 101, SampleIndex: 0, Time: now.Add(-4 * time.Hour), RTTMicroseconds: 13000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider2", Epoch: 100, SampleIndex: 0, Time: now.Add(-5 * time.Hour), RTTMicroseconds: 14000},
			{OriginMetroPK: testPK(1), TargetMetroPK: testPK(2), DataProvider: "provider2", Epoch: 100, SampleIndex: 1, Time: now.Add(-6 * time.Hour), RTTMicroseconds: 15000},
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

		db := testClient(t)

		store, err := NewStore(StoreConfig{
			Logger:     laketesting.NewLogger(),
			ClickHouse: db,
		})
		require.NoError(t, err)

		indices, err := store.GetExistingInternetMaxSampleIndices()
		require.NoError(t, err)
		require.Empty(t, indices)
	})
}
