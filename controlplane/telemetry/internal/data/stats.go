package data

import (
	"math"
	"sort"
	"time"
)

func computeStats(ts time.Time, rtts []float64) CircuitLatencyStat {
	var successRTTs []float64
	var success, lossCount uint64

	for _, rtt := range rtts {
		if rtt > 0 {
			successRTTs = append(successRTTs, rtt)
			success++
		} else {
			lossCount++
		}
	}

	total := success + lossCount
	if total == 0 || len(successRTTs) == 0 {
		var lossRate float64
		if total > 0 {
			lossRate = float64(lossCount) / float64(total)
		}
		return CircuitLatencyStat{
			Timestamp:         ts.Format(time.RFC3339),
			RTTMean:           0,
			RTTMedian:         0,
			RTTMin:            0,
			RTTMax:            0,
			RTTP95:            0,
			RTTP99:            0,
			RTTStdDev:         0,
			RTTVariance:       0,
			RTTMAD:            0,
			JitterAvg:         0,
			JitterEWMA:        0,
			JitterDeltaStdDev: 0,
			JitterPeakToPeak:  0,
			JitterMax:         0,
			SuccessCount:      0,
			SuccessRate:       0,
			LossCount:         lossCount,
			LossRate:          lossRate,
		}
	}

	sort.Float64s(successRTTs)
	n := float64(len(successRTTs))

	var sum, sumSq, mad float64
	var ipdvs []float64
	var smoothedIPDV float64
	var maxIPDV float64

	min := successRTTs[0]
	max := successRTTs[len(successRTTs)-1]

	var median float64
	mid := len(successRTTs) / 2
	if len(successRTTs)%2 == 0 {
		median = (successRTTs[mid-1] + successRTTs[mid]) / 2
	} else {
		median = successRTTs[mid]
	}

	for i, v := range successRTTs {
		sum += v
		sumSq += v * v
		if i > 0 {
			delta := math.Abs(v - successRTTs[i-1])
			ipdvs = append(ipdvs, delta)
			smoothedIPDV += (delta - smoothedIPDV) / 16
			if delta > maxIPDV {
				maxIPDV = delta
			}
		}
	}
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	stddev := math.Sqrt(variance)
	for _, v := range successRTTs {
		mad += math.Abs(v - mean)
	}
	mad /= n
	p95 := successRTTs[int(math.Ceil(0.95*n))-1]
	p99 := successRTTs[int(math.Ceil(0.99*n))-1]

	var ipdvMean, ipdvStddev float64
	if len(ipdvs) > 0 {
		var ipdvSum, ipdvSumSq float64
		for _, d := range ipdvs {
			ipdvSum += d
			ipdvSumSq += d * d
		}
		ipdvMean = ipdvSum / float64(len(ipdvs))
		ipdvStddev = math.Sqrt((ipdvSumSq / float64(len(ipdvs))) - (ipdvMean * ipdvMean))
	}

	return CircuitLatencyStat{
		Timestamp:         ts.Format(time.RFC3339),
		RTTMean:           mean,
		RTTMedian:         median,
		RTTMin:            min,
		RTTMax:            max,
		RTTP95:            p95,
		RTTP99:            p99,
		RTTStdDev:         stddev,
		RTTVariance:       variance,
		RTTMAD:            mad,
		JitterAvg:         ipdvMean,
		JitterEWMA:        smoothedIPDV,
		JitterDeltaStdDev: ipdvStddev,
		JitterPeakToPeak:  max - min,
		JitterMax:         maxIPDV,
		SuccessCount:      success,
		SuccessRate:       float64(success) / float64(total),
		LossCount:         lossCount,
		LossRate:          float64(lossCount) / float64(total),
	}
}
