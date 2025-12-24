package dztelem

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
)

type InternetMetroCircuit struct {
	Code            string
	OriginMetroPK   string
	TargetMetroPK   string
	OriginMetroCode string
	TargetMetroCode string
}

func ComputeInternetMetroCircuits(metros []dzsvc.Metro) []InternetMetroCircuit {
	circuits := make([]InternetMetroCircuit, 0)
	circuitsByCode := make(map[string]struct{})

	for _, originMetro := range metros {
		for _, targetMetro := range metros {
			if originMetro.Code == targetMetro.Code {
				continue
			}

			// Ensure consistent ordering (origin < target) to avoid duplicates
			var origin, target dzsvc.Metro
			if originMetro.Code < targetMetro.Code {
				origin, target = originMetro, targetMetro
			} else {
				origin, target = targetMetro, originMetro
			}

			code := fmt.Sprintf("%s → %s", origin.Code, target.Code)
			if _, ok := circuitsByCode[code]; ok {
				continue
			}

			circuitsByCode[code] = struct{}{}
			circuits = append(circuits, InternetMetroCircuit{
				Code:            code,
				OriginMetroPK:   origin.PK,
				TargetMetroPK:   target.PK,
				OriginMetroCode: origin.Code,
				TargetMetroCode: target.Code,
			})
		}
	}

	// Sort for consistent ordering
	sort.Slice(circuits, func(i, j int) bool {
		return circuits[i].Code < circuits[j].Code
	})

	return circuits
}

type InternetMetroLatencySample struct {
	CircuitCode           string
	DataProvider          string
	Epoch                 uint64
	SampleIndex           int
	TimestampMicroseconds uint64
	RTTMicroseconds       uint32
}

