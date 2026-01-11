package indexer

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestDimCurrentViews tests that all the current views for type2 dimension tables work correctly
func TestDimCurrentViews(t *testing.T) {
	t.Parallel()

	t.Run("dz_contributors_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		// Insert test data into history table
		entityID := "contributor1"
		opID1 := uuid.New()
		opID2 := uuid.New()

		// Insert first version
		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 0, 12345, 'pk1', 'CODE1', 'Name1')
		`, entityID, now, now, opID1)
		require.NoError(t, err)

		// Insert updated version (later snapshot_ts)
		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 0, 12346, 'pk1', 'CODE1', 'Name1Updated')
		`, entityID, now.Add(time.Hour), now.Add(time.Hour), opID2)
		require.NoError(t, err)

		// Query current view - should return the latest version
		rows, err := conn.Query(ctx, `SELECT code, name FROM dz_contributors_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var code, name string
		require.NoError(t, rows.Scan(&code, &name))
		require.Equal(t, "CODE1", code)
		require.Equal(t, "Name1Updated", name)
		rows.Close()

		// Insert deleted version
		opID3 := uuid.New()
		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 1, 12347, 'pk1', 'CODE1', 'Name1Updated')
		`, entityID, now.Add(2*time.Hour), now.Add(2*time.Hour), opID3)
		require.NoError(t, err)

		// Query current view - should return no rows (entity is deleted)
		rows, err = conn.Query(ctx, `SELECT COUNT(*) FROM dz_contributors_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var count uint64
		require.NoError(t, rows.Scan(&count))
		require.Equal(t, uint64(0), count)
		rows.Close()
	})

	t.Run("dz_devices_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "device1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_devices_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, status, device_type, code, public_ip, contributor_pk, metro_pk, max_users)
			VALUES (?, ?, ?, ?, 0, 12345, 'pk1', 'activated', 'switch', 'DEV001', '1.2.3.4', 'contrib_pk', 'metro_pk', 100)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT code, status FROM dz_devices_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var code, status string
		require.NoError(t, rows.Scan(&code, &status))
		require.Equal(t, "DEV001", code)
		require.Equal(t, "activated", status)
		rows.Close()
	})

	t.Run("dz_users_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "user1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_users_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, owner_pubkey, status, kind, client_ip, dz_ip, device_pk, tunnel_id)
			VALUES (?, ?, ?, ?, 0, 12345, 'pk1', 'owner1', 'activated', 'user', '10.0.0.1', '192.168.1.1', 'device_pk', 100)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT status, dz_ip FROM dz_users_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var status, dzIP string
		require.NoError(t, rows.Scan(&status, &dzIP))
		require.Equal(t, "activated", status)
		require.Equal(t, "192.168.1.1", dzIP)
		rows.Close()
	})

	t.Run("dz_metros_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "metro1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_metros_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name, longitude, latitude)
			VALUES (?, ?, ?, ?, 0, 12345, 'pk1', 'NYC', 'New York', -74.0060, 40.7128)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT code, name, latitude FROM dz_metros_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var code, name string
		var lat float64
		require.NoError(t, rows.Scan(&code, &name, &lat))
		require.Equal(t, "NYC", code)
		require.Equal(t, "New York", name)
		require.InDelta(t, 40.7128, lat, 0.0001)
		rows.Close()
	})

	t.Run("dz_links_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "link1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_links_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, status, code, tunnel_net, contributor_pk, side_a_pk, side_z_pk, side_a_iface_name, side_z_iface_name, link_type, committed_rtt_ns, committed_jitter_ns, bandwidth_bps, isis_delay_override_ns)
			VALUES (?, ?, ?, ?, 0, 12345, 'pk1', 'activated', 'LINK001', '192.168.1.0/24', 'contrib_pk', 'side_a', 'side_z', 'eth0', 'eth1', 'WAN', 5000000, 1000000, 1000000000, 0)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT code, link_type, status FROM dz_links_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var code, linkType, status string
		require.NoError(t, rows.Scan(&code, &linkType, &status))
		require.Equal(t, "LINK001", code)
		require.Equal(t, "WAN", linkType)
		require.Equal(t, "activated", status)
		rows.Close()
	})

	t.Run("geoip_records_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "1.2.3.4"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_geoip_records_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, ip, country_code, country, region, city, city_id, metro_name, latitude, longitude, postal_code, time_zone, accuracy_radius, asn, asn_org, is_anycast, is_anonymous_proxy, is_satellite_provider)
			VALUES (?, ?, ?, ?, 0, 12345, '1.2.3.4', 'US', 'United States', 'NY', 'New York', 123, 'NYC', 40.7128, -74.0060, '10001', 'America/New_York', 10, 12345, 'ASN Org', 0, 0, 0)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT ip, country_code, city FROM geoip_records_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var ip, countryCode, city string
		require.NoError(t, rows.Scan(&ip, &countryCode, &city))
		require.Equal(t, "1.2.3.4", ip)
		require.Equal(t, "US", countryCode)
		require.Equal(t, "New York", city)
		rows.Close()
	})

	t.Run("solana_leader_schedule_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "node1_epoch100"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_solana_leader_schedule_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, node_pubkey, epoch, slots, slot_count)
			VALUES (?, ?, ?, ?, 0, 12345, 'node1', 100, '1,2,3', 3)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT node_pubkey, epoch, slot_count FROM solana_leader_schedule_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var nodePubkey string
		var epoch int64
		var slotCount int64
		require.NoError(t, rows.Scan(&nodePubkey, &epoch, &slotCount))
		require.Equal(t, "node1", nodePubkey)
		require.Equal(t, int64(100), epoch)
		require.Equal(t, int64(3), slotCount)
		rows.Close()
	})

	t.Run("solana_vote_accounts_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "vote1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_solana_vote_accounts_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, vote_pubkey, epoch, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage)
			VALUES (?, ?, ?, ?, 0, 12345, 'vote1', 100, 'node1', 1000000000, 'true', 5)
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT vote_pubkey, epoch, activated_stake_lamports FROM solana_vote_accounts_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var votePubkey string
		var epoch int64
		var stake int64
		require.NoError(t, rows.Scan(&votePubkey, &epoch, &stake))
		require.Equal(t, "vote1", votePubkey)
		require.Equal(t, int64(100), epoch)
		require.Equal(t, int64(1000000000), stake)
		rows.Close()
	})

	t.Run("solana_gossip_nodes_current", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		entityID := "gossip1"
		opID := uuid.New()

		err = conn.Exec(ctx, `
			INSERT INTO dim_solana_gossip_nodes_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pubkey, epoch, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version)
			VALUES (?, ?, ?, ?, 0, 12345, 'gossip1', 100, '1.2.3.4', 8001, '1.2.3.5', 8002, '1.0.0')
		`, entityID, now, now, opID)
		require.NoError(t, err)

		rows, err := conn.Query(ctx, `SELECT pubkey, gossip_ip, version FROM solana_gossip_nodes_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var pubkey, gossipIP, version string
		require.NoError(t, rows.Scan(&pubkey, &gossipIP, &version))
		require.Equal(t, "gossip1", pubkey)
		require.Equal(t, "1.2.3.4", gossipIP)
		require.Equal(t, "1.0.0", version)
		rows.Close()
	})

	t.Run("all_views_exclude_deleted", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		// Test that deleted entities don't appear in current views
		testCases := []struct {
			viewName     string
			historyTable string
			entityID     string
			insertSQL    string
		}{
			{
				viewName:     "dz_contributors_current",
				historyTable: "dim_dz_contributors_history",
				entityID:     "deleted_contrib",
				insertSQL:    "INSERT INTO dim_dz_contributors_history (entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name) VALUES (?, ?, ?, ?, 1, 12345, 'pk', 'CODE', 'Name')",
			},
			{
				viewName:     "dz_devices_current",
				historyTable: "dim_dz_devices_history",
				entityID:     "deleted_device",
				insertSQL:    "INSERT INTO dim_dz_devices_history (entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, status, device_type, code, public_ip, contributor_pk, metro_pk, max_users) VALUES (?, ?, ?, ?, 1, 12345, 'pk', 'activated', 'switch', 'DEV', '1.2.3.4', 'contrib', 'metro', 100)",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.viewName, func(t *testing.T) {
				opID := uuid.New()
				err := conn.Exec(ctx, tc.insertSQL, tc.entityID, now, now, opID)
				require.NoError(t, err)

				rows, err := conn.Query(ctx, `SELECT COUNT(*) FROM `+tc.viewName+` WHERE entity_id = ?`, tc.entityID)
				require.NoError(t, err)
				require.True(t, rows.Next())
				var count uint64
				require.NoError(t, rows.Scan(&count))
				require.Equal(t, uint64(0), count, "deleted entity should not appear in current view")
				rows.Close()
			})
		}
	})

	t.Run("all_views_return_latest_version", func(t *testing.T) {
		t.Parallel()
		db := testClient(t)
		conn, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		// Test that views return the latest version when multiple versions exist
		entityID := "multi_version"
		opID1 := uuid.New()
		opID2 := uuid.New()
		opID3 := uuid.New()

		// Insert three versions with different snapshot_ts
		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 0, 1, 'pk', 'V1', 'Version1')
		`, entityID, now, now, opID1)
		require.NoError(t, err)

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 0, 2, 'pk', 'V2', 'Version2')
		`, entityID, now.Add(time.Hour), now.Add(time.Hour), opID2)
		require.NoError(t, err)

		err = conn.Exec(ctx, `
			INSERT INTO dim_dz_contributors_history
			(entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash, pk, code, name)
			VALUES (?, ?, ?, ?, 0, 3, 'pk', 'V3', 'Version3')
		`, entityID, now.Add(2*time.Hour), now.Add(2*time.Hour), opID3)
		require.NoError(t, err)

		// Query current view - should return V3 (latest)
		rows, err := conn.Query(ctx, `SELECT code, name FROM dz_contributors_current WHERE entity_id = ?`, entityID)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var code, name string
		require.NoError(t, rows.Scan(&code, &name))
		require.Equal(t, "V3", code)
		require.Equal(t, "Version3", name)
		rows.Close()
	})
}
