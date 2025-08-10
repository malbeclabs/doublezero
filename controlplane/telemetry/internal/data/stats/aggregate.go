package stats

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

type CircuitLatencySample struct {
	Timestamp string `json:"timestamp"`
	RTT       uint32 `json:"rtt"`
}

type AggregateOpts struct {
	Bucket   time.Duration
	FillGaps bool
}

func DownsampleCircuitLatencies(ctx context.Context, circuitCode string, from, to time.Time, maxPoints uint64, latencies []CircuitLatencySample) ([]CircuitLatencyStat, error) {
	span := to.Sub(from)
	if span <= 0 {
		return nil, nil
	}
	if maxPoints == 0 {
		maxPoints = 1
	}

	// Filter + sort once
	filtered := make([]CircuitLatencySample, 0, len(latencies))
	type tsPair struct {
		t time.Time
		i int
	}
	tmp := make([]tsPair, 0, len(latencies))
	for i, pt := range latencies {
		t, err := time.Parse(time.RFC3339Nano, pt.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		if t.Before(from) || t.After(to) {
			continue
		}
		tmp = append(tmp, tsPair{t: t, i: i})
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].t.Before(tmp[j].t) })
	for _, p := range tmp {
		filtered = append(filtered, latencies[p.i])
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	// FAST PATH: no downsample needed — 1 point → 1 stat
	if uint64(len(filtered)) <= maxPoints {
		out := make([]CircuitLatencyStat, 0, len(filtered))
		for _, pt := range filtered {
			t, _ := time.Parse(time.RFC3339Nano, pt.Timestamp)
			s := ComputeStats(t, []float64{float64(pt.RTT)})
			s.Circuit = circuitCode
			out = append(out, s)
		}
		applyCrossSeriesJitter(out, 0.2) // keep jitter meaningful across sparse points
		return out, nil
	}

	// Otherwise: aggregate into time buckets
	bucket := ceilDivDuration(span, maxPoints)
	return AggregateCircuitLatencies(ctx, circuitCode, from, to, filtered, AggregateOpts{Bucket: bucket, FillGaps: true})
}

func ceilDivDuration(d time.Duration, n uint64) time.Duration {
	if n == 0 {
		return d
	}
	q := d / time.Duration(n)
	if d%time.Duration(n) != 0 {
		q++
	}
	if q <= 0 {
		q = time.Nanosecond
	}
	return q
}

func AggregateCircuitLatencies(ctx context.Context, circuitCode string, from, to time.Time, latencies []CircuitLatencySample, opts AggregateOpts) ([]CircuitLatencyStat, error) {
	span := to.Sub(from)
	if span <= 0 {
		return nil, nil
	}
	if opts.Bucket <= 0 {
		return nil, fmt.Errorf("bucket must be > 0")
	}

	nBuckets := int(span / opts.Bucket)
	if span%opts.Bucket != 0 {
		nBuckets++
	}
	if nBuckets < 1 {
		nBuckets = 1
	}

	buckets := make([][]float64, nBuckets)
	starts := make([]time.Time, nBuckets)
	for i := 0; i < nBuckets; i++ {
		starts[i] = from.Add(time.Duration(i) * opts.Bucket)
	}

	for _, pt := range latencies {
		t, err := time.Parse(time.RFC3339Nano, pt.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp: %w", err)
		}
		if t.Before(from) || t.After(to) {
			continue
		}
		idx := int(t.Sub(from) / opts.Bucket)
		if idx >= nBuckets {
			idx = nBuckets - 1
		}
		buckets[idx] = append(buckets[idx], float64(pt.RTT))
	}

	out := make([]CircuitLatencyStat, 0, nBuckets)
	for i := 0; i < nBuckets; i++ {
		if len(buckets[i]) == 0 {
			if opts.FillGaps {
				s := ComputeStats(starts[i], nil)
				s.Circuit = circuitCode
				out = append(out, s)
			}
			continue
		}
		s := ComputeStats(starts[i], buckets[i])
		s.Circuit = circuitCode
		out = append(out, s)
	}
	return out, nil
}

// EWMA of |Δ bucket mean| across the series so jitter stays informative with 0/1-sample buckets
func applyCrossSeriesJitter(xs []CircuitLatencyStat, alpha float64) {
	if alpha <= 0 || alpha > 1 || len(xs) == 0 {
		return
	}
	var havePrev bool
	var prevMean, ewma float64
	for i := range xs {
		m := xs[i].RTTMean
		if havePrev {
			d := math.Abs(m - prevMean)
			if i == 1 {
				ewma = d
			} else {
				ewma = alpha*d + (1-alpha)*ewma
			}
			xs[i].JitterAvg = d
			xs[i].JitterEWMA = ewma
		}
		prevMean = m
		havePrev = true
	}
}
