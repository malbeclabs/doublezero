package worker

import (
	"context"
	"log/slog"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type Worker struct {
	log *slog.Logger
	cfg *Config
}

func New(cfg *Config) (*Worker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Worker{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Starting worker", "env", w.cfg.Env)

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Shutting down worker")
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	currentSlot, err := w.cfg.LedgerRPCClient.GetSlot(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		w.log.Error("Failed to get current slot", "error", err)
		return
	}

	provisioningSlot := currentSlot - w.cfg.ProvisioningSlotCount
	drainedSlot := currentSlot - w.cfg.DrainedSlotCount

	provisioningTime, err := w.cfg.LedgerRPCClient.GetBlockTime(ctx, provisioningSlot)
	if err != nil {
		w.log.Error("Failed to get block time for provisioning slot", "slot", provisioningSlot, "error", err)
		return
	}

	drainedTime, err := w.cfg.LedgerRPCClient.GetBlockTime(ctx, drainedSlot)
	if err != nil {
		w.log.Error("Failed to get block time for drained slot", "slot", drainedSlot, "error", err)
		return
	}

	w.log.Info("Device health oracle tick",
		"currentSlot", currentSlot,
		"provisioningSlotCount", w.cfg.ProvisioningSlotCount,
		"provisioningSlot", provisioningSlot,
		"provisioningTime", provisioningTime.Time(),
		"drainedSlotCount", w.cfg.DrainedSlotCount,
		"drainedSlot", drainedSlot,
		"drainedTime", drainedTime.Time())
}
