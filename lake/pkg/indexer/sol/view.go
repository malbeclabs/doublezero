package sol

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	"github.com/malbeclabs/doublezero/lake/pkg/indexer/metrics"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type SolanaRPC interface {
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	GetLeaderSchedule(ctx context.Context) (solanarpc.GetLeaderScheduleResult, error)
	GetClusterNodes(ctx context.Context) ([]*solanarpc.GetClusterNodesResult, error)
	GetVoteAccounts(ctx context.Context, opts *solanarpc.GetVoteAccountsOpts) (*solanarpc.GetVoteAccountsResult, error)
}

type ViewConfig struct {
	Logger        *slog.Logger
	Clock         clockwork.Clock
	RPC           SolanaRPC
	DB            duck.DB
	GeoIPStore    mcpgeoip.Store
	GeoIPResolver geoip.Resolver

	RefreshInterval time.Duration
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.RPC == nil {
		return errors.New("rpc is required")
	}
	if cfg.DB == nil {
		return errors.New("database is required")
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
		Logger: cfg.Logger,
		DB:     cfg.DB,
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
	// Tables are created automatically by SCDTableViaCSV on first refresh
	return v, nil
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
	go func() {
		v.log.Info("solana: starting refresh loop", "interval", v.cfg.RefreshInterval)

		if err := v.Refresh(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			v.log.Error("solana: initial refresh failed", "error", err)
		}
		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				if err := v.Refresh(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					v.log.Error("solana: refresh failed", "error", err)
				}
			}
		}
	}()
}

func (v *View) Refresh(ctx context.Context) error {
	refreshStart := time.Now()
	v.log.Debug("solana: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("solana: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("solana").Observe(duration.Seconds())
		if err := recover(); err != nil {
			metrics.ViewRefreshTotal.WithLabelValues("solana", "error").Inc()
			panic(err)
		}
	}()

	v.log.Debug("solana: starting refresh")

	fetchedAt := time.Now().UTC()

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

	v.log.Debug("solana: updating geoip records for gossip ips")
	geoipRecords := make([]*geoip.Record, 0, len(clusterNodes))
	for _, node := range clusterNodes {
		if node.Gossip == nil {
			continue
		}
		record := mcpgeoip.MaybeResolveAddr(v.cfg.GeoIPResolver, *node.Gossip)
		if record == nil {
			continue
		}
		geoipRecords = append(geoipRecords, record)
	}
	if err := v.cfg.GeoIPStore.UpsertRecords(ctx, geoipRecords); err != nil {
		return fmt.Errorf("failed to update geoip records: %w", err)
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
