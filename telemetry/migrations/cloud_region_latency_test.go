package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCloudRegionLatency covers the table created by 20260910000000_cloud_region_latency.sql.
func TestCloudRegionLatency(t *testing.T) {
	t.Parallel()
	db := newClickHouseWithMigrations(t)

	eventTS := time.Now().UTC().Truncate(time.Second)

	t.Run("samples from different probes survive a merge", func(t *testing.T) {
		mustExec(t, db, `
			INSERT INTO cloud_region_latency
				(event_ts, ingested_at, origin_cloud, origin_region, target_cloud, target_region, data_provider, probe_id, rtt_us, packets_sent, packets_received) VALUES
				(?, ?, 'aws', 'ap-south-1', 'aws', 'eu-west-1', 'ripeatlas', 1003385, 120500, 3, 3),
				(?, ?, 'aws', 'ap-south-1', 'aws', 'eu-west-1', 'ripeatlas', 1003386, 121900, 3, 3)
		`, eventTS, eventTS, eventTS, eventTS)

		mustExec(t, db, `OPTIMIZE TABLE cloud_region_latency FINAL`)

		probeIDs := selectAll(t, db, `
			SELECT probe_id
			FROM cloud_region_latency
			WHERE origin_region = 'ap-south-1' AND target_region = 'eu-west-1'
			ORDER BY probe_id
		`, func(r *sql.Rows) (uint32, error) {
			var v uint32
			err := r.Scan(&v)
			return v, err
		})

		require.Equal(t, []uint32{1003385, 1003386}, probeIDs,
			"samples that differ only by probe must both survive the merge")
	})

	t.Run("a re-exported sample collapses to one row", func(t *testing.T) {
		mustExec(t, db, `
			INSERT INTO cloud_region_latency
				(event_ts, ingested_at, origin_cloud, origin_region, target_cloud, target_region, data_provider, probe_id, rtt_us, packets_sent, packets_received) VALUES
				(?, ?, 'aws', 'ap-northeast-1', 'aws', 'us-west-2', 'ripeatlas', 1003390, 98000, 3, 3)
		`, eventTS, eventTS)
		mustExec(t, db, `
			INSERT INTO cloud_region_latency
				(event_ts, ingested_at, origin_cloud, origin_region, target_cloud, target_region, data_provider, probe_id, rtt_us, packets_sent, packets_received) VALUES
				(?, ?, 'aws', 'ap-northeast-1', 'aws', 'us-west-2', 'ripeatlas', 1003390, 98000, 3, 3)
		`, eventTS, eventTS.Add(time.Minute))

		mustExec(t, db, `OPTIMIZE TABLE cloud_region_latency FINAL`)

		counts := selectAll(t, db, `
			SELECT count()
			FROM cloud_region_latency
			WHERE origin_region = 'ap-northeast-1' AND target_region = 'us-west-2'
		`, func(r *sql.Rows) (uint64, error) {
			var v uint64
			err := r.Scan(&v)
			return v, err
		})

		require.Equal(t, []uint64{1}, counts, "the same probe's same sample must collapse")
	})

	t.Run("a total loss row is readable", func(t *testing.T) {
		mustExec(t, db, `
			INSERT INTO cloud_region_latency
				(event_ts, ingested_at, origin_cloud, origin_region, target_cloud, target_region, data_provider, probe_id, rtt_us, packets_sent, packets_received) VALUES
				(?, ?, 'aws', 'sa-east-1', 'aws', 'eu-north-1', 'ripeatlas', 1003391, 0, 3, 0)
		`, eventTS, eventTS)

		type row struct {
			RttUs           uint32
			PacketsSent     uint8
			PacketsReceived uint8
		}
		rows := selectAll(t, db, `
			SELECT rtt_us, packets_sent, packets_received
			FROM cloud_region_latency
			WHERE origin_region = 'sa-east-1' AND target_region = 'eu-north-1'
		`, func(r *sql.Rows) (row, error) {
			var v row
			err := r.Scan(&v.RttUs, &v.PacketsSent, &v.PacketsReceived)
			return v, err
		})

		require.Len(t, rows, 1)
		require.Equal(t, uint32(0), rows[0].RttUs)
		require.Equal(t, uint8(3), rows[0].PacketsSent)
		require.Equal(t, uint8(0), rows[0].PacketsReceived)
	})

	t.Run("the new table is created alongside the existing ones", func(t *testing.T) {
		names := selectAll(t, db, `
			SELECT name
			FROM system.tables
			WHERE database = currentDatabase()
			  AND name IN ('cloud_region_latency', 'device_ifindex', 'flows', 'interface_state', 'isis_global_state')
			ORDER BY name
		`, func(r *sql.Rows) (string, error) {
			var v string
			err := r.Scan(&v)
			return v, err
		})

		require.Equal(t, []string{
			"cloud_region_latency",
			"device_ifindex",
			"flows",
			"interface_state",
			"isis_global_state",
		}, names)
	})
}
