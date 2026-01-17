package geoip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func testPK(n int) string {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(n + i)
	}
	return solana.PublicKeyFromBytes(bytes).String()
}

type mockGeoIPResolver struct {
	resolveFunc func(net.IP) *geoip.Record
}

func (m *mockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	if m.resolveFunc != nil {
		return m.resolveFunc(ip)
	}
	return &geoip.Record{
		IP:          ip,
		CountryCode: "US",
		Country:     "United States",
		City:        "Test City",
	}
}

func TestLake_GeoIP_View_NewView(t *testing.T) {
	t.Parallel()

	t.Run("returns error when config validation fails", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			view, err := NewView(ViewConfig{
				GeoIPStore:          geoipStore,
				GeoIPResolver:       &mockGeoIPResolver{},
				ServiceabilityStore: svcStore,
				SolanaStore:         solStore,
				RefreshInterval:     time.Second,
			})
			require.Error(t, err)
			require.Nil(t, view)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing serviceability store", func(t *testing.T) {
			t.Parallel()
			view, err := NewView(ViewConfig{
				Logger:          laketesting.NewLogger(),
				GeoIPStore:      geoipStore,
				GeoIPResolver:   &mockGeoIPResolver{},
				SolanaStore:     solStore,
				RefreshInterval: time.Second,
			})
			require.Error(t, err)
			require.Nil(t, view)
			require.Contains(t, err.Error(), "serviceability store is required")
		})

		t.Run("missing solana store", func(t *testing.T) {
			t.Parallel()
			view, err := NewView(ViewConfig{
				Logger:              log,
				GeoIPStore:          geoipStore,
				GeoIPResolver:       &mockGeoIPResolver{},
				ServiceabilityStore: svcStore,
				RefreshInterval:     time.Second,
			})
			require.Error(t, err)
			require.Nil(t, view)
			require.Contains(t, err.Error(), "solana store is required")
		})
	})

	t.Run("returns view when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:              log,
			Clock:               clockwork.NewFakeClock(),
			GeoIPStore:          geoipStore,
			GeoIPResolver:       &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:         solStore,
			RefreshInterval:     time.Second,
		})
		require.NoError(t, err)
		require.NotNil(t, view)
	})
}

func TestLake_GeoIP_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:              log,
			Clock:               clockwork.NewFakeClock(),
			GeoIPStore:          geoipStore,
			GeoIPResolver:       &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:         solStore,
			RefreshInterval:     time.Second,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})
}

