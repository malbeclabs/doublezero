package sol

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

type mockSolanaRPC struct {
	getEpochInfoFunc       func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	getLeaderScheduleFunc  func(context.Context) (solanarpc.GetLeaderScheduleResult, error)
	getVoteAccountsFunc    func(context.Context, *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
	getClusterNodesFunc    func(context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	getSlotFunc            func(context.Context, solanarpc.CommitmentType) (uint64, error)
	getBlockProductionFunc func(context.Context) (*solanarpc.GetBlockProductionResult, error)
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

func (m *mockSolanaRPC) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	if m.getSlotFunc != nil {
		return m.getSlotFunc(ctx, commitment)
	}
	return 1000, nil
}

func (m *mockSolanaRPC) GetBlockProduction(ctx context.Context) (*solanarpc.GetBlockProductionResult, error) {
	if m.getBlockProductionFunc != nil {
		return m.getBlockProductionFunc(ctx)
	}
	return &solanarpc.GetBlockProductionResult{
		Value: solanarpc.BlockProductionResult{
			ByIdentity: make(solanarpc.IdentityToSlotsBlocks),
		},
	}, nil
}

func TestLake_Solana_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			ClickHouse:      db,
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

		db := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			ClickHouse:      db,
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

		db := testClient(t)

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestLake_Solana_View_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("stores all data on refresh", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

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
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
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

		// Verify data was inserted by querying the database
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Verify vote accounts were stored
		rows, err := conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_vote_accounts_history
			)
			SELECT count() FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var voteAccountsCount uint64
		require.NoError(t, rows.Scan(&voteAccountsCount))
		rows.Close()
		require.Equal(t, uint64(2), voteAccountsCount, "should have 2 vote accounts")

		// Verify gossip nodes were stored
		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_gossip_nodes_history
			)
			SELECT count() FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		var gossipNodesCount uint64
		require.NoError(t, rows.Scan(&gossipNodesCount))
		rows.Close()
		require.Equal(t, uint64(3), gossipNodesCount, "should have 3 gossip nodes")

		// Note: GeoIP records are now handled by the geoip view, not the solana view

		// Verify specific data in leader schedule
		var slotCount int64
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_leader_schedule_history
			)
			SELECT slot_count FROM ranked WHERE rn = 1 AND is_deleted = 0 AND node_pubkey = ?
		`, leaderPK1.String())
		require.NoError(t, err)
		require.True(t, rows.Next())
		err = rows.Scan(&slotCount)
		require.NoError(t, err)
		require.Equal(t, int64(3), slotCount, "leaderPK1 should have 3 slots")

		// Verify specific data in vote accounts
		var activatedStake int64
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_vote_accounts_history
			)
			SELECT activated_stake_lamports FROM ranked WHERE rn = 1 AND is_deleted = 0 AND vote_pubkey = ?
		`, votePK1.String())
		require.NoError(t, err)
		require.True(t, rows.Next())
		err = rows.Scan(&activatedStake)
		require.NoError(t, err)
		require.Equal(t, int64(1000000), activatedStake, "votePK1 should have correct stake")

		// Verify specific data in gossip nodes
		var gossipIP string
		conn, err = db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		rows, err = conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_gossip_nodes_history
			)
			SELECT gossip_ip FROM ranked WHERE rn = 1 AND is_deleted = 0 AND pubkey = ?
		`, pk1.String())
		require.NoError(t, err)
		require.True(t, rows.Next())
		err = rows.Scan(&gossipIP)
		require.NoError(t, err)
		require.Equal(t, "1.1.1.1", gossipIP, "pk1 should have correct gossip IP")
	})

	t.Run("handles nodes without gossip addresses", func(t *testing.T) {

		t.Parallel()

		db := testClient(t)

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
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		// Verify gossip nodes are still stored
		var gossipNodesCount uint64
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		rows, err := conn.Query(ctx, `
			WITH ranked AS (
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM dim_solana_gossip_nodes_history
			)
			SELECT count() FROM ranked WHERE rn = 1 AND is_deleted = 0
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&gossipNodesCount))
		rows.Close()
		require.Equal(t, uint64(2), gossipNodesCount, "should have 2 gossip nodes")
		// Note: GeoIP records are now handled by the geoip view, not the solana view
	})
}

func stringPtr(s string) *string {
	return &s
}

