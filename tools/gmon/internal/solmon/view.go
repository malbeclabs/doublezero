package solmon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type SolanaRPC interface {
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

type ValidatorView struct {
	// TODO(snormore): Rename to PK
	Pubkey solana.PublicKey

	Node        *solanarpc.GetClusterNodesResult
	VoteAccount *solanarpc.VoteAccountsResult
	LeaderSlots []uint64
}

// ValidatorsView is the shared Solana RPC-backed index of validators.
// It does *all* the Solana RPC work and is intended to be reused by
// multiple target sources (public internet, DoubleZero, etc).
type ValidatorsView struct {
	log *slog.Logger
	rpc SolanaRPC

	refreshInterval time.Duration

	ready chan struct{}
	once  sync.Once

	mu   sync.Mutex
	data map[solana.PublicKey]*ValidatorView
}

func NewValidatorsView(log *slog.Logger, rpc SolanaRPC, refreshInterval time.Duration) (*ValidatorsView, error) {
	if refreshInterval <= 0 {
		return nil, fmt.Errorf("refresh interval must be greater than 0")
	}
	return &ValidatorsView{
		log:             log,
		rpc:             rpc,
		refreshInterval: refreshInterval,

		ready: make(chan struct{}),
		data:  make(map[solana.PublicKey]*ValidatorView),
	}, nil
}

// Ready is closed after the first successful Refresh.
func (v *ValidatorsView) Ready() <-chan struct{} { return v.ready }

// All returns a snapshot copy of the current validators map.
func (v *ValidatorsView) All() map[solana.PublicKey]*ValidatorView {
	v.mu.Lock()
	defer v.mu.Unlock()

	out := make(map[solana.PublicKey]*ValidatorView, len(v.data))
	for pk, vv := range v.data {
		out[pk] = vv
	}
	return out
}

// Start runs Run in a goroutine and blocks until the first successful refresh.
func (v *ValidatorsView) Start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		if err := v.Run(ctx); err != nil {
			v.log.Error("solana validators view failed to run", "error", err)
			cancel()
		}
	}()
	<-v.Ready()
}

func (v *ValidatorsView) Run(ctx context.Context) error {
	v.log.Debug("solana validators view running", "refreshInterval", v.refreshInterval)

	ticker := time.NewTicker(v.refreshInterval)
	defer ticker.Stop()

	if err := v.refreshOnce(ctx); err != nil {
		v.log.Warn("failed to refresh validators view", "error", err)
	} else {
		v.once.Do(func() { close(v.ready) })
	}

	for {
		select {
		case <-ctx.Done():
			v.log.Debug("solana validators view done, stopping", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			if err := v.refreshOnce(ctx); err != nil {
				v.log.Warn("failed to refresh validators view", "error", err)
			}
			v.once.Do(func() { close(v.ready) })
		}
	}
}

func (v *ValidatorsView) refreshOnce(ctx context.Context) error {
	v.log.Debug("refreshing solana validators view", "currentCount", len(v.data))

	next := make(map[solana.PublicKey]*ValidatorView)

	leaderSchedule, err := v.rpc.GetLeaderSchedule(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leader schedule: %w", err)
	}

	nodes, err := v.rpc.GetClusterNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	nodesByPK := make(map[solana.PublicKey]*solanarpc.GetClusterNodesResult, len(nodes))
	for _, node := range nodes {
		nodesByPK[node.Pubkey] = node
	}

	voteAccountsRes, err := v.rpc.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentConfirmed,
	})
	if err != nil {
		return fmt.Errorf("failed to get vote accounts: %w", err)
	}

	for _, va := range voteAccountsRes.Current {
		node, ok := nodesByPK[va.NodePubkey]
		if !ok {
			continue
		}
		next[va.NodePubkey] = &ValidatorView{
			Pubkey:      va.NodePubkey,
			Node:        node,
			VoteAccount: &va,
			LeaderSlots: leaderSchedule[va.NodePubkey],
		}
	}

	v.mu.Lock()
	v.data = next
	v.mu.Unlock()

	return nil
}