func TestLake_GeoIP_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("resolves and saves geoip records from users and gossip nodes", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Set up test data: users with client IPs
		ctx := context.Background()

		userPK1 := testPK(1)
		userPK2 := testPK(2)
		ownerPubkey := testPK(3)
		devicePK := testPK(4)

		users := []dzsvc.User{
			{
				PK:          userPK1,
				OwnerPubkey: ownerPubkey,
				Status:      "activated",
				Kind:        "ibrl",
				ClientIP:    net.ParseIP("1.1.1.1"),
				DZIP:        net.ParseIP("10.0.0.1"),
				DevicePK:    devicePK,
				TunnelID:    1,
			},
			{
				PK:          userPK2,
				OwnerPubkey: ownerPubkey,
				Status:      "activated",
				Kind:        "ibrl",
				ClientIP:    net.ParseIP("8.8.8.8"),
				DZIP:        net.ParseIP("10.0.0.2"),
				DevicePK:    devicePK,
				TunnelID:    2,
			},
		}

		err = svcStore.ReplaceUsers(ctx, users)
		require.NoError(t, err)

		// Wait for users to be written to history table
		// No need to optimize since we query history directly
		time.Sleep(500 * time.Millisecond)

		// Set up gossip nodes
		nodePK1 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		nodePK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		gossipAddr1 := "192.168.1.1:8001"
		gossipAddr2 := "192.168.1.2:8001"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK1,
				Gossip:  &gossipAddr1,
				Version: &nodeVersion,
			},
			{
				Pubkey:  nodePK2,
				Gossip:  &gossipAddr2,
				Version: &nodeVersion,
			},
		}

		err = solStore.ReplaceGossipNodes(ctx, nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		// Create mock resolver
		resolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{
					IP:          ip,
					CountryCode: "US",
					Country:     "United States",
					City:        "Test City",
				}
			},
		}

		view, err := NewView(ViewConfig{
			Logger:              log,
			Clock:               clockwork.NewFakeClock(),
			GeoIPStore:          geoipStore,
			GeoIPResolver:       resolver,
			ServiceabilityStore: svcStore,
			SolanaStore:         solStore,
			RefreshInterval:     time.Second,
		})
		require.NoError(t, err)

		// Refresh the view
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify geoip records were saved
		// GetRecords method was removed - verify data was inserted by querying history table
		// Data may need a moment to be written, so retry
		var count uint64
		for i := 0; i < 20; i++ {
			conn2, err := db.Conn(context.Background())
			require.NoError(t, err)
			var rows driver.Rows
			rows, err = conn2.Query(context.Background(), `
				WITH ranked AS (
					SELECT
						*,
						ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
					FROM dim_geoip_records_history
				)
				SELECT count() FROM ranked WHERE rn = 1 AND is_deleted = 0
			`)
			require.NoError(t, err)
			require.True(t, rows.Next())
			err = rows.Scan(&count)
			require.NoError(t, err)
			rows.Close()
			conn2.Close()
			if count >= 4 {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.GreaterOrEqual(t, count, uint64(4), "should have at least 4 records")

		// Check that all expected IPs are present using the dataset API
		conn3, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn3.Close()

		d, err := NewGeoIPRecordDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, d)

		for _, ip := range []string{"1.1.1.1", "8.8.8.8", "192.168.1.1", "192.168.1.2"} {
			var current map[string]any
			for i := 0; i < 20; i++ {
				entityID := dataset.NewNaturalKey(ip).ToSurrogate()
				current, err = d.GetCurrentRow(context.Background(), conn3, entityID)
				require.NoError(t, err)
				if current != nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			require.NotNil(t, current, "should have found record for %s", ip)
		}
	})

	t.Run("handles empty stores gracefully", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Create empty tables so queries don't fail
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create empty gossip nodes table
		// Note: solana_gossip_nodes_current table no longer exists - using history table instead

		view, err := NewView(ViewConfig{
			Logger:              log,
			Clock:               clockwork.NewFakeClock(),
			GeoIPStore:          geoipStore,
			GeoIPResolver:       &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:         solStore,
			RefreshInterval:     time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err) // Should succeed even with no data

		require.True(t, view.Ready())
	})

	t.Run("deduplicates IPs from multiple sources", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)
		log := laketesting.NewLogger()
		geoipStore, err := NewStore(StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger:     log,
			ClickHouse: db,
		})
		require.NoError(t, err)

		// Set up test data with same IP in both users and gossip nodes
		ctx := context.Background()

		userPK := testPK(1)
		ownerPubkey := testPK(2)
		devicePK := testPK(3)

		users := []dzsvc.User{
			{
				PK:          userPK,
				OwnerPubkey: ownerPubkey,
				Status:      "activated",
				Kind:        "ibrl",
				ClientIP:    net.ParseIP("1.1.1.1"),
				DZIP:        net.ParseIP("10.0.0.1"),
				DevicePK:    devicePK,
				TunnelID:    1,
			},
		}

		err = svcStore.ReplaceUsers(ctx, users)
		require.NoError(t, err)

		// Wait for users to be synced to current table
		// Wait for users to be written to history table
		// No need to optimize since we query history directly
		time.Sleep(500 * time.Millisecond)

		// Set up gossip node with same IP
		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		gossipAddr := "1.1.1.1:8001"
		fetchedAt := time.Now().UTC()
		currentEpoch := uint64(100)

		nodeVersion := "1.0.0"
		nodes := []*solanarpc.GetClusterNodesResult{
			{
				Pubkey:  nodePK,
				Gossip:  &gossipAddr,
				Version: &nodeVersion,
			},
		}

		err = solStore.ReplaceGossipNodes(ctx, nodes, fetchedAt, currentEpoch)
		require.NoError(t, err)

		resolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{
					IP:          ip,
					CountryCode: "US",
				}
			},
		}

		view, err := NewView(ViewConfig{
			Logger:              log,
			Clock:               clockwork.NewFakeClock(),
			GeoIPStore:          geoipStore,
			GeoIPResolver:       resolver,
			ServiceabilityStore: svcStore,
			SolanaStore:         solStore,
			RefreshInterval:     time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Should only have one record for 1.1.1.1 (deduplicated)
		// Verify data was inserted using the dataset API
		// Data may need a moment to be written, so retry
		conn5, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer conn5.Close()

		d, err := NewGeoIPRecordDataset(laketesting.NewLogger())
		require.NoError(t, err)
		require.NotNil(t, d)

		var current map[string]any
		for i := 0; i < 20; i++ {
			entityID := dataset.NewNaturalKey("1.1.1.1").ToSurrogate()
			current, err = d.GetCurrentRow(context.Background(), conn5, entityID)
			require.NoError(t, err)
			if current != nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		require.NotNil(t, current)
		require.Equal(t, "1.1.1.1", current["ip"]) // Check natural key column
	})
}
