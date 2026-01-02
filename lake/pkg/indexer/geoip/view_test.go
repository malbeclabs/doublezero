package geoip

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/sol"
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

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		t.Run("missing logger", func(t *testing.T) {
			t.Parallel()
			view, err := NewView(ViewConfig{
				DB:                db,
				GeoIPStore:        geoipStore,
				GeoIPResolver:     &mockGeoIPResolver{},
				ServiceabilityStore: svcStore,
				SolanaStore:       solStore,
				RefreshInterval:   time.Second,
			})
			require.Error(t, err)
			require.Nil(t, view)
			require.Contains(t, err.Error(), "logger is required")
		})

		t.Run("missing serviceability store", func(t *testing.T) {
			t.Parallel()
			view, err := NewView(ViewConfig{
				Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
				DB:              db,
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
				Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
				DB:                db,
				GeoIPStore:        geoipStore,
				GeoIPResolver:     &mockGeoIPResolver{},
				ServiceabilityStore: svcStore,
				RefreshInterval:   time.Second,
			})
			require.Error(t, err)
			require.Nil(t, view)
			require.Contains(t, err.Error(), "solana store is required")
		})
	})

	t.Run("returns view when config is valid", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			DB:                 db,
			GeoIPStore:         geoipStore,
			GeoIPResolver:      &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:        solStore,
			RefreshInterval:    time.Second,
		})
		require.NoError(t, err)
		require.NotNil(t, view)
	})
}

func TestLake_GeoIP_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			DB:                 db,
			GeoIPStore:         geoipStore,
			GeoIPResolver:      &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:        solStore,
			RefreshInterval:    time.Second,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})
}

func TestLake_GeoIP_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("resolves and saves geoip records from users and gossip nodes", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		// Set up test data: users with client IPs
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create users table
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_users_current (
			pk VARCHAR,
			owner_pk VARCHAR,
			status VARCHAR,
			kind VARCHAR,
			client_ip VARCHAR,
			dz_ip VARCHAR,
			device_pk VARCHAR,
			tunnel_id INTEGER,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		userPK1 := testPK(1)
		userPK2 := testPK(2)
		ownerPK := testPK(3)
		devicePK := testPK(4)

		_, err = conn.ExecContext(ctx, `INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'hash1'), (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'hash2')`,
			userPK1, ownerPK, "activated", "IBRL", "1.1.1.1", "10.0.0.1", devicePK, 1,
			userPK2, ownerPK, "activated", "IBRL", "8.8.8.8", "10.0.0.2", devicePK, 2)
		require.NoError(t, err)

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
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			DB:                 db,
			GeoIPStore:         geoipStore,
			GeoIPResolver:      resolver,
			ServiceabilityStore: svcStore,
			SolanaStore:        solStore,
			RefreshInterval:    time.Second,
		})
		require.NoError(t, err)

		// Refresh the view
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify geoip records were saved
		records, err := geoipStore.GetRecords()
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(records), 4) // At least 4 unique IPs (2 from users, 2 from gossip nodes)

		// Check that all expected IPs are present
		ipSet := make(map[string]bool)
		for _, record := range records {
			if record.IP != nil {
				ipSet[record.IP.String()] = true
			}
		}
		require.Contains(t, ipSet, "1.1.1.1")
		require.Contains(t, ipSet, "8.8.8.8")
		require.Contains(t, ipSet, "192.168.1.1")
		require.Contains(t, ipSet, "192.168.1.2")
	})

	t.Run("handles empty stores gracefully", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		// Create empty tables so queries don't fail
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create empty users table
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_users_current (
			pk VARCHAR,
			owner_pk VARCHAR,
			status VARCHAR,
			kind VARCHAR,
			client_ip VARCHAR,
			dz_ip VARCHAR,
			device_pk VARCHAR,
			tunnel_id INTEGER,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		// Create empty gossip nodes table
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey VARCHAR,
			epoch BIGINT,
			gossip_ip VARCHAR,
			gossip_port INTEGER,
			tpuquic_ip VARCHAR,
			tpuquic_port INTEGER,
			version VARCHAR,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		view, err := NewView(ViewConfig{
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			DB:                 db,
			GeoIPStore:         geoipStore,
			GeoIPResolver:      &mockGeoIPResolver{},
			ServiceabilityStore: svcStore,
			SolanaStore:        solStore,
			RefreshInterval:    time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err) // Should succeed even with no data

		require.True(t, view.Ready())
	})

	t.Run("deduplicates IPs from multiple sources", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)
		geoipStore, err := NewStore(StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		svcStore, err := dzsvc.NewStore(dzsvc.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		solStore, err := sol.NewStore(sol.StoreConfig{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
			DB:     db,
		})
		require.NoError(t, err)

		// Set up test data with same IP in both users and gossip nodes
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Create users table
		_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dz_users_current (
			pk VARCHAR,
			owner_pk VARCHAR,
			status VARCHAR,
			kind VARCHAR,
			client_ip VARCHAR,
			dz_ip VARCHAR,
			device_pk VARCHAR,
			tunnel_id INTEGER,
			as_of_ts TIMESTAMP NOT NULL,
			row_hash VARCHAR NOT NULL
		)`)
		require.NoError(t, err)

		userPK := testPK(1)
		ownerPK := testPK(2)
		devicePK := testPK(3)

		_, err = conn.ExecContext(ctx, `INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'hash1')`,
			userPK, ownerPK, "activated", "IBRL", "1.1.1.1", "10.0.0.1", devicePK, 1)
		require.NoError(t, err)

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
			Logger:            slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:             clockwork.NewFakeClock(),
			DB:                 db,
			GeoIPStore:         geoipStore,
			GeoIPResolver:      resolver,
			ServiceabilityStore: svcStore,
			SolanaStore:        solStore,
			RefreshInterval:    time.Second,
		})
		require.NoError(t, err)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Should only have one record for 1.1.1.1 (deduplicated)
		records, err := geoipStore.GetRecords()
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, net.ParseIP("1.1.1.1"), records[0].IP)
	})
}

