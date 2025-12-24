package sol

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
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


