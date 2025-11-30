package solana

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type ValidatorsView struct {
	log *slog.Logger
	rpc SolanaRPC

	refreshInterval time.Duration

	ready chan struct{}
	once  sync.Once

	data map[solana.PublicKey]*ValidatorView
	mu   sync.Mutex
}

type ValidatorView struct {
	Pubkey solana.PublicKey

	Node        *solanarpc.GetClusterNodesResult
	VoteAccount *solanarpc.VoteAccountsResult
	LeaderSlots []uint64
}

type SolanaRPC interface {
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
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
		mu:    sync.Mutex{},
	}, nil
}

func (v *ValidatorsView) All() map[solana.PublicKey]*ValidatorView {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.data
}

func (v *ValidatorsView) Filtered(filter func(validator *ValidatorView) bool) map[solana.PublicKey]*ValidatorView {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[solana.PublicKey]*ValidatorView)
	for pk, validator := range v.data {
		if filter(validator) {
			out[pk] = validator
		}
	}
	return out
}

func (v *ValidatorsView) Ready() <-chan struct{} {
	return v.ready
}

func (v *ValidatorsView) Start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		err := v.Run(ctx)
		if err != nil {
			v.log.Error("solana validators view failed to run", "error", err)
			cancel()
		}
	}()

	// Wait for validators view to be ready (at least one refresh)
	<-v.Ready()
}

func (v *ValidatorsView) Run(ctx context.Context) error {
	v.log.Debug("solana validators view running", "refreshInterval", v.refreshInterval.String())

	ticker := time.NewTicker(v.refreshInterval)
	defer ticker.Stop()

	// Refresh immediately before entering the loop.
	err := v.Refresh(ctx)
	if err != nil {
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
			err := v.Refresh(ctx)
			if err != nil {
				v.log.Warn("failed to refresh validators view", "error", err)
				// TODO: maybe track consecutive failures and bail?
			}

			// In case first successful Refresh happens later, close the ready channel.
			v.once.Do(func() { close(v.ready) })
		}
	}
}

func (v *ValidatorsView) Refresh(ctx context.Context) error {
	v.log.Debug("refreshing solana validators view", "currentCount", len(v.data))

	validators := make(map[solana.PublicKey]*ValidatorView)

	// Get leader schedule.
	leaderSchedule, err := v.rpc.GetLeaderSchedule(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leader schedule: %w", err)
	}
	leaderPKs := make(map[solana.PublicKey]struct{})
	for pk := range leaderSchedule {
		leaderPKs[pk] = struct{}{}
	}

	// Get cluster/gossip nodes.
	nodes, err := v.rpc.GetClusterNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}
	nodesByPK := make(map[solana.PublicKey]*solanarpc.GetClusterNodesResult)
	for _, node := range nodes {
		nodesByPK[node.Pubkey] = node
	}

	// Get validators / vote accounts.
	voteAccountsRes, err := v.rpc.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentConfirmed,
	})
	if err != nil {
		return fmt.Errorf("failed to get vote accounts: %w", err)
	}
	for _, voteAccount := range voteAccountsRes.Current {
		node, ok := nodesByPK[voteAccount.NodePubkey]
		if !ok {
			continue
		}
		validators[voteAccount.NodePubkey] = &ValidatorView{
			Pubkey:      voteAccount.NodePubkey,
			Node:        node,
			VoteAccount: &voteAccount,
			LeaderSlots: leaderSchedule[voteAccount.NodePubkey],
		}
	}

	v.mu.Lock()
	v.data = validators
	v.mu.Unlock()

	return nil
}
