package sol

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func stringPtr(s string) *string {
	return &s
}

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

func TestMCP_Solana_View_Ready(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not ready", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		require.False(t, view.Ready(), "view should not be ready before first refresh")
	})

	t.Run("returns true after successful refresh", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		require.True(t, view.Ready(), "view should be ready after successful refresh")
	})
}

func TestMCP_Solana_View_WaitReady(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when already ready", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
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

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             &mockSolanaRPC{},
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = view.WaitReady(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "context cancelled")
	})
}

func TestMCP_Solana_View_Refresh_SavesToDB(t *testing.T) {
	t.Parallel()

	t.Run("saves leader schedule to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		slots := []uint64{100, 200, 300}

		mockRPC := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getLeaderScheduleFunc: func(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
				return solanarpc.GetLeaderScheduleResult{
					nodePK: slots,
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             mockRPC,
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_leader_schedule").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var nodePubkey string
		var slotCount int
		var slotsArray []interface{}
		var currentEpoch int64
		err = db.QueryRow("SELECT node_pubkey, slots, slot_count, current_epoch FROM solana_leader_schedule LIMIT 1").Scan(&nodePubkey, &slotsArray, &slotCount, &currentEpoch)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), nodePubkey)
		require.Equal(t, 3, slotCount)
		require.Equal(t, int64(100), currentEpoch)
		require.Len(t, slotsArray, 3)
		// DuckDB returns integers in arrays as int32
		require.Equal(t, int32(100), slotsArray[0])
		require.Equal(t, int32(200), slotsArray[1])
		require.Equal(t, int32(300), slotsArray[2])
	})

	t.Run("saves vote accounts to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		votePK := solana.MustPublicKeyFromBase58("Vote111111111111111111111111111111111111111")
		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

		mockRPC := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getVoteAccountsFunc: func(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
				return &solanarpc.GetVoteAccountsResult{
					Current: []solanarpc.VoteAccountsResult{
						{
							VotePubkey:       votePK,
							NodePubkey:       nodePK,
							ActivatedStake:   1000000000,
							EpochVoteAccount: true,
							Commission:       5,
							LastVote:         5000,
							RootSlot:         4500,
						},
					},
					Delinquent: []solanarpc.VoteAccountsResult{},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             mockRPC,
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_vote_accounts").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var votePubkey, nodePubkey string
		var activatedStake, lastVoteSlot, rootSlot int64
		var epochVoteAccount bool
		var commission int
		var currentEpoch int64
		err = db.QueryRow("SELECT vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage, last_vote_slot, root_slot, current_epoch FROM solana_vote_accounts LIMIT 1").Scan(&votePubkey, &nodePubkey, &activatedStake, &epochVoteAccount, &commission, &lastVoteSlot, &rootSlot, &currentEpoch)
		require.NoError(t, err)
		require.Equal(t, votePK.String(), votePubkey)
		require.Equal(t, nodePK.String(), nodePubkey)
		require.Equal(t, int64(1000000000), activatedStake)
		require.True(t, epochVoteAccount)
		require.Equal(t, 5, commission)
		require.Equal(t, int64(5000), lastVoteSlot)
		require.Equal(t, int64(4500), rootSlot)
		require.Equal(t, int64(100), currentEpoch)
	})

	t.Run("saves gossip nodes to database", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		gossipAddr := "192.168.1.1:8001"
		tpuQUICAddr := "192.168.1.1:8002"

		mockRPC := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getClusterNodesFunc: func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
				return []*solanarpc.GetClusterNodesResult{
					{
						Pubkey:  nodePK,
						Gossip:  &gossipAddr,
						TPUQUIC: &tpuQUICAddr,
						Version: stringPtr("1.18.0"),
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             mockRPC,
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_gossip_nodes").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pubkey, gossipIP, tpuQUICIP, version string
		var gossipPort, tpuQUICPort int
		var currentEpoch int64
		err = db.QueryRow("SELECT pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version, current_epoch FROM solana_gossip_nodes LIMIT 1").Scan(&pubkey, &gossipIP, &gossipPort, &tpuQUICIP, &tpuQUICPort, &version, &currentEpoch)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), pubkey)
		require.Equal(t, "192.168.1.1", gossipIP)
		require.Equal(t, 8001, gossipPort)
		require.Equal(t, "192.168.1.1", tpuQUICIP)
		require.Equal(t, 8002, tpuQUICPort)
		require.Equal(t, "1.18.0", version)
		require.Equal(t, int64(100), currentEpoch)
	})

	t.Run("handles missing gossip and tpuquic addresses", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		nodePK := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

		mockRPC := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getClusterNodesFunc: func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
				return []*solanarpc.GetClusterNodesResult{
					{
						Pubkey:  nodePK,
						Gossip:  nil,
						TPUQUIC: nil,
						Version: stringPtr("1.18.0"),
					},
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             mockRPC,
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_gossip_nodes").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var pubkey, gossipIP, tpuQUICIP string
		var gossipPort, tpuQUICPort int
		var version sql.NullString
		err = db.QueryRow("SELECT pubkey, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version FROM solana_gossip_nodes LIMIT 1").Scan(&pubkey, &gossipIP, &gossipPort, &tpuQUICIP, &tpuQUICPort, &version)
		require.NoError(t, err)
		require.Equal(t, nodePK.String(), pubkey)
		require.Empty(t, gossipIP, "gossipIP should be empty when Gossip is nil")
		require.Equal(t, 0, gossipPort, "gossipPort should be 0 when Gossip is nil")
		require.Empty(t, tpuQUICIP, "tpuQUICIP should be empty when TPUQUIC is nil")
		require.Equal(t, 0, tpuQUICPort, "tpuQUICPort should be 0 when TPUQUIC is nil")
		// Version is set in the test data, so it should be valid
		require.True(t, version.Valid, "version should be valid when set")
		require.Equal(t, "1.18.0", version.String)
	})

	t.Run("replaces existing data on refresh", func(t *testing.T) {
		t.Parallel()

		db, err := sql.Open("duckdb", "")
		require.NoError(t, err)
		defer db.Close()

		nodePK1 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
		nodePK2 := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111113")
		slots1 := []uint64{100, 200}
		slots2 := []uint64{300, 400}

		callCount := 0
		mockRPC := &mockSolanaRPC{
			getEpochInfoFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					Epoch: 100,
				}, nil
			},
			getLeaderScheduleFunc: func(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
				callCount++
				if callCount == 1 {
					return solanarpc.GetLeaderScheduleResult{
						nodePK1: slots1,
					}, nil
				}
				return solanarpc.GetLeaderScheduleResult{
					nodePK2: slots2,
				}, nil
			},
		}

		view, err := NewView(ViewConfig{
			Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
			Clock:           clockwork.NewFakeClock(),
			RPC:             mockRPC,
			RefreshInterval: time.Second,
			DB:              db,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = view.Refresh(ctx)
		require.NoError(t, err)

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM solana_leader_schedule").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var nodePubkey string
		err = db.QueryRow("SELECT node_pubkey FROM solana_leader_schedule LIMIT 1").Scan(&nodePubkey)
		require.NoError(t, err)
		require.Equal(t, nodePK1.String(), nodePubkey)

		err = view.Refresh(ctx)
		require.NoError(t, err)

		err = db.QueryRow("SELECT COUNT(*) FROM solana_leader_schedule").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		err = db.QueryRow("SELECT node_pubkey FROM solana_leader_schedule LIMIT 1").Scan(&nodePubkey)
		require.NoError(t, err)
		require.Equal(t, nodePK2.String(), nodePubkey)
	})
}

