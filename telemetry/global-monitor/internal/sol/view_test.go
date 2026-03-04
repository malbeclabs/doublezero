package sol

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_Solana_ValidatorsView_New_NilArgs(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	rpc := &MockSolanaRPC{}
	geo := &MockGeoIPResolver{}

	_, err := NewSolanaView(nil, rpc, geo)
	require.Error(t, err)

	_, err = NewSolanaView(logger, nil, geo)
	require.Error(t, err)

	_, err = NewSolanaView(logger, rpc, nil)
	require.Error(t, err)
}

func TestGlobalMonitor_Solana_ValidatorsView_New_Success(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	rpc := &MockSolanaRPC{}
	geo := &MockGeoIPResolver{}

	v, err := NewSolanaView(logger, rpc, geo)
	require.NoError(t, err)
	require.NotNil(t, v)

	require.Equal(t, logger, v.log)
	require.Equal(t, rpc, v.rpc)
	require.Equal(t, geo, v.geoIP)
}

func TestGlobalMonitor_Solana_ValidatorsView_GetByNodePubkey_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	nodeWallet := solana.NewWallet()
	nodePK := nodeWallet.PublicKey()
	otherPK := solana.NewWallet().PublicKey()
	votePK := solana.NewWallet().PublicKey()

	leaderSchedule := solanarpc.GetLeaderScheduleResult{
		nodePK:  {1, 2, 3, 4, 5},
		otherPK: {6, 7, 8, 9, 10},
	}

	gossip := "1.2.3.4:8001"
	tpuAddr := "1.2.3.4:9000"
	clusterNodes := []*solanarpc.GetClusterNodesResult{
		{Pubkey: nodePK, Gossip: &gossip, TPUQUIC: &tpuAddr},
	}

	voteAccounts := &solanarpc.GetVoteAccountsResult{
		Current: []solanarpc.VoteAccountsResult{
			{
				VotePubkey:     votePK,
				NodePubkey:     nodePK,
				ActivatedStake: 555,
			},
		},
	}

	var resolvedIP net.IP

	rpc := &MockSolanaRPC{
		GetLeaderScheduleFunc: func(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return leaderSchedule, nil
		},
		GetClusterNodesFunc: func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
			return clusterNodes, nil
		},
		GetVoteAccountsFunc: func(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
			return voteAccounts, nil
		},
	}

	geo := &MockGeoIPResolver{
		ResolveFunc: func(ip net.IP) *geoip.Record {
			resolvedIP = ip
			return nil
		},
	}

	view, err := NewSolanaView(logger, rpc, geo)
	require.NoError(t, err)

	vals, err := view.GetValidatorsByNodePubkey(ctx)
	require.NoError(t, err)
	require.Len(t, vals, 1)

	v := vals[nodePK]
	require.Equal(t, votePK, v.VoteAccount.VotePubkey)
	require.Equal(t, uint64(555), v.VoteAccount.ActivatedStake)

	require.NotNil(t, v.Node.GossipIP)
	require.Equal(t, "1.2.3.4", v.Node.GossipIP.String())
	require.Equal(t, uint16(8001), v.Node.GossipPort)

	require.NotNil(t, v.Node.TPUQUICIP)
	require.Equal(t, "1.2.3.4", v.Node.TPUQUICIP.String())
	require.Equal(t, uint16(9000), v.Node.TPUQUICPort)

	require.InDelta(t, 0.5, v.LeaderRatio, 1e-9)

	require.NotNil(t, resolvedIP)
	require.Equal(t, v.Node.GossipIP.String(), resolvedIP.String())
	require.Nil(t, v.GeoIP)
}

func TestGlobalMonitor_Solana_ValidatorsView_LeaderScheduleError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()
	exp := errors.New("boom")

	rpc := &MockSolanaRPC{
		GetLeaderScheduleFunc: func(context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return nil, exp
		},
	}

	view, _ := NewSolanaView(logger, rpc, &MockGeoIPResolver{})
	_, err := view.GetValidatorsByNodePubkey(ctx)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to get leader schedule")
	require.ErrorIs(t, err, exp)
}

func TestGlobalMonitor_Solana_ValidatorsView_ClusterNodesError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()
	exp := errors.New("boom")

	rpc := &MockSolanaRPC{
		GetLeaderScheduleFunc: func(context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return solanarpc.GetLeaderScheduleResult{}, nil
		},
		GetClusterNodesFunc: func(context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
			return nil, exp
		},
	}

	view, _ := NewSolanaView(logger, rpc, &MockGeoIPResolver{})
	_, err := view.GetValidatorsByNodePubkey(ctx)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to get cluster nodes")
	require.ErrorIs(t, err, exp)
}

