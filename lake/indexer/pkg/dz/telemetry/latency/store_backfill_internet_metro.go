package dztelemlatency

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"

	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

// BackfillInternetMetroLatencyRPC defines the RPC interface needed for internet metro latency backfill
type BackfillInternetMetroLatencyRPC interface {
	GetInternetLatencySamples(ctx context.Context, dataProviderName string, originLocationPK, targetLocationPK, agentPK solana.PublicKey, epoch uint64) (*telemetry.InternetLatencySamples, error)
}

// BackfillInternetMetroLatencyResult contains the results of an internet metro latency backfill operation
type BackfillInternetMetroLatencyResult struct {
	Epoch        uint64
	SampleCount  int
	PairsQueried int
}

// MetroPair represents a pair of metros for internet latency measurement
type MetroPair struct {
	Origin dzsvc.Metro
	Target dzsvc.Metro
}

// GenerateMetroPairs generates unique metro pairs for internet latency measurement.
// It ensures consistent ordering (origin < target by code) to avoid duplicates.
func GenerateMetroPairs(metros []dzsvc.Metro) []MetroPair {
	pairs := make([]MetroPair, 0)
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
			pairs = append(pairs, MetroPair{Origin: origin, Target: target})
		}
	}

	// Sort for consistent ordering
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Origin.Code < pairs[j].Origin.Code ||
			(pairs[i].Origin.Code == pairs[j].Origin.Code && pairs[i].Target.Code < pairs[j].Target.Code)
	})

	return pairs
}

// BackfillInternetMetroLatencyForEpoch fetches and stores internet metro latency samples for a single epoch.
// It fetches all samples and relies on ReplacingMergeTree for deduplication.
func (s *Store) BackfillInternetMetroLatencyForEpoch(
	ctx context.Context,
	rpc BackfillInternetMetroLatencyRPC,
	metroPairs []MetroPair,
	dataProviders []string,
	internetLatencyAgentPK solana.PublicKey,
	epoch uint64,
	maxConcurrency int,
) (*BackfillInternetMetroLatencyResult, error) {
	var allSamples []InternetMetroLatencySample
	var samplesMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)
	pairsQueried := 0

	for _, pair := range metroPairs {
		originPK, err := solana.PublicKeyFromBase58(pair.Origin.PK)
		if err != nil {
			continue
		}
		targetPK, err := solana.PublicKeyFromBase58(pair.Target.PK)
		if err != nil {
			continue
		}

		// Fetch samples for each data provider
		for _, dataProvider := range dataProviders {
			pairsQueried++
			wg.Add(1)
			sem <- struct{}{}
			go func(originMetroPK, targetMetroPK string, originPK, targetPK solana.PublicKey, dataProvider string) {
				defer wg.Done()
				defer func() { <-sem }()

				samples, err := rpc.GetInternetLatencySamples(ctx, dataProvider, originPK, targetPK, internetLatencyAgentPK, epoch)
				if err != nil {
					if errors.Is(err, telemetry.ErrAccountNotFound) {
						return
					}
					s.log.Debug("failed to get internet latency samples", "error", err, "epoch", epoch, "origin", originMetroPK, "target", targetMetroPK)
					return
				}
				if samples == nil || len(samples.Samples) == 0 {
					return
				}

				// Convert samples to our format
				converted := convertInternetLatencySamplesForBackfill(samples, originMetroPK, targetMetroPK, dataProvider, epoch)

				if len(converted) > 0 {
					samplesMu.Lock()
					allSamples = append(allSamples, converted...)
					samplesMu.Unlock()
				}
			}(pair.Origin.PK, pair.Target.PK, originPK, targetPK, dataProvider)
		}
	}

	wg.Wait()

	if len(allSamples) > 0 {
		if err := s.AppendInternetMetroLatencySamples(ctx, allSamples); err != nil {
			return nil, err
		}
	}

	return &BackfillInternetMetroLatencyResult{
		Epoch:        epoch,
		SampleCount:  len(allSamples),
		PairsQueried: pairsQueried,
	}, nil
}

func convertInternetLatencySamplesForBackfill(samples *telemetry.InternetLatencySamples, originMetroPK, targetMetroPK, dataProvider string, epoch uint64) []InternetMetroLatencySample {
	result := make([]InternetMetroLatencySample, len(samples.Samples))
	for i, rtt := range samples.Samples {
		timestamp := samples.StartTimestampMicroseconds + uint64(i)*samples.SamplingIntervalMicroseconds
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