func (v *View) refreshInternetMetroLatencySamples(ctx context.Context, circuits []InternetMetroCircuit) error {
	v.log.Debug("telemetry/internet-metro: starting sample refresh", "circuits", len(circuits), "data_providers", len(v.cfg.InternetDataProviders))

	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch
	v.log.Debug("telemetry/internet-metro: current epoch", "epoch", currentEpoch)

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}
	v.log.Debug("telemetry/internet-metro: fetching epochs", "epochs", epochsToFetch, "max_concurrency", v.cfg.MaxConcurrency)

	// Get existing max sample_index for each circuit_code+data_provider+epoch to determine what's new
	existingMaxIndices, err := v.store.GetExistingInternetMaxSampleIndices()
	if err != nil {
		v.log.Warn("telemetry/internet-metro: failed to get existing max indices, will insert all samples", "error", err)
		existingMaxIndices = make(map[string]int) // Empty map means no existing data
	} else {
		v.log.Debug("telemetry/internet-metro: found existing max indices", "count", len(existingMaxIndices))
	}

	var allSamples []InternetMetroLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	circuitsProcessed := 0
	circuitsWithSamples := 0
	var circuitsWithSamplesMu sync.Mutex

	for _, circuit := range circuits {
		// Check for context cancellation before starting new goroutines
		select {
		case <-ctx.Done():
			v.log.Debug("telemetry/internet-metro: context cancelled, stopping circuit processing")
			goto done
		default:
		}

		circuitsProcessed++
		originPK, err := solana.PublicKeyFromBase58(circuit.OriginMetroPK)
		if err != nil {
			v.log.Debug("telemetry/internet-metro: invalid origin metro PK", "circuit", circuit.Code, "error", err)
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(circuit.TargetMetroPK)
		if err != nil {
			v.log.Debug("telemetry/internet-metro: invalid target metro PK", "circuit", circuit.Code, "error", err)
			continue
		}

		// Fetch samples for each data provider
		for _, dataProvider := range v.cfg.InternetDataProviders {
			// Check for context cancellation before starting new goroutines
			select {
			case <-ctx.Done():
				v.log.Debug("telemetry/internet-metro: context cancelled, stopping data provider processing")
				goto done
			default:
			}

			wg.Add(1)
			// Try to acquire semaphore with context cancellation support
			select {
			case <-ctx.Done():
				wg.Done()
				goto done
			case sem <- struct{}{}:
				go func(circuit InternetMetroCircuit, originPK, targetPK solana.PublicKey, dataProvider string) {
					defer wg.Done()
					defer func() { <-sem }() // Release semaphore

					circuitHasSamples := false
					var circuitSamples []InternetMetroLatencySample

					for _, epoch := range epochsToFetch {
						// Check for context cancellation before each RPC call
						select {
						case <-ctx.Done():
							v.log.Debug("telemetry/internet-metro: context cancelled during fetch", "circuit", circuit.Code, "data_provider", dataProvider)
							return
						default:
						}

						samples, err := v.cfg.TelemetryRPC.GetInternetLatencySamples(ctx, dataProvider, originPK, targetPK, v.cfg.InternetLatencyAgentPK, epoch)
						if err != nil {
							if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
								return
							}
							if errors.Is(err, telemetry.ErrAccountNotFound) {
								v.log.Debug("telemetry/internet-metro: no samples found", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch)
								continue
							}
							v.log.Debug("telemetry/internet-metro: failed to get latency samples", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch, "error", err)
							continue
						}

						circuitHasSamples = true
						sampleCount := len(samples.Samples)
						nextSampleIndex := samples.NextSampleIndex
						v.log.Debug("telemetry/internet-metro: fetched samples", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch, "samples", sampleCount, "next_sample_index", nextSampleIndex)

						// Check what's already in the database for this circuit+data_provider+epoch
						key := fmt.Sprintf("%s:%s:%d", circuit.Code, dataProvider, epoch)
						existingMaxIdx := -1
						if maxIdx, ok := existingMaxIndices[key]; ok {
							existingMaxIdx = maxIdx
						}

						// Convert samples to our format - only include new samples (sample_index > existingMaxIdx)
						converted := convertInternetLatencySamples(samples, circuit.Code, dataProvider, epoch)
						newSamples := 0
						for _, sample := range converted {
							if sample.SampleIndex > existingMaxIdx {
								circuitSamples = append(circuitSamples, sample)
								newSamples++
							}
						}
						if newSamples > 0 {
							v.log.Debug("telemetry/internet-metro: found new samples", "circuit", circuit.Code, "data_provider", dataProvider, "epoch", epoch, "existing_max_idx", existingMaxIdx, "new_samples", newSamples, "total_samples", len(converted))
						}
					}

					if circuitHasSamples {
						circuitsWithSamplesMu.Lock()
						circuitsWithSamples++
						circuitsWithSamplesMu.Unlock()
					}

					// Append samples to shared slice
					if len(circuitSamples) > 0 {
						samplesMu.Lock()
						allSamples = append(allSamples, circuitSamples...)
						samplesMu.Unlock()
					}
				}(circuit, originPK, targetPK, dataProvider)
			}
		}
	}

done:
	wg.Wait()

	v.log.Debug("telemetry/internet-metro: processed circuits", "total", circuitsProcessed, "with_samples", circuitsWithSamples, "total_samples", len(allSamples))

	// Append new samples to table (instead of replacing)
	if len(allSamples) > 0 {
		v.log.Debug("telemetry/internet-metro: appending new latency samples", "new_samples", len(allSamples))
		if err := v.store.AppendInternetMetroLatencySamples(allSamples); err != nil {
			v.log.Error("telemetry/internet-metro: failed to append latency samples", "error", err, "total_samples", len(allSamples))
			return fmt.Errorf("failed to append internet-metro latency samples: %w", err)
		}
		v.log.Debug("telemetry/internet-metro: sample refresh completed", "samples_inserted", len(allSamples))
	} else {
		v.log.Debug("telemetry/internet-metro: no new samples to insert")
	}
	return nil
}

func convertInternetLatencySamples(samples *telemetry.InternetLatencySamples, circuitCode, dataProvider string, epoch uint64) []InternetMetroLatencySample {
	result := make([]InternetMetroLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
		result[i] = InternetMetroLatencySample{
			CircuitCode:           circuitCode,
			DataProvider:          dataProvider,
			Epoch:                 epoch,
			SampleIndex:           i,
			TimestampMicroseconds: timestamp,
			RTTMicroseconds:       rtt,
		}
	}
	return result
}