func TestGlobalMonitor_Solana_ValidatorsView_VoteAccountsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()
	exp := errors.New("boom")

	rpc := &MockSolanaRPC{
		GetLeaderScheduleFunc: func(context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return solanarpc.GetLeaderScheduleResult{}, nil
		},
		GetClusterNodesFunc: func(context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
			return []*solanarpc.GetClusterNodesResult{}, nil
		},
		GetVoteAccountsFunc: func(context.Context, *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
			return nil, exp
		},
	}

	view, _ := NewSolanaView(logger, rpc, &MockGeoIPResolver{})
	_, err := view.GetValidatorsByNodePubkey(ctx)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to get vote accounts")
	require.ErrorIs(t, err, exp)
}

func TestGlobalMonitor_Solana_ValidatorsView_NilVoteAccountsResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := newTestLogger()

	rpc := &MockSolanaRPC{
		GetLeaderScheduleFunc: func(context.Context) (solanarpc.GetLeaderScheduleResult, error) {
			return solanarpc.GetLeaderScheduleResult{}, nil
		},
		GetClusterNodesFunc: func(context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
			return []*solanarpc.GetClusterNodesResult{}, nil
		},
		GetVoteAccountsFunc: func(context.Context, *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
			return nil, nil
		},
	}

	view, _ := NewSolanaView(logger, rpc, &MockGeoIPResolver{})
	_, err := view.GetValidatorsByNodePubkey(ctx)

	require.Error(t, err)
	require.ErrorContains(t, err, "vote accounts response is nil")
}

func TestGlobalMonitor_Solana_ValidatorsView_gossipNodeFromRPC_ParsesIPv4AndPort(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	view, _ := NewSolanaView(logger, &MockSolanaRPC{}, &MockGeoIPResolver{})

	nodePK := solana.NewWallet().PublicKey()
	addr := "5.6.7.8:12345"

	res := &solanarpc.GetClusterNodesResult{
		Pubkey:  nodePK,
		TPUQUIC: &addr,
	}

	g := view.gossipNodeFromRPC(res)

	require.Equal(t, nodePK, g.Pubkey)
	require.Nil(t, g.GossipIP)
	require.NotNil(t, g.TPUQUICIP)
	require.Equal(t, "5.6.7.8", g.TPUQUICIP.String())
	require.Equal(t, uint16(12345), g.TPUQUICPort)

	s, ok := g.TPUQUICAddr()
	require.True(t, ok)
	require.Equal(t, "5.6.7.8:12345", s)
}

func TestGlobalMonitor_Solana_ValidatorsView_gossipNodeFromRPC_InvalidTPUQUIC(t *testing.T) {
	t.Parallel()

	logger := newTestLogger()
	view, _ := NewSolanaView(logger, &MockSolanaRPC{}, &MockGeoIPResolver{})

	nodePK := solana.NewWallet().PublicKey()
	bad := "not-a-hostport"

	res := &solanarpc.GetClusterNodesResult{
		Pubkey:  nodePK,
		TPUQUIC: &bad,
	}

	g := view.gossipNodeFromRPC(res)
	require.Equal(t, nodePK, g.Pubkey)
	require.Nil(t, g.GossipIP)
	require.Nil(t, g.TPUQUICIP)
	require.Equal(t, uint16(0), g.TPUQUICPort)

	s, ok := g.TPUQUICAddr()
	require.False(t, ok)
	require.Equal(t, "", s)
}

type MockSolanaRPC struct {
	GetLeaderScheduleFunc func(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodesFunc   func(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccountsFunc   func(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

func (m *MockSolanaRPC) GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error) {
	if m.GetLeaderScheduleFunc == nil {
		return nil, nil
	}
	return m.GetLeaderScheduleFunc(ctx)
}

func (m *MockSolanaRPC) GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error) {
	if m.GetClusterNodesFunc == nil {
		return nil, nil
	}
	return m.GetClusterNodesFunc(ctx)
}

func (m *MockSolanaRPC) GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error) {
	if m.GetVoteAccountsFunc == nil {
		return nil, nil
	}
	return m.GetVoteAccountsFunc(ctx, opts)
}

type MockGeoIPResolver struct {
	ResolveFunc func(ip net.IP) *geoip.Record
}

func (m *MockGeoIPResolver) Resolve(ip net.IP) *geoip.Record {
	if m.ResolveFunc == nil {
		return nil
	}
	return m.ResolveFunc(ip)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}
