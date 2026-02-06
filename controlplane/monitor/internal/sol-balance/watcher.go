package solbalance

import (
	"context"
	"log/slog"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

const (
	watcherName = "sol-balance"
)

type SolBalanceWatcher struct {
	log *slog.Logger
	cfg *Config
}

func NewSolBalanceWatcher(cfg *Config) (*SolBalanceWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &SolBalanceWatcher{
		log: cfg.Logger.With("watcher", watcherName),
		cfg: cfg,
	}, nil
}

func (w *SolBalanceWatcher) Name() string {
	return watcherName
}

func (w *SolBalanceWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	err := w.Tick(ctx)
	if err != nil {
		w.log.Error("failed to tick", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("context done, stopping")
			return nil
		case <-ticker.C:
			err := w.Tick(ctx)
			if err != nil {
				w.log.Error("failed to tick", "error", err)
			}
		}
	}
}

func (w *SolBalanceWatcher) Tick(ctx context.Context) error {
	w.log.Debug("ticking sol-balance")

	for label, pubkey := range w.cfg.Accounts {
		result, err := w.cfg.RPCClient.GetBalance(ctx, pubkey, solanarpc.CommitmentFinalized)
		if err != nil {
			MetricErrors.WithLabelValues(MetricErrorTypeGetBalance).Inc()
			w.log.Info("failed to get balance", "account", label, "pubkey", pubkey.String(), "error", err)
			continue
		}

		lamports := float64(result.Value)
		sol := lamports / LamportsPerSOL

		MetricBalanceLamports.WithLabelValues(label).Set(lamports)
		MetricBalanceSOL.WithLabelValues(label).Set(sol)

		w.log.Debug("balance", "account", label, "pubkey", pubkey.String(), "lamports", lamports, "sol", sol)

		if sol < w.cfg.Threshold {
			w.log.Warn("balance below threshold", "account", label, "pubkey", pubkey.String(), "sol", sol, "threshold", w.cfg.Threshold)
		}
	}

	return nil
}
