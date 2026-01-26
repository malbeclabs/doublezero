package sol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
)

type SolanaRPC interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
	GetBlockProduction(ctx context.Context) (*solanarpc.GetBlockProductionResult, error)
}

type ViewConfig struct {
	Logger          *slog.Logger
	Clock           clockwork.Clock
	RPC             SolanaRPC
	ClickHouse      clickhouse.Client
	RefreshInterval time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.RPC == nil {
		return errors.New("rpc is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}

	// Optional with default
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log   *slog.Logger
	cfg   ViewConfig
	store *Store

	fetchedAt time.Time

	readyOnce sync.Once
	readyCh   chan struct{}
}

func NewView(
	cfg ViewConfig,
) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	store, err := NewStore(StoreConfig{
		Logger:     cfg.Logger,
		ClickHouse: cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		store:   store,
		readyCh: make(chan struct{}),
	}
	// Tables are created automatically by the dataset API on first refresh
	return v, nil
}

func (v *View) Store() *Store {
	return v.store
}

func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for serviceability view: %w", ctx.Err())
	}
}

func (v *View) Start(ctx context.Context) {
	// Main refresh loop
	go func() {
		v.log.Info("solana: starting refresh loop", "interval", v.cfg.RefreshInterval)

		v.safeRefresh(ctx)

		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				v.safeRefresh(ctx)
			}
		}
	}()

	// Hourly block production collection
	go func() {
		v.log.Info("solana: starting hourly block production collection")

		v.safeRefreshBlockProduction(ctx)

		// Then run every hour
		hourlyTicker := v.cfg.Clock.NewTicker(1 * time.Hour)
		defer hourlyTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hourlyTicker.Chan():
				v.safeRefreshBlockProduction(ctx)
			}
		}
	}()
}

// safeRefresh wraps Refresh with panic recovery to prevent the refresh loop from dying
func (v *View) safeRefresh(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			v.log.Error("solana: refresh panicked", "panic", r)
			metrics.ViewRefreshTotal.WithLabelValues("solana", "panic").Inc()
		}
	}()

	if err := v.Refresh(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		v.log.Error("solana: refresh failed", "error", err)
	}
}

// safeRefreshBlockProduction wraps RefreshBlockProduction with panic recovery
func (v *View) safeRefreshBlockProduction(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			v.log.Error("solana: block production refresh panicked", "panic", r)
			metrics.ViewRefreshTotal.WithLabelValues("solana-block-production", "panic").Inc()
		}
	}()

	if err := v.RefreshBlockProduction(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		v.log.Error("solana: block production refresh failed", "error", err)
	}
}

func (v *View) Refresh(ctx context.Context) error {
	refreshStart := time.Now()
	v.log.Debug("solana: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("solana: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("solana").Observe(duration.Seconds())
	}()

	v.log.Debug("solana: starting refresh")

	fetchedAt := time.Now().UTC().Truncate(time.Second)

	epochInfo, err := v.cfg.RPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("solana", "error").Inc()
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	leaderSchedule, err := v.cfg.RPC.GetLeaderSchedule(ctx)
	if err != nil {
		return fmt.Errorf("failed to get leader schedule: %w", err)
	}
	leaderScheduleEntries := make([]LeaderScheduleEntry, 0, len(leaderSchedule))
	for pk, slots := range leaderSchedule {
		leaderScheduleEntries = append(leaderScheduleEntries, LeaderScheduleEntry{
			NodePubkey: pk,
			Slots:      slots,
		})
	}

	voteAccounts, err := v.cfg.RPC.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentFinalized,
	})
	if err != nil {
		return fmt.Errorf("failed to get vote accounts: %w", err)
	}

	clusterNodes, err := v.cfg.RPC.GetClusterNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	v.log.Debug("solana: refreshing leader schedule", "count", len(leaderScheduleEntries))
	if err := v.store.ReplaceLeaderSchedule(ctx, leaderScheduleEntries, fetchedAt, currentEpoch); err != nil {
		return fmt.Errorf("failed to refresh leader schedule: %w", err)
	}

	v.log.Debug("solana: refreshing vote accounts", "count", len(voteAccounts.Current))
	if err := v.store.ReplaceVoteAccounts(ctx, voteAccounts.Current, fetchedAt, currentEpoch); err != nil {
		return fmt.Errorf("failed to refresh vote accounts: %w", err)
	}

	v.log.Debug("solana: refreshing cluster nodes", "count", len(clusterNodes))
	if err := v.store.ReplaceGossipNodes(ctx, clusterNodes, fetchedAt, currentEpoch); err != nil {
		return fmt.Errorf("failed to refresh cluster nodes: %w", err)
	}

	// Refresh vote account activity (sampled every minute)
	if err := v.RefreshVoteAccountActivity(ctx); err != nil {
		v.log.Error("solana: failed to refresh vote account activity", "error", err)
		// Don't fail the entire refresh if vote account activity fails
	}

	v.fetchedAt = fetchedAt
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("solana: view is now ready")
	})

	v.log.Debug("solana: refresh completed", "fetched_at", fetchedAt)
	metrics.ViewRefreshTotal.WithLabelValues("solana", "success").Inc()
	return nil
}

