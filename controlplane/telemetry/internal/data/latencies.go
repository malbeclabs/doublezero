package data

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/data"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

type CircuitLatencySample struct {
	Timestamp string `json:"timestamp"`
	RTT       uint32 `json:"rtt"`
}

type CircuitLatencyStat struct {
	Circuit   string `json:"circuit"`
	Timestamp string `json:"timestamp"` // Start timestamp of the window this stat represents, in RFC3339 format.

	// RTT metrics (in microseconds)
	RTTMean     float64 `json:"rtt_mean"`     // Arithmetic mean of all successful round-trip times (RTTs); overall latency average.
	RTTMedian   float64 `json:"rtt_median"`   // Median RTT; more robust to outliers than the mean.
	RTTMin      float64 `json:"rtt_min"`      // Minimum observed RTT in the window; represents best-case latency.
	RTTMax      float64 `json:"rtt_max"`      // Maximum observed RTT; reflects outliers or transient spikes.
	RTTP95      float64 `json:"rtt_p95"`      // 95th percentile RTT; indicates tail latency for most users.
	RTTP99      float64 `json:"rtt_p99"`      // 99th percentile RTT; captures rare extreme latency events.
	RTTStdDev   float64 `json:"rtt_stddev"`   // Standard deviation of RTTs; reflects latency variability over the window.
	RTTVariance float64 `json:"rtt_variance"` // Variance of RTTs (square of RTTStdDev); useful in modeling or advanced analysis.
	RTTMAD      float64 `json:"rtt_mad"`      // Mean absolute deviation of RTTs; measures deviation from the mean.

	// Jitter metrics (in microseconds)
	JitterAvg         float64 `json:"jitter_avg"`          // Average jitter between packets (IPDV mean); commonly reported as "jitter" in network systems.
	JitterEWMA        float64 `json:"jitter_ewma"`         // Smoothed jitter (RFC3550): exponentially-weighted moving average of per-packet RTT deltas.
	JitterDeltaStdDev float64 `json:"jitter_delta_stddev"` // Stddev of inter-packet RTT deltas (IPDV); measures jitter burstiness.
	JitterPeakToPeak  float64 `json:"jitter_peak_to_peak"` // Max-min RTT spread; worst-case jitter window.
	JitterMax         float64 `json:"jitter_max"`          // Maximum inter-packet jitter observed (max of |RTT[i] - RTT[i-1]|)

	// Success/failure counts and ratios
	SuccessCount uint64  `json:"success_count"` // Number of RTT samples with a valid (positive) value; successful responses.
	SuccessRate  float64 `json:"success_rate"`  // Proportion of successful responses out of total samples.
	LossCount    uint64  `json:"loss_count"`    // Number of RTT samples with zero/missing values; interpreted as loss or timeout.
	LossRate     float64 `json:"loss_rate"`     // Proportion of lost packets out of total samples.
}

func (p *provider) GetCircuitLatenciesForEpoch(ctx context.Context, circuitCode string, epoch uint64) ([]CircuitLatencySample, error) {
	cached := p.GetCachedCircuitLatencies(ctx, circuitCode, epoch)
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

	account, err := p.cfg.TelemetryClient.GetDeviceLatencySamples(ctx, circuit.OriginDevice.PK, circuit.TargetDevice.PK, circuit.Link.PK, epoch)
	if err != nil {
		if errors.Is(err, telemetry.ErrAccountNotFound) {
			// If the account is not found, cache an empty array for the epoch for a short time.
			p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, []CircuitLatencySample{}, p.cfg.CurrentEpochLatenciesCacheTTL)
			return nil, err
		}
		return nil, fmt.Errorf("failed to get device latency samples for epoch %d: %w", epoch, err)
	}

	samples := enrichSamplesWithTimestamps(account.Samples, account.StartTimestampMicroseconds, account.SamplingIntervalMicroseconds)

	// If the epoch is sufficiently in the past, cache for much longer.
	currentEpoch := data.DeriveEpoch(time.Now())
	ttl := p.cfg.CurrentEpochLatenciesCacheTTL
	if epoch < currentEpoch-1 {
		ttl = p.cfg.HistoricEpochLatenciesCacheTTL
	}
	p.SetCachedCircuitLatencies(ctx, circuitCode, epoch, samples, ttl)

	return samples, nil
}

func (p *provider) GetCircuitLatencies(ctx context.Context, circuitCode string, from, to time.Time) ([]CircuitLatencySample, error) {
	startEpoch := data.DeriveEpoch(from)
	endEpoch := data.DeriveEpoch(to)

	var latencies []CircuitLatencySample

	group := p.getCircuitLatenciesPool.NewGroupContext(ctx)

	for epoch := startEpoch; epoch <= endEpoch; epoch++ {
		epoch := epoch

		group.SubmitErr(func() ([]CircuitLatencySample, error) {
			result, err := p.GetCircuitLatenciesForEpoch(ctx, circuitCode, epoch)
			if errors.Is(err, telemetry.ErrAccountNotFound) {
				p.cfg.Logger.Info("Device latency samples not found, skipping", "epoch", epoch, "circuit", circuitCode)
				return []CircuitLatencySample{}, nil
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
) ([]CircuitLatencyStat, error) {
	latencies, err := p.GetCircuitLatencies(ctx, circuitCode, from, to)
	if err != nil {
		return nil, err
	}

	if maxPoints == 0 {
		maxPoints = 1
	}

	interval := time.Duration(int64(to.Sub(from)) / int64(maxPoints))
	interval = max(interval, time.Minute)

	buckets := make(map[time.Time][]float64)
	for _, pt := range latencies {
		t, err := time.Parse(time.RFC3339Nano, pt.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		key := from.Add(t.Sub(from).Truncate(interval)) // align buckets from `from`
		buckets[key] = append(buckets[key], float64(pt.RTT))
	}

	var result []CircuitLatencyStat

	if maxPoints == 1 {
		var all []float64
		for _, rtts := range buckets {
			all = append(all, rtts...)
		}
		if len(all) == 0 {
			return nil, nil
		}
		stats := computeStats(from, all)
		stats.Circuit = circuitCode
		result = []CircuitLatencyStat{stats}
	} else {
		for ts, rtts := range buckets {
			if len(rtts) == 0 {
				continue
			}
			stats := computeStats(ts, rtts)
			stats.Circuit = circuitCode
			result = append(result, stats)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Timestamp < result[j].Timestamp
		})
	}

	return result, nil
}

func enrichSamplesWithTimestamps(samples []uint32, startTimestampMicroseconds, samplingIntervalMicroseconds uint64) []CircuitLatencySample {
	circuitLatencies := make([]CircuitLatencySample, 0, len(samples))
	for i, sample := range samples {
		timestampMicros := startTimestampMicroseconds + uint64(i)*samplingIntervalMicroseconds
		secs := int64(timestampMicros / 1_000_000)
		nanos := int64(timestampMicros%1_000_000) * 1000
		timestamp := time.Unix(secs, nanos)
		circuitLatencies = append(circuitLatencies, CircuitLatencySample{
			Timestamp: timestamp.Format(time.RFC3339Nano),
			RTT:       sample,
		})
	}
	return circuitLatencies
}