func TestLake_Solana_View_RefreshBlockProduction(t *testing.T) {
	t.Parallel()

	t.Run("stores block production data", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		leaderPK1 := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
		leaderPK2 := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")

		rpc := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getBlockProductionFunc: func(ctx context.Context) (*solanarpc.GetBlockProductionResult, error) {
				return &solanarpc.GetBlockProductionResult{
					Value: solanarpc.BlockProductionResult{
						ByIdentity: solanarpc.IdentityToSlotsBlocks{
							leaderPK1: {100, 95},  // 100 slots assigned, 95 blocks produced
							leaderPK2: {200, 198}, // 200 slots assigned, 198 blocks produced
						},
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.RefreshBlockProduction(ctx)
		require.NoError(t, err)

		// Verify block production was stored
		var count uint64
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		rows, err := conn.Query(ctx, "SELECT COUNT(*) FROM fact_solana_block_production")
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&count))
		rows.Close()
		require.Equal(t, uint64(2), count, "should have 2 block production entries")

		// Verify specific data
		var epoch int32
		var leaderIdentityPubkey string
		var leaderSlotsAssigned, blocksProduced int64
		rows, err = conn.Query(ctx, "SELECT epoch, leader_identity_pubkey, leader_slots_assigned_cum, blocks_produced_cum FROM fact_solana_block_production WHERE leader_identity_pubkey = ?", leaderPK1.String())
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&epoch, &leaderIdentityPubkey, &leaderSlotsAssigned, &blocksProduced))
		rows.Close()
		require.NoError(t, err)
		require.Equal(t, int32(100), epoch)
		require.Equal(t, leaderPK1.String(), leaderIdentityPubkey)
		require.Equal(t, int64(100), leaderSlotsAssigned)
		require.Equal(t, int64(95), blocksProduced)
	})

	t.Run("handles empty block production response", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		rpc := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getBlockProductionFunc: func(ctx context.Context) (*solanarpc.GetBlockProductionResult, error) {
				return &solanarpc.GetBlockProductionResult{
					Value: solanarpc.BlockProductionResult{
						ByIdentity: nil,
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.RefreshBlockProduction(ctx)
		require.NoError(t, err)

		// Verify no data was stored (table may not exist if no data was inserted)
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Check if table exists before querying
		var tableExists bool
		var rows driver.Rows
		rows, err = conn.Query(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM system.tables
				WHERE database = currentDatabase()
				AND name = 'fact_solana_block_production'
			)
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&tableExists))
		rows.Close()
		if err == nil && tableExists {
			var count uint64
			rows, err = conn.Query(ctx, "SELECT COUNT(*) FROM fact_solana_block_production")
			require.NoError(t, err)
			require.True(t, rows.Next())
			err = rows.Scan(&count)
			rows.Close()
			require.NoError(t, err)
			require.Equal(t, uint64(0), count, "should have no block production entries")
		}
		// If table doesn't exist, that's also fine - no data means no table creation
	})

	t.Run("handles invalid production data", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		rpc := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getBlockProductionFunc: func(ctx context.Context) (*solanarpc.GetBlockProductionResult, error) {
				// Create a map with no data (empty map)
				// This will result in no entries being inserted
				return &solanarpc.GetBlockProductionResult{
					Value: solanarpc.BlockProductionResult{
						ByIdentity: make(solanarpc.IdentityToSlotsBlocks),
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.RefreshBlockProduction(ctx)
		require.NoError(t, err)

		// Verify no data was stored (table may not exist if no data was inserted)
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()

		// Check if table exists before querying
		var tableExists bool
		var rows driver.Rows
		rows, err = conn.Query(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM system.tables
				WHERE database = currentDatabase()
				AND name = 'fact_solana_block_production'
			)
		`)
		require.NoError(t, err)
		require.True(t, rows.Next())
		require.NoError(t, rows.Scan(&tableExists))
		rows.Close()
		if err == nil && tableExists {
			var count uint64
			rows, err = conn.Query(ctx, "SELECT COUNT(*) FROM fact_solana_block_production")
			require.NoError(t, err)
			require.True(t, rows.Next())
			err = rows.Scan(&count)
			rows.Close()
			require.NoError(t, err)
			require.Equal(t, uint64(0), count, "should have no block production entries")
		}
		// If table doesn't exist, that's also fine - no data means no table creation
	})

	t.Run("handles RPC errors", func(t *testing.T) {
		t.Parallel()

		db := testClient(t)

		rpc := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return nil, fmt.Errorf("RPC error")
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          laketesting.NewLogger(),
			Clock:           clockwork.NewFakeClock(),
			RPC:             rpc,
			RefreshInterval: time.Second,
			ClickHouse:      db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.RefreshBlockProduction(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get epoch info")
	})
}
