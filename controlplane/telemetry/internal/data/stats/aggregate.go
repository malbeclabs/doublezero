package stats

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type CircuitLatencySample struct {
	Timestamp time.Time `json:"timestamp"`
	RTT       uint32    `json:"rtt"`
}

type CircuitLatencyStat struct {
	Circuit   string `json:"circuit"`
	Timestamp string `json:"timestamp"` // Start of window (RFC3339Nano)

	// RTT metrics (in microseconds)
	RTTMean     float64 `json:"rtt_mean"`
	RTTMedian   float64 `json:"rtt_median"`
	RTTMin      float64 `json:"rtt_min"`
	RTTMax      float64 `json:"rtt_max"`
	RTTP90      float64 `json:"rtt_p90"`
	RTTP95      float64 `json:"rtt_p95"`
	RTTP99      float64 `json:"rtt_p99"`
	RTTStdDev   float64 `json:"rtt_stddev"`   // population
	RTTVariance float64 `json:"rtt_variance"` // population
	RTTMAD      float64 `json:"rtt_mad"`      // median absolute deviation

	// Jitter metrics (in microseconds)
	JitterAvg         float64 `json:"jitter_avg"`          // mean(|ΔRTT|)
	JitterEWMA        float64 `json:"jitter_ewma"`         // RFC3550-like EWMA of |ΔRTT|
	JitterDeltaStdDev float64 `json:"jitter_delta_stddev"` // stddev of ΔRTT
	JitterPeakToPeak  float64 `json:"jitter_peak_to_peak"` // range of |ΔRTT| (max-min)
	JitterMax         float64 `json:"jitter_max"`          // max(|ΔRTT|)

	// Success/failure counts and ratios
	SuccessCount uint64  `json:"success_count"`
	SuccessRate  float64 `json:"success_rate"`
	LossCount    uint64  `json:"loss_count"`
	LossRate     float64 `json:"loss_rate"`
}

func (s *CircuitLatencyStat) ConvertUnit(factor float64) {
	s.RTTMean /= factor
	s.RTTMedian /= factor
	s.RTTMin /= factor
	s.RTTMax /= factor
	s.RTTP90 /= factor
	s.RTTP95 /= factor
	s.RTTP99 /= factor
	s.RTTStdDev /= factor
	s.RTTVariance /= (factor * factor)
	s.RTTMAD /= factor
	s.JitterAvg /= factor
	s.JitterEWMA /= factor
	s.JitterDeltaStdDev /= factor
	s.JitterPeakToPeak /= factor
	s.JitterMax /= factor
}

func Aggregate(circuitCode string, samples []CircuitLatencySample, maxPoints uint64, interval time.Duration) ([]CircuitLatencyStat, error) {
	if len(samples) == 0 {
		return []CircuitLatencyStat{}, nil
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].Timestamp.Before(samples[j].Timestamp) })
	firstTime := samples[0].Timestamp
	lastTime := samples[len(samples)-1].Timestamp

	const tf = time.RFC3339Nano

	// Per-sample path (also handles == len(samples))
	if interval == 0 && (maxPoints == 0 || maxPoints >= uint64(len(samples))) {
		stats := make([]CircuitLatencyStat, len(samples))
		var lastGoodRTT *float64
		for i, sample := range samples {
			if sample.RTT == 0 {
				stats[i] = CircuitLatencyStat{
					Circuit:   circuitCode,
					Timestamp: sample.Timestamp.Format(tf),
					LossCount: 1, LossRate: 1,
				}
				continue
			}
			r := float64(sample.RTT)
			s := CircuitLatencyStat{
				Circuit:      circuitCode,
				Timestamp:    sample.Timestamp.Format(tf),
				RTTMean:      r,
				RTTMedian:    r,
				RTTMin:       r,
				RTTMax:       r,
				RTTP90:       r,
				RTTP95:       r,
				RTTP99:       r,
				RTTStdDev:    0,
				RTTVariance:  0,
				RTTMAD:       0,
				SuccessCount: 1,
				SuccessRate:  1,
			}
			if lastGoodRTT != nil {
				d := r - *lastGoodRTT
				ad := math.Abs(d)
				s.JitterAvg = ad
				s.JitterEWMA = ad
				s.JitterDeltaStdDev = 0
				s.JitterPeakToPeak = 0
				s.JitterMax = ad
			}
			stats[i] = s
			lastGoodRTT = &r
		}
		return stats, nil
	}

	if maxPoints == 1 {
		rtts := make([]float64, 0, len(samples))
		for _, sample := range samples {
			rtts = append(rtts, float64(sample.RTT))
		}
		stats := AggregateIntoOne(firstTime, rtts)
		stats.Circuit = circuitCode
		return []CircuitLatencyStat{stats}, nil
	}

	if interval > 0 {
		stats, err := AggregateIntoTimeBuckets(circuitCode, samples, interval, true)
		if err != nil {
			return nil, fmt.Errorf("failed to downsample circuit latencies: %w", err)
		}
		return stats, nil
	}

	if maxPoints > 1 && maxPoints < uint64(len(samples)) {
		span := lastTime.Sub(firstTime)
		q := float64(span) / float64(maxPoints)
		if q < 1 {
			q = 1
		}
		bucket := time.Duration(math.Ceil(q))
		stats, err := AggregateIntoTimeBuckets(circuitCode, samples, bucket, true)
		if err != nil {
			return nil, fmt.Errorf("failed to downsample circuit latencies: %w", err)
		}
		return stats, nil
	}

	return nil, fmt.Errorf("invalid max points: %d", maxPoints)
}

