package dztelemlatency

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type InternetMetroLatencySample struct {
	OriginMetroPK   string
	TargetMetroPK   string
	DataProvider    string
	Epoch           uint64
	SampleIndex     int
	Time            time.Time
	RTTMicroseconds uint32
}

func (v *View) refreshInternetMetroLatencySamples(ctx context.Context, metros []dzsvc.Metro) error {
	// Get current epoch
	epochInfo, err := v.cfg.EpochRPC.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("failed to get epoch info: %w", err)
	}
	currentEpoch := epochInfo.Epoch

	// Fetch samples for current epoch and previous epoch
	epochsToFetch := []uint64{currentEpoch}
	if currentEpoch > 0 {
		epochsToFetch = append(epochsToFetch, currentEpoch-1)
	}

	// Get existing max sample_index for each origin_metro_pk+target_metro_pk+data_provider+epoch to determine what's new
	existingMaxIndices, err := v.store.GetExistingInternetMaxSampleIndices()
	if err != nil {
		return fmt.Errorf("failed to get existing max indices: %w", err)
	}

	var allSamples []InternetMetroLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.MaxConcurrency)
	metrosProcessed := 0
	metrosWithSamples := 0
	var metrosWithSamplesMu sync.Mutex

	// Generate metro pairs (avoid duplicates by ensuring origin < target)
	metroPairs := make([]struct {
		origin, target dzsvc.Metro
	}, 0)
	seenPairs := make(map[string]struct{})
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

			pairKey := fmt.Sprintf("%s:%s", origin.PK, target.PK)
			if _, ok := seenPairs[pairKey]; ok {
				continue
			}
			seenPairs[pairKey] = struct{}{}
			metroPairs = append(metroPairs, struct {
				origin, target dzsvc.Metro
			}{origin, target})
		}
	}

	// Sort for consistent ordering
	sort.Slice(metroPairs, func(i, j int) bool {
		return metroPairs[i].origin.Code < metroPairs[j].origin.Code ||
			(metroPairs[i].origin.Code == metroPairs[j].origin.Code && metroPairs[i].target.Code < metroPairs[j].target.Code)
	})

	for _, pair := range metroPairs {
		// Check for context cancellation before starting new goroutines
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		metrosProcessed++
		originPK, err := solana.PublicKeyFromBase58(pair.origin.PK)
		if err != nil {
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(pair.target.PK)
		if err != nil {
			continue
		}

		// Fetch samples for each data provider
		for _, dataProvider := range v.cfg.InternetDataProviders {
			// Check for context cancellation before starting new goroutines
			select {
			case <-ctx.Done():
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
				go func(originMetroPK, targetMetroPK string, originPK, targetPK solana.PublicKey, dataProvider string) {
					defer wg.Done()
					defer func() { <-sem }() // Release semaphore

					metroHasSamples := false
					var metroSamples []InternetMetroLatencySample

					for _, epoch := range epochsToFetch {
						// Check for context cancellation before each RPC call
						select {
						case <-ctx.Done():
							return
						default:
						}

						samples, err := v.cfg.TelemetryRPC.GetInternetLatencySamples(ctx, dataProvider, originPK, targetPK, v.cfg.InternetLatencyAgentPK, epoch)
						if err != nil {
							if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
								return
							}
							if errors.Is(err, telemetry.ErrAccountNotFound) {
								continue
							}
							continue
						}

						metroHasSamples = true

						// Check what's already in the database for this origin_metro_pk+target_metro_pk+data_provider+epoch
						key := fmt.Sprintf("%s:%s:%s:%d", originMetroPK, targetMetroPK, dataProvider, epoch)
						existingMaxIdx := -1
						if maxIdx, ok := existingMaxIndices[key]; ok {
							existingMaxIdx = maxIdx
						}

						// Convert samples to our format - only include new samples (sample_index > existingMaxIdx)
						converted := convertInternetLatencySamples(samples, originMetroPK, targetMetroPK, dataProvider, epoch)
						for _, sample := range converted {
							if sample.SampleIndex > existingMaxIdx {
								metroSamples = append(metroSamples, sample)
							}
						}
					}

					if metroHasSamples {
						metrosWithSamplesMu.Lock()
						metrosWithSamples++
						metrosWithSamplesMu.Unlock()
					}

					// Append samples to shared slice
					if len(metroSamples) > 0 {
						samplesMu.Lock()
						allSamples = append(allSamples, metroSamples...)
						samplesMu.Unlock()
					}
				}(pair.origin.PK, pair.target.PK, originPK, targetPK, dataProvider)
			}
		}
	}

done:
	wg.Wait()

	// Append new samples to table (instead of replacing)
	if len(allSamples) > 0 {
		if err := v.store.AppendInternetMetroLatencySamples(ctx, allSamples); err != nil {
			return fmt.Errorf("failed to append internet-metro latency samples: %w", err)
		}
		v.log.Debug("telemetry/internet-metro: sample refresh completed", "metros", metrosProcessed, "samples", len(allSamples))
	}
	return nil
}

func convertInternetLatencySamples(samples *telemetry.InternetLatencySamples, originMetroPK, targetMetroPK, dataProvider string, epoch uint64) []InternetMetroLatencySample {
	result := make([]InternetMetroLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
		// Convert microseconds since Unix epoch to time.Time
		sampleTime := time.Unix(int64(timestamp)/1_000_000, (int64(timestamp)%1_000_000)*1000)
		result[i] = InternetMetroLatencySample{
			OriginMetroPK:   originMetroPK,
			TargetMetroPK:   targetMetroPK,
			DataProvider:    dataProvider,
			Epoch:           epoch,
			SampleIndex:     i,
			Time:            sampleTime,
			RTTMicroseconds: rtt,
		}
	}
	return result
}
