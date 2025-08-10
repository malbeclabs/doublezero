package data

import (
	"math"
	"sort"
	"time"
)

func computeStats(ts time.Time, rtts []float64) CircuitLatencyStat {
	// Keep original order for jitter/IPDV; count losses (rtt<=0) separately.
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
		return CircuitLatencyStat{
			Timestamp: ts.Format(time.RFC3339),
			LossCount: lossCount,
			LossRate:  lossRate,
		}
	}

	// Sorted copy for summary stats (min/max/percentiles/mean/stddev).
	sorted := append([]float64(nil), ordered...)
	sort.Float64s(sorted)
	n := float64(len(sorted))

	min, max := sorted[0], sorted[len(sorted)-1]

	// Median
	mid := len(sorted) / 2
	var median float64
	if len(sorted)%2 == 0 {
		median = (sorted[mid-1] + sorted[mid]) / 2
	} else {
		median = sorted[mid]
	}

	// Mean, variance (numerically safe), stddev
	var sum, sumSq float64
	for _, v := range sorted {
		sum += v
		sumSq += v * v
	}
	mean := sum / n
	variance := (sumSq / n) - mean*mean
	if variance < 0 && variance > -1e-12 {
		variance = 0
	} // clamp tiny negatives
	stddev := math.Sqrt(variance)

	// Median Absolute Deviation (MAD) from the median (robust)
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

	// Percentiles: nearest-rank with ceil(p*n)-1
	p95 := sorted[int(math.Ceil(0.95*n))-1]
	p99 := sorted[int(math.Ceil(0.99*n))-1]

	// IPDV/jitter from ORIGINAL time order.
	var deltas, absDeltas []float64
	var ewma float64
	var maxAbs, minAbs float64
	if len(ordered) > 1 {
		// Seed EWMA with first |ΔRTT| to avoid cold-start bias
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

			// RFC3550-style smoothing with α=1/16
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

	// JitterAvg = mean(|ΔRTT|)
	// JitterDeltaStdDev = standard deviation of signed ΔRTT (not RMS)
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
		Timestamp: ts.Format(time.RFC3339),

		// RTT stats (from sorted data)
		RTTMean:     mean,
		RTTMedian:   median,
		RTTMin:      min,
		RTTMax:      max,
		RTTP95:      p95,
		RTTP99:      p99,
		RTTStdDev:   stddev,
		RTTVariance: variance,
		RTTMAD:      mad, // median(|RTT - median(RTT)|)

		// Jitter/IPDV stats (from original order)
		JitterAvg:         ipdvMeanAbs,
		JitterEWMA:        ewma,
		JitterDeltaStdDev: ipdvStddev,
		JitterPeakToPeak:  maxAbs - minAbs, // over |ΔRTT|
		JitterMax:         maxAbs,

		// Success/loss
		SuccessCount: success,
		SuccessRate:  float64(success) / float64(total),
		LossCount:    lossCount,
		LossRate:     float64(lossCount) / float64(total),
	}
}
