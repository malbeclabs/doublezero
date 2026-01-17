package dztelemlatency

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
)

func TestLake_TelemetryLatency_Store_BackfillInternetMetroLatency_Idempotency(t *testing.T) {
	t.Parallel()

	t.Run("simulated backfill is idempotent - no duplicates after multiple runs", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(50)
		targetPK := testPK(51)
		dataProvider := "ripeatlas"

		backfillData := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 15000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 15100,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(1000002, 0),
				RTTMicroseconds: 15200,
			},
		}

		// Simulate first backfill run
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Simulate second backfill run (same data)
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Simulate third backfill run (same data)
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Force merge to deduplicate (ReplacingMergeTree behavior)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 3 samples, not 9
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(3), count, "should have exactly 3 samples after 3 identical backfill runs")
	})

	t.Run("backfill with existing data does not create duplicates", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(60)
		targetPK := testPK(61)
		dataProvider := "wheresitup"

		// Pre-existing data (from regular indexer operation)
		existingData := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 15000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 15100,
			},
		}
		err = store.AppendInternetMetroLatencySamples(context.Background(), existingData)
		require.NoError(t, err)

		// Backfill data includes existing data PLUS older data
		backfillData := []InternetMetroLatencySample{
			// Re-insert of existing data (should be deduplicated)
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 15000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 15100,
			},
			// New data from older epoch (the actual backfill)
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           99, // Older epoch
				SampleIndex:     0,
				Time:            time.Unix(900000, 0),
				RTTMicroseconds: 14800,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           99, // Older epoch
				SampleIndex:     1,
				Time:            time.Unix(900001, 0),
				RTTMicroseconds: 14900,
			},
		}
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 4 samples (2 from epoch 100 + 2 from epoch 99)
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(4), count, "should have exactly 4 unique samples")

		// Verify we have samples from both epochs
		rows, err = conn.Query(context.Background(),
			"SELECT DISTINCT epoch FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ? ORDER BY epoch",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)

		var epochs []int64
		for rows.Next() {
			var epoch int64
			require.NoError(t, rows.Scan(&epoch))
			epochs = append(epochs, epoch)
		}
		rows.Close()
		require.Equal(t, []int64{99, 100}, epochs, "should have samples from epochs 99 and 100")
	})

	t.Run("multiple data providers are handled independently", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(70)
		targetPK := testPK(71)

		// Insert data for both providers
		backfillData := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    "ripeatlas",
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 15000,
			},
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    "wheresitup",
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 16000,
			},
		}

		// Insert twice
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)
		err = store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Force merge
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 2 samples (1 per provider)
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ?",
			originPK, targetPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have exactly 2 samples (one per provider)")
	})

	t.Run("multiple concurrent backfills are safe", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(80)
		targetPK := testPK(81)
		dataProvider := "ripeatlas"

		backfillData := []InternetMetroLatencySample{
			{
				OriginMetroPK:   originPK,
				TargetMetroPK:   targetPK,
				DataProvider:    dataProvider,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 15000,
			},
		}

		// Simulate concurrent backfill runs
		done := make(chan error, 5)
		for i := 0; i < 5; i++ {
			go func() {
				done <- store.AppendInternetMetroLatencySamples(context.Background(), backfillData)
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 5; i++ {
			err := <-done
			require.NoError(t, err)
		}

		// Force merge
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_internet_metro_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 1 sample despite 5 concurrent inserts
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_internet_metro_latency WHERE origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?",
			originPK, targetPK, dataProvider)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(1), count, "should have exactly 1 sample after concurrent backfills")
	})
}

func TestLake_TelemetryLatency_Store_GenerateMetroPairs(t *testing.T) {
	t.Parallel()

	t.Run("generates unique pairs with consistent ordering", func(t *testing.T) {
		t.Parallel()

		metros := []dzsvc.Metro{
			{PK: testPK(1), Code: "NYC"},
			{PK: testPK(2), Code: "LAX"},
			{PK: testPK(3), Code: "CHI"},
		}

		pairs := GenerateMetroPairs(metros)

		// Should have 3 pairs: CHI-LAX, CHI-NYC, LAX-NYC (sorted by origin code)
		require.Len(t, pairs, 3)

		// Verify consistent ordering (origin < target)
		for _, pair := range pairs {
			require.True(t, pair.Origin.Code < pair.Target.Code,
				"origin code should be less than target code: %s < %s", pair.Origin.Code, pair.Target.Code)
		}

		// Verify sorted by origin, then target
		require.Equal(t, "CHI", pairs[0].Origin.Code)
		require.Equal(t, "LAX", pairs[0].Target.Code)
		require.Equal(t, "CHI", pairs[1].Origin.Code)
		require.Equal(t, "NYC", pairs[1].Target.Code)
		require.Equal(t, "LAX", pairs[2].Origin.Code)
		require.Equal(t, "NYC", pairs[2].Target.Code)
	})

	t.Run("handles empty metros", func(t *testing.T) {
		t.Parallel()

		pairs := GenerateMetroPairs([]dzsvc.Metro{})
		require.Empty(t, pairs)
	})

	t.Run("handles single metro", func(t *testing.T) {
		t.Parallel()

		metros := []dzsvc.Metro{
			{PK: testPK(1), Code: "NYC"},
		}

		pairs := GenerateMetroPairs(metros)
		require.Empty(t, pairs, "single metro should produce no pairs")
	})
}
