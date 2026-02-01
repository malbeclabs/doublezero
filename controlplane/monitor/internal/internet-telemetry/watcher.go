package internettelemetry

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
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
)

const (
	watcherName = "internet-telemetry"
)

var (
	dataProviders = []string{
		telemetryconfig.InternetTelemetryDataProviderNameRIPEAtlas,
		telemetryconfig.InternetTelemetryDataProviderNameWheresitup,
	}
)

type InternetTelemetryWatcher struct {
	log *slog.Logger
	cfg *Config

	lastEpoch uint64
	epochSet  bool
	stats     map[string]CircuitTelemetryStats
	mu        sync.RWMutex

	prevCircuits map[string]struct{}
}

func NewInternetTelemetryWatcher(cfg *Config) (*InternetTelemetryWatcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &InternetTelemetryWatcher{
		log:          cfg.Logger.With("watcher", watcherName),
		cfg:          cfg,
		stats:        map[string]CircuitTelemetryStats{},
		prevCircuits: map[string]struct{}{},
	}, nil
}

func (w *InternetTelemetryWatcher) Name() string {
	return watcherName
}

func (w *InternetTelemetryWatcher) Run(ctx context.Context) error {
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

func (w *InternetTelemetryWatcher) Tick(ctx context.Context) error {
	circuits, err := telemetrycircuits.GetInternetExchangeCircuits(ctx, w.log, w.cfg.Serviceability)
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

	// prune stats for old epochs if we rolled over
	if w.epochSet && w.lastEpoch != epoch {
		w.mu.Lock()
		prefix := fmt.Sprintf("epoch=%d, ", epoch)
		for k := range w.stats {
			if !strings.HasPrefix(k, prefix) {
				delete(w.stats, k)
			}
		}
		w.mu.Unlock()
	}

	// if no circuits: delete metrics for everything we had last tick (for all providers) and return
	if len(circuits) == 0 {
		w.mu.Lock()
		for code := range w.prevCircuits {
			for _, dp := range dataProviders {
				w.cfg.Metrics.Successes.DeleteLabelValues(dp, code)
				w.cfg.Metrics.Losses.DeleteLabelValues(dp, code)
				w.cfg.Metrics.Samples.DeleteLabelValues(dp, code)
				w.cfg.Metrics.AccountNotFound.DeleteLabelValues(dp, code)
			}
			for k := range w.stats {
				if strings.HasSuffix(k, ", circuit="+code) {
					delete(w.stats, k)
				}
			}
			w.log.Debug("deleted metrics for absent circuit", "code", code)
		}
		w.prevCircuits = map[string]struct{}{}
		w.mu.Unlock()
		return nil
	}

	// build the current set of circuits for diffing
	currCircuits := make(map[string]struct{}, len(circuits))
	for _, c := range circuits {
		currCircuits[c.Code] = struct{}{}
	}

	var wg sync.WaitGroup
	errorChan := make(chan error, len(dataProviders)*len(circuits))
	sem := make(chan struct{}, w.cfg.MaxConcurrency)

	for _, dataProvider := range dataProviders {
		for _, circuit := range circuits {
			wg.Add(1)
			sem <- struct{}{}
			go func(circuit telemetrycircuits.InternetExchangeCircuit, dataProvider string) {
				defer wg.Done()
				defer func() { <-sem }()

				originCode := circuit.OriginExchange.Code
				targetCode := circuit.TargetExchange.Code
				originPK := solana.PublicKeyFromBytes(circuit.OriginExchange.PubKey[:])
				targetPK := solana.PublicKeyFromBytes(circuit.TargetExchange.PubKey[:])

				account, err := w.cfg.Telemetry.GetInternetLatencySamples(ctx, w.cfg.InternetLatencyCollectorPK, dataProvider, originPK, targetPK, epoch)
				if err != nil {
					if errors.Is(err, telemetry.ErrAccountNotFound) {
						w.log.Debug("internet latency samples account not found", "error", err, "circuit_code", circuit.Code, "data_provider", dataProvider)
						w.cfg.Metrics.AccountNotFound.WithLabelValues(dataProvider, circuit.Code).Add(1)
						return
					}
					w.cfg.Metrics.Errors.WithLabelValues(MetricErrorTypeGetLatencySamples).Inc()
					w.log.Error("failed to get internet latency samples", "error", err)
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

				key := fmt.Sprintf("epoch=%d, data_provider=%s, circuit=%s", epoch, dataProvider, circuit.Code)

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
						w.cfg.Metrics.Successes.WithLabelValues(dataProvider, circuit.Code).Add(float64(successCountDelta))
					}
					if lossCountDelta > 0 {
						w.cfg.Metrics.Losses.WithLabelValues(dataProvider, circuit.Code).Add(float64(lossCountDelta))
					}
					if samplesDelta > 0 {
						w.cfg.Metrics.Samples.WithLabelValues(dataProvider, circuit.Code).Add(float64(samplesDelta))
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
					"data_provider", dataProvider,
					"origin_exchange", originCode,
					"target_exchange", targetCode,
					"agent_pk", w.cfg.InternetLatencyCollectorPK.String(),
					"epoch", epoch,
					"samples", len(account.Samples),
					"success_count", successCount,
					"loss_count", lossCount,
					"success_count_delta", successCountDelta,
					"loss_count_delta", lossCountDelta,
				)
			}(circuit, dataProvider)
		}
	}

	wg.Wait()

	select {
	case err := <-errorChan:
		return err
	default:
	}

	// delete metrics for circuits that disappeared since the previous tick
	w.mu.Lock()
	for code := range w.prevCircuits {
		if _, ok := currCircuits[code]; !ok {
			for _, dp := range dataProviders {
				w.cfg.Metrics.Successes.DeleteLabelValues(dp, code)
				w.cfg.Metrics.Losses.DeleteLabelValues(dp, code)
				w.cfg.Metrics.Samples.DeleteLabelValues(dp, code)
				w.cfg.Metrics.AccountNotFound.DeleteLabelValues(dp, code)
			}
			for k := range w.stats {
				if strings.HasSuffix(k, ", circuit="+code) {
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
