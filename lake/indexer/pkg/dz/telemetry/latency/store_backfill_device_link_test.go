package dztelemlatency

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
)

func TestLake_TelemetryLatency_Store_BackfillDeviceLinkLatency_Idempotency(t *testing.T) {
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

		// Simulate backfill data - this is what the backfill command would fetch from RPC
		originPK := testPK(1)
		targetPK := testPK(2)
		linkPK := testPK(3)

		backfillData := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 5100,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     2,
				Time:            time.Unix(1000002, 0),
				RTTMicroseconds: 5200,
			},
		}

		// Simulate first backfill run
		err = store.AppendDeviceLinkLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Simulate second backfill run (same data)
		err = store.AppendDeviceLinkLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Simulate third backfill run (same data)
		err = store.AppendDeviceLinkLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Force merge to deduplicate (ReplacingMergeTree behavior)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_device_link_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 3 samples, not 9
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
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

		originPK := testPK(10)
		targetPK := testPK(11)
		linkPK := testPK(12)

		// Pre-existing data (from regular indexer operation)
		existingData := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 5100,
			},
		}
		err = store.AppendDeviceLinkLatencySamples(context.Background(), existingData)
		require.NoError(t, err)

		// Backfill data includes existing data PLUS older data
		backfillData := []DeviceLinkLatencySample{
			// Re-insert of existing data (should be deduplicated)
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 5000,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     1,
				Time:            time.Unix(1000001, 0),
				RTTMicroseconds: 5100,
			},
			// New data from older epoch (the actual backfill)
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           99, // Older epoch
				SampleIndex:     0,
				Time:            time.Unix(900000, 0),
				RTTMicroseconds: 4800,
			},
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           99, // Older epoch
				SampleIndex:     1,
				Time:            time.Unix(900001, 0),
				RTTMicroseconds: 4900,
			},
		}
		err = store.AppendDeviceLinkLatencySamples(context.Background(), backfillData)
		require.NoError(t, err)

		// Force merge to deduplicate
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_device_link_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 4 samples (2 from epoch 100 + 2 from epoch 99)
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(4), count, "should have exactly 4 unique samples")

		// Verify we have samples from both epochs
		rows, err = conn.Query(context.Background(),
			"SELECT DISTINCT epoch FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ? ORDER BY epoch",
			originPK, targetPK, linkPK)
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

	t.Run("multiple concurrent backfills are safe", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()

		store, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		originPK := testPK(20)
		targetPK := testPK(21)
		linkPK := testPK(22)

		backfillData := []DeviceLinkLatencySample{
			{
				OriginDevicePK:  originPK,
				TargetDevicePK:  targetPK,
				LinkPK:          linkPK,
				Epoch:           100,
				SampleIndex:     0,
				Time:            time.Unix(1000000, 0),
				RTTMicroseconds: 5000,
			},
		}

		// Simulate concurrent backfill runs
		done := make(chan error, 5)
		for i := 0; i < 5; i++ {
			go func() {
				done <- store.AppendDeviceLinkLatencySamples(context.Background(), backfillData)
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

		err = conn.Exec(context.Background(), "OPTIMIZE TABLE fact_dz_device_link_latency FINAL")
		require.NoError(t, err)

		// Should have exactly 1 sample despite 5 concurrent inserts
		rows, err := conn.Query(context.Background(),
			"SELECT count() FROM fact_dz_device_link_latency WHERE origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?",
			originPK, targetPK, linkPK)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(1), count, "should have exactly 1 sample after concurrent backfills")
	})
}