func AggregateIntoTimeBuckets(circuitCode string, timeseries []CircuitLatencySample, bucket time.Duration, fillGaps bool) ([]CircuitLatencyStat, error) {
	if len(timeseries) == 0 {
		return []CircuitLatencyStat{}, nil
	}

	from := timeseries[0].Timestamp
	to := timeseries[len(timeseries)-1].Timestamp
	span := to.Sub(from)
	if span <= 0 {
		return []CircuitLatencyStat{}, nil
	}
	if bucket <= 0 {
		return nil, fmt.Errorf("bucket must be > 0")
	}

	nBuckets := int(span / bucket)
	if span%bucket != 0 {
		nBuckets++
	}
	if nBuckets < 1 {
		nBuckets = 1
	}

	buckets := make([][]float64, nBuckets)
	starts := make([]time.Time, nBuckets)
	for i := 0; i < nBuckets; i++ {
		starts[i] = from.Add(time.Duration(i) * bucket)
	}

	for _, pt := range timeseries {
		idx := int(pt.Timestamp.Sub(from) / bucket)
		if idx >= nBuckets {
			idx = nBuckets - 1
		}
		buckets[idx] = append(buckets[idx], float64(pt.RTT))
	}

	// Track first/last successful RTT in each bucket to enable cross-bucket jitter when a bucket
	// has exactly one success.
	type ends struct{ first, last *float64 }
	endsByBucket := make([]ends, nBuckets)
	for i := 0; i < nBuckets; i++ {
		for _, v := range buckets[i] {
			if v > 0 {
				vv := v
				if endsByBucket[i].first == nil {
					endsByBucket[i].first = &vv
				}
				endsByBucket[i].last = &vv
			}
		}
	}

	out := make([]CircuitLatencyStat, 0, nBuckets)
	var prevLastGood *float64
	for i := 0; i < nBuckets; i++ {
		if len(buckets[i]) == 0 {
			if fillGaps {
				s := AggregateIntoOne(starts[i], nil)
				s.Circuit = circuitCode
				out = append(out, s)
			}
			continue
		}
		s := AggregateIntoOne(starts[i], buckets[i])
		s.Circuit = circuitCode

		// If this bucket has exactly one success and we have a previous good RTT, synthesize jitter
		// against the last good RTT from the previous non-empty bucket.
		if s.SuccessCount == 1 && prevLastGood != nil && endsByBucket[i].first != nil {
			d := math.Abs(*endsByBucket[i].first - *prevLastGood)
			s.JitterAvg = d
			s.JitterEWMA = d
			s.JitterDeltaStdDev = 0
			s.JitterPeakToPeak = 0
			s.JitterMax = d
		}

		out = append(out, s)
		if endsByBucket[i].last != nil {
			prevLastGood = endsByBucket[i].last
		}
	}
	return out, nil
}

