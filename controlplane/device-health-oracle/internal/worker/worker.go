package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

// Processing in batches greatly speeds up e2e tests since large numbers of link and device records are created at the same time.
// Transaction wire cost per instruction is only ~9 bytes (accounts are deduplicated as 1-byte indices),
// so transaction size is not the bottleneck. The limiting factor is compute units: each instruction
// deserializes GlobalState (which contains variable-length allowlists) plus the device/link account,
// costing roughly 10,000-20,000 CUs per instruction. With Solana's default 200,000 CU budget per
// transaction, 8 instructions is a conservative limit that avoids compute budget failures.
const maxBatchSize = 8

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

	// Calculate burn-in slots, handling underflow for recently created environments
	var provisioningSlot, drainedSlot uint64
	if currentSlot > w.cfg.ProvisioningSlotCount {
		provisioningSlot = currentSlot - w.cfg.ProvisioningSlotCount
	}
	if currentSlot > w.cfg.DrainedSlotCount {
		drainedSlot = currentSlot - w.cfg.DrainedSlotCount
	}

	w.log.Info("Device health oracle tick",
		"currentSlot", currentSlot,
		"provisioningSlotCount", w.cfg.ProvisioningSlotCount,
		"provisioningSlot", provisioningSlot,
		"drainedSlotCount", w.cfg.DrainedSlotCount,
		"drainedSlot", drainedSlot)

	programData, err := w.cfg.Serviceability.GetProgramData(ctx)
	if err != nil {
		w.log.Error("Failed to get program data", "error", err)
		return
	}

	globalStatePubkey, _, err := serviceability.GetGlobalStatePDA(w.cfg.ServiceabilityProgramID)
	if err != nil {
		w.log.Error("Failed to get globalstate PDA", "error", err)
		return
	}

	w.updatePendingDeviceHealth(ctx, programData.Devices, globalStatePubkey)
	w.updatePendingLinkHealth(ctx, programData.Links, globalStatePubkey)
}

func (w *Worker) updatePendingDeviceHealth(ctx context.Context, devices []serviceability.Device, globalStatePubkey solana.PublicKey) {
	w.log.Debug("Processing devices", "count", len(devices))

	// Collect devices that need health updates
	var updates []serviceability.DeviceHealthUpdate
	for _, device := range devices {
		devicePubkey := solana.PublicKeyFromBytes(device.PubKey[:])
		w.log.Debug("Device state",
			"device", devicePubkey.String(),
			"code", device.Code,
			"status", device.Status,
			"statusValue", int(device.Status),
			"health", device.DeviceHealth,
			"healthValue", int(device.DeviceHealth))

		updates = append(updates, serviceability.DeviceHealthUpdate{
			DevicePubkey: devicePubkey,
			Health:       serviceability.DeviceHealthReadyForUsers,
		})
		w.log.Info("Queuing device health update",
			"device", devicePubkey.String(),
			"code", device.Code,
			"status", device.Status.String())
	}

	if len(updates) == 0 {
		return
	}

	for i := 0; i < len(updates); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(updates) {
			end = len(updates)
		}
		batch := updates[i:end]

		w.log.Info("Sending batched device health update", "batchSize", len(batch), "batchNum", i/maxBatchSize+1)

		sig, err := w.cfg.ServiceabilityExecutor.SetDeviceHealthBatch(ctx, batch, globalStatePubkey)
		if err != nil {
			w.log.Error("Failed to set device health batch", "error", err)
			continue
		}

		w.log.Info("Device health batch updated", "count", len(batch), "signature", sig.String())
	}
}

func (w *Worker) updatePendingLinkHealth(ctx context.Context, links []serviceability.Link, globalStatePubkey solana.PublicKey) {
	// Collect links that need health updates
	var updates []serviceability.LinkHealthUpdate
	for _, link := range links {
		linkPubkey := solana.PublicKeyFromBytes(link.PubKey[:])
		updates = append(updates, serviceability.LinkHealthUpdate{
			LinkPubkey: linkPubkey,
			Health:     serviceability.LinkHealthReadyForService,
		})
		w.log.Info("Queuing link health update",
			"link", linkPubkey.String(),
			"code", link.Code,
			"status", link.Status.String())
	}

	if len(updates) == 0 {
		return
	}

	for i := 0; i < len(updates); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(updates) {
			end = len(updates)
		}
		batch := updates[i:end]

		w.log.Info("Sending batched link health update", "batchSize", len(batch), "batchNum", i/maxBatchSize+1)

		sig, err := w.cfg.ServiceabilityExecutor.SetLinkHealthBatch(ctx, batch, globalStatePubkey)
		if err != nil {
			w.log.Error("Failed to set link health batch", "error", err)
			continue
		}

		w.log.Info("Link health batch updated", "count", len(batch), "signature", sig.String())
	}
}