// RefreshVoteAccountActivity collects and inserts vote account activity data
func (v *View) RefreshVoteAccountActivity(ctx context.Context) error {
	refreshStart := time.Now()
	v.log.Debug("solana: vote account activity refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("solana: vote account activity refresh completed", "duration", duration.String())
	}()

	collectionTime := time.Now().UTC()
	runID := fmt.Sprintf("vote_account_activity_%d", collectionTime.Unix())

	// Get cluster slot
	clusterSlot, err := v.cfg.RPC.GetSlot(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get cluster slot: %w", err)
	}

	// Get vote accounts
	voteAccounts, err := v.cfg.RPC.GetVoteAccounts(ctx, &solanarpc.GetVoteAccountsOpts{
		Commitment: solanarpc.CommitmentFinalized,
	})
	if err != nil {
		return fmt.Errorf("failed to get vote accounts: %w", err)
	}

	// Create a map to track delinquent accounts
	delinquentMap := make(map[string]bool)
	for _, acc := range voteAccounts.Delinquent {
		delinquentMap[acc.VotePubkey.String()] = true
	}

	// Process all vote accounts (both current and delinquent)
	allAccounts := make([]solanarpc.VoteAccountsResult, 0, len(voteAccounts.Current)+len(voteAccounts.Delinquent))
	allAccounts = append(allAccounts, voteAccounts.Current...)
	allAccounts = append(allAccounts, voteAccounts.Delinquent...)

	entries := make([]VoteAccountActivityEntry, 0, len(allAccounts))
	for _, account := range allAccounts {
		votePubkey := account.VotePubkey.String()
		isDelinquent := delinquentMap[votePubkey]

		// Extract epochCredits
		epochCreditsJSON := ""
		creditsEpoch := 0
		creditsEpochCredits := uint64(0)

		if len(account.EpochCredits) > 0 {
			// EpochCredits is [][]int64 where each entry is [epoch, credits]
			// Convert to JSON string
			epochCreditsBytes, err := json.Marshal(account.EpochCredits)
			if err == nil {
				epochCreditsJSON = string(epochCreditsBytes)
				// Get the last entry (most recent)
				lastEntry := account.EpochCredits[len(account.EpochCredits)-1]
				if len(lastEntry) >= 2 {
					creditsEpoch = int(lastEntry[0])
					creditsEpochCredits = uint64(lastEntry[1])
				}
			}
		}

		// Calculate activated_stake_sol if activated_stake_lamports is available
		var activatedStakeSol *float64
		if account.ActivatedStake > 0 {
			sol := float64(account.ActivatedStake) / 1e9
			activatedStakeSol = &sol
		}

		entry := VoteAccountActivityEntry{
			Time:                   collectionTime,
			VoteAccountPubkey:      votePubkey,
			NodeIdentityPubkey:     account.NodePubkey.String(),
			RootSlot:               account.RootSlot,
			LastVoteSlot:           account.LastVote,
			ClusterSlot:            clusterSlot,
			IsDelinquent:           isDelinquent,
			EpochCreditsJSON:       epochCreditsJSON,
			CreditsEpoch:           creditsEpoch,
			CreditsEpochCredits:    creditsEpochCredits,
			ActivatedStakeLamports: &account.ActivatedStake,
			ActivatedStakeSol:      activatedStakeSol,
			Commission:             &account.Commission,
			CollectorRunID:         runID,
		}

		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		if err := v.store.InsertVoteAccountActivity(ctx, entries); err != nil {
			return fmt.Errorf("failed to insert vote account activity: %w", err)
		}
		v.log.Debug("solana: inserted vote account activity", "count", len(entries))
	}

	return nil
}

// RefreshBlockProduction collects and inserts block production data
func (v *View) RefreshBlockProduction(ctx context.Context) error {
	refreshStart := time.Now()
	v.log.Debug("solana: block production refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("solana: block production refresh completed", "duration", duration.String())
	}()

	collectionTime := time.Now().UTC()

	// Get current epoch
	epochInfo, err := v.cfg.RPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := int(epochInfo.Epoch)

	// Get block production for the current epoch
	// getBlockProduction returns data for the current or previous epoch
	blockProduction, err := v.cfg.RPC.GetBlockProduction(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block production: %w", err)
	}

	if blockProduction == nil {
		return fmt.Errorf("block production response is nil")
	}

	if blockProduction.Value.ByIdentity == nil {
		v.log.Debug("solana: block production has no identity data")
		return nil
	}

	entries := make([]BlockProductionEntry, 0, len(blockProduction.Value.ByIdentity))
	for identity, production := range blockProduction.Value.ByIdentity {
		if len(production) < 2 {
			v.log.Warn("solana: invalid block production data", "identity", identity, "production", production)
			continue
		}

		leaderSlotsAssigned := uint64(production[0])
		blocksProduced := uint64(production[1])

		entry := BlockProductionEntry{
			Epoch:                  currentEpoch,
			Time:                   collectionTime,
			LeaderIdentityPubkey:   identity.String(),
			LeaderSlotsAssignedCum: leaderSlotsAssigned,
			BlocksProducedCum:      blocksProduced,
		}

		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		if err := v.store.InsertBlockProduction(ctx, entries); err != nil {
			return fmt.Errorf("failed to insert block production: %w", err)
		}
		v.log.Debug("solana: inserted block production", "count", len(entries), "epoch", currentEpoch)
	}

	return nil
}
