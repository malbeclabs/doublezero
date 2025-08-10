package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	datastats "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

const (
	DataProviderNameRIPEAtlas  = "ripeatlas"
	DataProviderNameWheresitup = "wheresitup"
)

func (p *provider) GetCircuitLatenciesForEpoch(ctx context.Context, circuitCode string, epoch uint64, dataProvider string) ([]datastats.CircuitLatencySample, error) {
	cached := p.GetCachedCircuitLatencies(ctx, circuitCode, epoch, dataProvider)
	if cached != nil {
		return cached, nil
	}

	circuits, err := p.GetCircuits(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get circuits: %w", err)
	}
	circuitsByCode := map[string]Circuit{}
	for _, circuit := range circuits {
		circuitsByCode[circuit.Code] = circuit
	}
	circuit, ok := circuitsByCode[circuitCode]
	if !ok {
		return nil, fmt.Errorf("circuit not found: %s", circuitCode)
	}

	// TODO(snormore): If empty, we should combine/aggregate the data from all data providers here based on some given aggregator function (avg, min, max, etc.).
	if dataProvider == "" {
		dataProvider = DataProviderNameWheresitup
	}
	account, err := p.cfg.TelemetryClient.GetInternetLatencySamples(ctx, dataProvider, circuit.OriginLocation.PK, circuit.TargetLocation.PK, p.cfg.AgentPK, epoch)
	if err != nil {
		if errors.Is(err, telemetry.ErrAccountNotFound) {
			// If the account is not found, cache an empty array for the epoch for a short time.
			p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, dataProvider, []datastats.CircuitLatencySample{}, p.cfg.CurrentEpochLatenciesCacheTTL)
			return nil, err
		}
		return nil, fmt.Errorf("failed to get internet latency samples for epoch %d: %w", epoch, err)
	}

	samples := enrichSamplesWithTimestamps(account.Samples, account.StartTimestampMicroseconds, account.SamplingIntervalMicroseconds)

	// If the epoch is sufficiently in the past, cache for much longer.
	currentEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to get current epoch: %w", err)
	}
	ttl := p.cfg.CurrentEpochLatenciesCacheTTL
	if epoch < currentEpoch-1 {
		ttl = p.cfg.HistoricEpochLatenciesCacheTTL
	}
	p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, dataProvider, samples, ttl)

	return samples, nil
}

func (p *provider) GetCircuitLatenciesForTimeRange(ctx context.Context, circuitCode string, from, to time.Time, dataProvider string) ([]datastats.CircuitLatencySample, error) {
	startEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("failed to get start epoch: %w", err)
	}
	endEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get end epoch: %w", err)
	}

	var latencies []datastats.CircuitLatencySample

	group := p.getCircuitLatenciesPool.NewGroupContext(ctx)

	for epoch := startEpoch; epoch <= endEpoch; epoch++ {
		epoch := epoch

		group.SubmitErr(func() ([]datastats.CircuitLatencySample, error) {
			result, err := p.GetCircuitLatenciesForEpoch(ctx, circuitCode, epoch, dataProvider)
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				p.cfg.Logger.Info("Internet latency samples not found, skipping", "epoch", epoch, "circuit", circuitCode)
				return []datastats.CircuitLatencySample{}, nil
			}
			return result, err
		})

	}

	results, err := group.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to get circuit latencies: %w", err)
	}

	for _, samples := range results {
		for _, sample := range samples {
			ts, err := time.Parse(time.RFC3339Nano, sample.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("failed to parse timestamp: %w", err)
			}
			if !ts.Before(from) && !ts.After(to) {
				latencies = append(latencies, sample)
			}
		}
	}

	return latencies, nil
}

func (p *provider) GetCircuitLatenciesDownsampled(
	ctx context.Context,
	circuitCode string,
	from, to time.Time,
	maxPoints uint64,
	unit Unit,
	dataProvider string,
) ([]datastats.CircuitLatencyStat, error) {
	switch unit {
	case UnitMillisecond, UnitMicrosecond:
	default:
		return nil, fmt.Errorf("invalid unit: %s (must be %s or %s)", unit, UnitMillisecond, UnitMicrosecond)
	}

	latencies, err := p.GetCircuitLatenciesForTimeRange(ctx, circuitCode, from, to, dataProvider)
	if err != nil {
		return nil, err
	}
	if maxPoints == 0 {
		maxPoints = 1
	}

	if maxPoints == 1 {
		var all []float64
		for _, s := range latencies {
			all = append(all, float64(s.RTT))
		}
		if len(all) == 0 {
			return nil, nil
		}
		stats := datastats.ComputeStats(from, all)
		stats.Circuit = circuitCode
		if unit == UnitMillisecond {
			stats.ConvertUnit(1000.0)
		}
		return []datastats.CircuitLatencyStat{stats}, nil
	}

	out, err := datastats.DownsampleCircuitLatencies(ctx, circuitCode, from, to, maxPoints, latencies)
	if err != nil {
		return nil, err
	}

	if unit == UnitMillisecond {
		for i := range out {
			out[i].ConvertUnit(1000.0)
		}
	}
	return out, nil
}

func enrichSamplesWithTimestamps(samples []uint32, startTimestampMicroseconds, samplingIntervalMicroseconds uint64) []datastats.CircuitLatencySample {
	circuitLatencies := make([]datastats.CircuitLatencySample, 0, len(samples))
	for i, sample := range samples {
		timestampMicros := startTimestampMicroseconds + uint64(i)*samplingIntervalMicroseconds
		secs := int64(timestampMicros / 1_000_000)
		nanos := int64(timestampMicros%1_000_000) * 1000
		timestamp := time.Unix(secs, nanos)
		circuitLatencies = append(circuitLatencies, datastats.CircuitLatencySample{
			Timestamp: timestamp.Format(time.RFC3339Nano),
			RTT:       sample,
		})
	}
	return circuitLatencies
}
