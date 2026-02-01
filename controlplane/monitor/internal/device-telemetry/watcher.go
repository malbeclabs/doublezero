package devicetelemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	telemetrycircuits "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/circuits"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
)

const (
	watcherName = "device-telemetry"
)

type DeviceTelemetryWatcher struct {
	log *slog.Logger
	cfg *Config

	lastEpoch uint64
	epochSet  bool
	stats     map[string]CircuitTelemetryStats
	mu        sync.RWMutex

	prevCircuits map[string]string
}

func NewDeviceTelemetryWatcher(cfg *Config) (*DeviceTelemetryWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &DeviceTelemetryWatcher{
		log:          cfg.Logger.With("watcher", watcherName),
		cfg:          cfg,
		stats:        map[string]CircuitTelemetryStats{},
		prevCircuits: map[string]string{},
	}, nil
}

func (w *DeviceTelemetryWatcher) Name() string {
	return watcherName
}

func (w *DeviceTelemetryWatcher) Run(ctx context.Context) error {
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

type CircuitTelemetryStats struct {
	SuccessCount uint32
	LossCount    uint32
}

func (w *DeviceTelemetryWatcher) Tick(ctx context.Context) error {
	circuits, err := telemetrycircuits.GetDeviceLinkCircuits(ctx, w.log, w.cfg.Serviceability)
	if err != nil {
		w.cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetCircuits).Inc()
		return fmt.Errorf("failed to get circuits: %w", err)
	}

	epochInfo, err := w.cfg.LedgerRPCClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		w.cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetEpochInfo).Inc()
		w.log.Error("failed to get epoch info", "error", err)
		return err
	}
	epoch := epochInfo.Epoch

	// prune old epoch entries once epoch advances; keep only keys for current epoch
	if w.epochSet && w.lastEpoch != epoch {
		w.mu.Lock()
		prefix := fmt.Sprintf("%d-", epoch)
		for k := range w.stats {
			if !strings.HasPrefix(k, prefix) {
				delete(w.stats, k)
			}
		}
		w.mu.Unlock()
	}

	// if no circuits, delete metrics for everything we saw previously and return
	if len(circuits) == 0 {
		w.mu.Lock()
		for code, linkStatus := range w.prevCircuits {
			w.cfg.Metrics.Successes.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.Losses.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.Samples.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.AccountNotFound.DeleteLabelValues(code, linkStatus)
			for k := range w.stats {
				if strings.HasSuffix(k, "-"+code) {
					delete(w.stats, k)
				}
			}
			w.log.Debug("deleted metrics for absent circuit", "code", code)
		}
		w.prevCircuits = map[string]string{}
		w.mu.Unlock()
		return nil
	}

	// build the current set of circuits for diffing
	currCircuits := make(map[string]string, len(circuits))
	for _, c := range circuits {
		currCircuits[c.Code] = c.Link.Status.String()
	}

	var wg sync.WaitGroup
	errorChan := make(chan error, len(circuits))
	sem := make(chan struct{}, w.cfg.MaxConcurrency)

	for _, circuit := range circuits {
		wg.Add(1)
		sem <- struct{}{}
		go func(circuit telemetrycircuits.DeviceLinkCircuit) {
			defer wg.Done()
			defer func() { <-sem }()

			linkPK := solana.PublicKeyFromBytes(circuit.Link.PubKey[:])
			linkCode := circuit.Link.Code
			originCode := circuit.OriginDevice.Code
			targetCode := circuit.TargetDevice.Code
			originPK := solana.PublicKeyFromBytes(circuit.OriginDevice.PubKey[:])
			targetPK := solana.PublicKeyFromBytes(circuit.TargetDevice.PubKey[:])

			account, err := w.cfg.Telemetry.GetDeviceLatencySamples(ctx, originPK, targetPK, linkPK, epoch)
			if err != nil {
				if errors.Is(err, telemetry.ErrAccountNotFound) {
					w.log.Debug("device latency samples account not found", "error", err, "circuit_code", circuit.Code)
					w.cfg.Metrics.AccountNotFound.WithLabelValues(circuit.Code, circuit.Link.Status.String()).Add(1)
					return
				}
				w.cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetLatencySamples).Inc()
				w.log.Error("failed to get device latency samples", "error", err)
				errorChan <- err
				return
			}

			var successCount, lossCount uint32
			for _, sample := range account.Samples {
				if sample == 0 {
					lossCount++
				} else {
					successCount++
				}
			}

			key := fmt.Sprintf("%d-%s", epoch, circuit.Code)

			var successCountDelta, lossCountDelta, samplesDelta uint32
			w.mu.RLock()
			if prevStats, ok := w.stats[key]; ok && w.epochSet && w.lastEpoch == epoch {
				if successCount >= prevStats.SuccessCount {
					successCountDelta = successCount - prevStats.SuccessCount
				} else {
					w.log.Warn("success counter decreased unexpectedly",
						"circuit_code", circuit.Code,
						"epoch", epoch,
						"prev_success_count", prevStats.SuccessCount,
						"current_success_count", successCount,
					)
					successCountDelta = 0 // counter shrank; treat as no delta
				}
				if lossCount >= prevStats.LossCount {
					lossCountDelta = lossCount - prevStats.LossCount
				} else {
					w.log.Warn("loss counter decreased unexpectedly",
						"circuit_code", circuit.Code,
						"epoch", epoch,
						"prev_loss_count", prevStats.LossCount,
						"current_loss_count", lossCount,
					)
					lossCountDelta = 0
				}
				samplesDelta = successCountDelta + lossCountDelta

				if successCountDelta > 0 {
					w.cfg.Metrics.Successes.WithLabelValues(circuit.Code, circuit.Link.Status.String()).Add(float64(successCountDelta))
				}
				if lossCountDelta > 0 {
					w.cfg.Metrics.Losses.WithLabelValues(circuit.Code, circuit.Link.Status.String()).Add(float64(lossCountDelta))
				}
				if samplesDelta > 0 {
					w.cfg.Metrics.Samples.WithLabelValues(circuit.Code, circuit.Link.Status.String()).Add(float64(samplesDelta))
				}
			}
			w.mu.RUnlock()

			w.mu.Lock()
			w.stats[key] = CircuitTelemetryStats{
				SuccessCount: successCount,
				LossCount:    lossCount,
			}
			w.mu.Unlock()

			w.log.Debug("circuit telemetry",
				"code", circuit.Code,
				"origin_device", originCode,
				"target_device", targetCode,
				"link", linkCode,
				"link_pk", linkPK.String(),
				"epoch", epoch,
				"samples", len(account.Samples),
				"success_count", successCount,
				"loss_count", lossCount,
				"success_count_delta", successCountDelta,
				"loss_count_delta", lossCountDelta,
			)
		}(circuit)
	}

	wg.Wait()

	select {
	case err := <-errorChan:
		return err
	default:
	}

	// delete metrics for circuits that disappeared since the previous tick
	w.mu.Lock()
	for code, linkStatus := range w.prevCircuits {
		if _, ok := currCircuits[code]; !ok {
			w.cfg.Metrics.Successes.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.Losses.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.Samples.DeleteLabelValues(code, linkStatus)
			w.cfg.Metrics.AccountNotFound.DeleteLabelValues(code, linkStatus)
			for k := range w.stats {
				if strings.HasSuffix(k, "-"+code) {
					delete(w.stats, k)
				}
			}
			w.log.Debug("deleted metrics for absent circuit", "code", code)
		}
	}
	w.prevCircuits = currCircuits
	w.epochSet = true
	w.lastEpoch = epoch
	w.mu.Unlock()

	return nil
}
