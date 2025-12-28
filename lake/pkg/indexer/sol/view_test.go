package sol

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

type mockSolanaRPC struct {
	getEpochInfoFunc      func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	getLeaderScheduleFunc func(context.Context) (solanarpc.GetLeaderScheduleResult, error)
	getVoteAccountsFunc   func(context.Context, *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
	getClusterNodesFunc   func(context.Context) ([]*solanarpc.GetClusterNodesResult, error)
}

func (m *mockSolanaRPC) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	if m.getEpochInfoFunc != nil {
		return m.getEpochInfoFunc(ctx, commitment)
	}
	return &solanarpc.GetEpochInfoResult{
		Epoch: 100,
	}, nil
}

func (m *mockSolanaRPC) GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
	if m.getLeaderScheduleFunc != nil {
		return m.getLeaderScheduleFunc(ctx)
	}
	return solanarpc.GetLeaderScheduleResult{}, nil
}

func (m *mockSolanaRPC) GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
	if m.getVoteAccountsFunc != nil {
		return m.getVoteAccountsFunc(ctx, opts)
	}
	return &solanarpc.GetVoteAccountsResult{
		Current:    []solanarpc.VoteAccountsResult{},
		Delinquent: []solanarpc.VoteAccountsResult{},
	}, nil
}

func (m *mockSolanaRPC) GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
	if m.getClusterNodesFunc != nil {
		return m.getClusterNodesFunc(ctx)
	}
	return []*solanarpc.GetClusterNodesResult{}, nil
}

func testDB(t *testing.T) duck.DB {
	db, err := duck.NewDB(t.Context(), "", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
	})
	return db
}
func TestLake_Solana_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		require.True(t, view.Ready(), "view should be ready after successful refresh")
	})
}

func TestLake_Solana_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when already ready", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		err = view.WaitReady(ctx)
		require.NoError(t, err, "WaitReady should return immediately when already ready")
	})

	t.Run("returns error when context is cancelled", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   &mockGeoIPResolver{},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

type mockGeoIPResolver struct {
	resolveFunc func(net.IP) *geoip.Record
}

func (m *mockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	if m.resolveFunc != nil {
		return m.resolveFunc(ip)
	}
	return nil
}

type testGeoIPStore struct {
	store *mcpgeoip.Store
	db    duck.DB
}

func newTestGeoIPStore(t *testing.T) (*testGeoIPStore, error) {
	t.Helper()
	db := testDB(t)

	store, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:     db,
	})
	if err != nil {
		return nil, err
	}

	if err := store.CreateTablesIfNotExists(); err != nil {
		return nil, err
	}

	return &testGeoIPStore{
		store: store,
		db:    db,
	}, nil
}