func AggregateIntoOne(ts time.Time, rtts []float64) CircuitLatencyStat {
	const tf = time.RFC3339Nano

	var ordered []float64
	var success, lossCount uint64
	for _, rtt := range rtts {
		if rtt > 0 {
			ordered = append(ordered, rtt)
			success++
		} else {
			lossCount++
		}
	}
	total := success + lossCount
	if total == 0 || len(ordered) == 0 {
		var lossRate float64
		if total > 0 {
			lossRate = float64(lossCount) / float64(total)
		}
		return CircuitLatencyStat{Timestamp: ts.Format(tf), LossCount: lossCount, LossRate: lossRate}
	}

	sorted := append([]float64(nil), ordered...)
	sort.Float64s(sorted)
	n := float64(len(sorted))
	min, max := sorted[0], sorted[len(sorted)-1]

	mid := len(sorted) / 2
	var median float64
	if len(sorted)%2 == 0 {
		median = (sorted[mid-1] + sorted[mid]) / 2
	} else {
		median = sorted[mid]
	}

	var sum, sumSq float64
	for _, v := range sorted {
		sum += v
		sumSq += v * v
	}

	// Mean/variance with Welford's algorithm (population)
	var mean, m2 float64
	for i, v := range sorted {
		delta := v - mean
		mean += delta / float64(i+1)
		m2 += delta * (v - mean)
	}
	variance := m2 / float64(len(sorted))
	if variance < 0 {
		variance = 0
	} // unconditional clamp; we don't want NaNs
	stddev := math.Sqrt(variance)

	res := make([]float64, len(sorted))
	for i, v := range sorted {
		res[i] = math.Abs(v - median)
	}
	sort.Float64s(res)
	var mad float64
	rm := len(res) / 2
	if len(res)%2 == 0 {
		mad = (res[rm-1] + res[rm]) / 2
	} else {
		mad = res[rm]
	}

	p90 := sorted[int(math.Ceil(0.90*n))-1]
	p95 := sorted[int(math.Ceil(0.95*n))-1]
	p99 := sorted[int(math.Ceil(0.99*n))-1]

	var deltas, absDeltas []float64
	var ewma float64
	var maxAbs, minAbs float64
	if len(ordered) > 1 {
		firstAbs := math.Abs(ordered[1] - ordered[0])
		ewma = firstAbs
		maxAbs, minAbs = firstAbs, firstAbs
		deltas = append(deltas, ordered[1]-ordered[0])
		absDeltas = append(absDeltas, firstAbs)
		for i := 2; i < len(ordered); i++ {
			d := ordered[i] - ordered[i-1]
			ad := math.Abs(d)
			deltas = append(deltas, d)
			absDeltas = append(absDeltas, ad)
			ewma += (ad - ewma) / 16
			if ad > maxAbs {
				maxAbs = ad
			}
			if ad < minAbs {
				minAbs = ad
			}
		}
	} else {
		minAbs, maxAbs = 0, 0
	}

	var ipdvMeanAbs, ipdvStddev float64
	if len(deltas) > 0 {
		var sAbs, mu, s2 float64
		for _, d := range deltas {
			mu += d
		}
		mu /= float64(len(deltas))
		for i, d := range deltas {
			sAbs += absDeltas[i]
			diff := d - mu
			s2 += diff * diff
		}
		ipdvMeanAbs = sAbs / float64(len(absDeltas))
		ipdvStddev = math.Sqrt(s2 / float64(len(deltas)))
	}

	return CircuitLatencyStat{
		Timestamp: ts.Format(tf),

		RTTMean:     mean,
		RTTMedian:   median,
		RTTMin:      min,
		RTTMax:      max,
		RTTP90:      p90,
		RTTP95:      p95,
		RTTP99:      p99,
		RTTStdDev:   stddev,
		RTTVariance: variance,
		RTTMAD:      mad,

		JitterAvg:         ipdvMeanAbs,
		JitterEWMA:        ewma,
		JitterDeltaStdDev: ipdvStddev,
		JitterPeakToPeak:  maxAbs - minAbs,
		JitterMax:         maxAbs,

		SuccessCount: success,
		SuccessRate:  float64(success) / float64(total),
		LossCount:    lossCount,
		LossRate:     float64(lossCount) / float64(total),
	}
}
