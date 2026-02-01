package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	datastats "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	telemetry "github.com/malbeclabs/doublezero/sdk/telemetry/go"
)

const (
	DataProviderNameRIPEAtlas  = "ripeatlas"
	DataProviderNameWheresitup = "wheresitup"
)

type CircuitLatenciesWithHeader struct {
	Samples []datastats.CircuitLatencySample
}

func (p *provider) GetCircuitLatencies(ctx context.Context, cfg GetCircuitLatenciesConfig) ([]datastats.CircuitLatencyStat, error) {
	p.cfg.Logger.Debug("Getting circuit latencies", "circuit", cfg.Circuit, "unit", cfg.Unit, "epochs", cfg.Epochs, "time", cfg.Time, "maxPoints", cfg.MaxPoints, "interval", cfg.Interval, "dataProvider", cfg.DataProvider)

	switch cfg.Unit {
	case UnitMillisecond, UnitMicrosecond:
	default:
		return nil, fmt.Errorf("invalid unit: %s (must be %s or %s)", cfg.Unit, UnitMillisecond, UnitMicrosecond)
	}

	if cfg.DataProvider == "" {
		return nil, fmt.Errorf("data provider is required")
	}

	if cfg.Epochs != nil && cfg.Time != nil {
		return nil, fmt.Errorf("from_epoch and to_epoch or from_time and to_time cannot be set at the same time")
	}

	var samples []datastats.CircuitLatencySample
	if cfg.Epochs != nil {
		if cfg.Epochs.From > cfg.Epochs.To {
			return nil, fmt.Errorf("from_epoch must be less than to_epoch")
		}
		partitions, err := p.getCircuitLatenciesForEpochRange(ctx, cfg.Circuit, cfg.Epochs.From, cfg.Epochs.To, cfg.DataProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to get circuit latencies for epoch range: %w", err)
		}
		for _, partition := range partitions {
			samples = append(samples, partition.Samples...)
		}
	} else if cfg.Time != nil {
		if cfg.Time.From.After(cfg.Time.To) {
			return nil, fmt.Errorf("from_time must be before to_time")
		}
		var err error
		samples, err = p.getCircuitLatenciesForTimeRange(ctx, cfg.Circuit, cfg.Time.From, cfg.Time.To, cfg.DataProvider)
		if err != nil {
			return nil, fmt.Errorf("failed to get circuit latencies for time range: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no time or epoch range provided")
	}

	stats, err := datastats.Aggregate(cfg.Circuit, samples, cfg.MaxPoints, cfg.Interval)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate circuit latencies: %w", err)
	}

	if cfg.Unit == UnitMillisecond {
		for i := range stats {
			stats[i].ConvertUnit(1000.0)
		}
	}

	return stats, nil
}

func (p *provider) GetCircuitLatenciesForEpoch(ctx context.Context, circuitCode string, epoch uint64, dataProvider string) (*CircuitLatenciesWithHeader, error) {
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

	account, err := p.cfg.TelemetryClient.GetInternetLatencySamples(ctx, p.cfg.AgentPK, dataProvider, circuit.OriginExchange.PK, circuit.TargetExchange.PK, epoch)
	if err != nil {
		if errors.Is(err, telemetry.ErrAccountNotFound) {
			// If the account is not found, cache an empty array for the epoch for a short time.
			p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, dataProvider, &CircuitLatenciesWithHeader{}, p.cfg.CurrentEpochLatenciesCacheTTL)
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
	p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, dataProvider, &CircuitLatenciesWithHeader{
		Samples: samples,
	}, ttl)

	return &CircuitLatenciesWithHeader{
		Samples: samples,
	}, nil
}

func (p *provider) getCircuitLatenciesForEpochRange(ctx context.Context, circuitCode string, from, to uint64, dataProvider string) ([]*CircuitLatenciesWithHeader, error) {
	group := p.getCircuitLatenciesPool.NewGroupContext(ctx)

	for epoch := from; epoch <= to; epoch++ {
		epoch := epoch

		group.SubmitErr(func() (*CircuitLatenciesWithHeader, error) {
			if result, err := p.GetCircuitLatenciesForEpoch(ctx, circuitCode, epoch, dataProvider); err != nil {
				if errors.Is(err, telemetry.ErrAccountNotFound) {
					p.log.Info("Internet latency samples not found, skipping", "epoch", epoch, "circuit", circuitCode, "dataProvider", dataProvider)
					return nil, nil
				}
				return nil, err
			} else {
				return result, nil
			}
		})
	}

	results, err := group.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to get circuit latencies for epoch range: %w", err)
	}

	partitions := make([]*CircuitLatenciesWithHeader, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		partitions = append(partitions, result)
	}

	return partitions, nil
}

func (p *provider) getCircuitLatenciesForTimeRange(ctx context.Context, circuitCode string, from, to time.Time, dataProvider string) ([]datastats.CircuitLatencySample, error) {
	startEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("failed to get start epoch: %w", err)
	}
	endEpoch, err := p.cfg.EpochFinder.ApproximateAtTime(ctx, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get end epoch: %w", err)
	}

	group := p.getCircuitLatenciesPool.NewGroupContext(ctx)

	for epoch := startEpoch; epoch <= endEpoch; epoch++ {
		epoch := epoch

		group.SubmitErr(func() (*CircuitLatenciesWithHeader, error) {
			result, err := p.GetCircuitLatenciesForEpoch(ctx, circuitCode, epoch, dataProvider)
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				p.cfg.Logger.Info("Internet latency samples not found, skipping", "epoch", epoch, "circuit", circuitCode, "dataProvider", dataProvider)
				return nil, nil
			}
			return result, err
		})

	}

	results, err := group.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to get circuit latencies: %w", err)
	}

	var latencies []datastats.CircuitLatencySample

	for _, result := range results {
		if result == nil {
			continue
		}
		for _, sample := range result.Samples {
			if !sample.Timestamp.Before(from) && !sample.Timestamp.After(to) {
				latencies = append(latencies, sample)
			}
		}
	}

	return latencies, nil
}

func enrichSamplesWithTimestamps(samples []uint32, startTimestampMicroseconds, samplingIntervalMicroseconds uint64) []datastats.CircuitLatencySample {
	circuitLatencies := make([]datastats.CircuitLatencySample, 0, len(samples))
	for i, sample := range samples {
		timestampMicros := startTimestampMicroseconds + uint64(i)*samplingIntervalMicroseconds
		secs := int64(timestampMicros / 1_000_000)
		nanos := int64(timestampMicros%1_000_000) * 1000
		timestamp := time.Unix(secs, nanos)
		circuitLatencies = append(circuitLatencies, datastats.CircuitLatencySample{
			Timestamp: timestamp,
			RTT:       sample,
		})
	}
	return circuitLatencies
}