func TestLake_Solana_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("stores all data on refresh", func(t *testing.T) {
		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		geoipResolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				if ip.String() == "1.1.1.1" {
					return &geoip.Record{
						IP:          ip,
						CountryCode: "US",
						Country:     "United States",
						City:        "San Francisco",
					}
				}
				if ip.String() == "8.8.8.8" {
					return &geoip.Record{
						IP:          ip,
						CountryCode: "US",
						Country:     "United States",
						City:        "Mountain View",
					}
				}
				return nil
			},
		}

		pk1 := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
		pk2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		pk3 := solana.MustPublicKeyFromBase58("SysvarC1ock11111111111111111111111111111111")
		rpc := &mockSolanaRPC{
			getClusterNodesFunc: func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
				return []*solanarpc.GetClusterNodesResult{
					{
						Pubkey: pk1,
						Gossip: stringPtr("1.1.1.1:8001"),
					},
					{
						Pubkey: pk2,
						Gossip: stringPtr("8.8.8.8:8001"),
					},
					{
						Pubkey: pk3,
						Gossip: nil, // Node without gossip address
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   geoipResolver,
		})
		require.NoError(t, err)

		// Set up leader schedule
		leaderPK1 := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
		leaderPK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		rpc.getLeaderScheduleFunc = func(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return solanarpc.GetLeaderScheduleResult{
				leaderPK1: []uint64{100, 101, 102},
				leaderPK2: []uint64{200, 201},
			}, nil
		}

		// Set up vote accounts
		votePK1 := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")
		votePK2 := solana.MustPublicKeyFromBase58("Vote222222222222222222222222222222222222222")
		rpc.getVoteAccountsFunc = func(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
			return &solanarpc.GetVoteAccountsResult{
				Current: []solanarpc.VoteAccountsResult{
					{
						VotePubkey:       votePK1,
						NodePubkey:       pk1,
						ActivatedStake:   1000000,
						EpochVoteAccount: true,
						Commission:       5,
						LastVote:         1000,
						RootSlot:         999,
					},
					{
						VotePubkey:       votePK2,
						NodePubkey:       pk2,
						ActivatedStake:   2000000,
						EpochVoteAccount: true,
						Commission:       10,
						LastVote:         2000,
						RootSlot:         1999,
					},
				},
				Delinquent: []solanarpc.VoteAccountsResult{},
			}, nil
		}

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify leader schedule was stored
		var leaderScheduleCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_leader_schedule").Scan(&leaderScheduleCount)
		require.NoError(t, err)
		require.Equal(t, 2, leaderScheduleCount, "should have 2 leader schedule entries")

		// Verify vote accounts were stored
		var voteAccountsCount int
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_vote_accounts").Scan(&voteAccountsCount)
		require.NoError(t, err)
		require.Equal(t, 2, voteAccountsCount, "should have 2 vote accounts")

		// Verify gossip nodes were stored
		var gossipNodesCount int
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_gossip_nodes").Scan(&gossipNodesCount)
		require.NoError(t, err)
		require.Equal(t, 3, gossipNodesCount, "should have 3 gossip nodes")

		// Verify geoip records were upserted
		records, err := geoipStore.store.GetRecords()
		require.NoError(t, err)
		require.Len(t, records, 2, "should have 2 resolved geoip records")
		// Find records by IP
		var record1, record2 *geoip.Record
		for _, r := range records {
			if r.IP.String() == "1.1.1.1" {
				record1 = r
			}
			if r.IP.String() == "8.8.8.8" {
				record2 = r
			}
		}
		require.NotNil(t, record1, "should have record for 1.1.1.1")
		require.Equal(t, "San Francisco", record1.City)
		require.NotNil(t, record2, "should have record for 8.8.8.8")
		require.Equal(t, "Mountain View", record2.City)

		// Verify specific data in leader schedule
		var slotCount int
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT slot_count FROM solana_leader_schedule WHERE node_pubkey = ?", leaderPK1.String()).Scan(&slotCount)
		require.NoError(t, err)
		require.Equal(t, 3, slotCount, "leaderPK1 should have 3 slots")

		// Verify specific data in vote accounts
		var activatedStake int64
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT activated_stake_lamports FROM solana_vote_accounts WHERE vote_pubkey = ?", votePK1.String()).Scan(&activatedStake)
		require.NoError(t, err)
		require.Equal(t, int64(1000000), activatedStake, "votePK1 should have correct stake")

		// Verify specific data in gossip nodes
		var gossipIP string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT gossip_ip FROM solana_gossip_nodes WHERE pubkey = ?", pk1.String()).Scan(&gossipIP)
		require.NoError(t, err)
		require.Equal(t, "1.1.1.1", gossipIP, "pk1 should have correct gossip IP")
	})

	t.Run("handles nodes without gossip addresses for geoip", func(t *testing.T) {

		t.Parallel()

		db := testDB(t)

		geoipStore, err := newTestGeoIPStore(t)
		require.NoError(t, err)
		defer geoipStore.db.Close()

		geoipResolver := &mockGeoIPResolver{
			resolveFunc: func(ip net.IP) *geoip.Record {
				return &geoip.Record{IP: ip}
			},
		}

		pk1 := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
		pk2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")
		rpc := &mockSolanaRPC{
			getClusterNodesFunc: func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
				return []*solanarpc.GetClusterNodesResult{
					{
						Pubkey: pk1,
						Gossip: nil,
					},
					{
						Pubkey: pk2,
						Gossip: nil,
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			DB:              db,
			GeoIPStore:      *geoipStore.store,
			GeoIPResolver:   geoipResolver,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify gossip nodes are still stored even without geoip
		var gossipNodesCount int
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM solana_gossip_nodes").Scan(&gossipNodesCount)
		require.NoError(t, err)
		require.Equal(t, 2, gossipNodesCount, "should have 2 gossip nodes even without geoip")

		// Verify no geoip records were upserted
		records, err := geoipStore.store.GetRecords()
		require.NoError(t, err)
		require.Len(t, records, 0, "should have no geoip records when no gossip addresses")
	})
}

func stringPtr(s string) *string {
	return &s
}
