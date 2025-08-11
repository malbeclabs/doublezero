package stats

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func near(t *testing.T, got, want, tol float64) { assert.InDelta(t, want, got, tol) }
func ts(base time.Time, i int, step time.Duration) time.Time {
	return base.Add(time.Duration(i) * step)
}

func TestTelemetry_Data_ConvertUnit(t *testing.T) {
	t.Parallel()
	s := CircuitLatencyStat{
		RTTMean: 2000, RTTMedian: 2000, RTTMin: 1000, RTTMax: 3000,
		RTTP90: 2500, RTTP95: 2800, RTTP99: 2999,
		RTTStdDev: 100, RTTVariance: 10000, RTTMAD: 50,
		JitterAvg: 20, JitterEWMA: 15, JitterDeltaStdDev: 5, JitterPeakToPeak: 8, JitterMax: 6,
	}
	s.ConvertUnit(1000)
	near(t, s.RTTMean, 2, 1e-9)
	near(t, s.RTTMedian, 2, 1e-9)
	near(t, s.RTTMin, 1, 1e-9)
	near(t, s.RTTMax, 3, 1e-9)
	near(t, s.RTTP90, 2.5, 1e-9)
	near(t, s.RTTP95, 2.8, 1e-9)
	near(t, s.RTTP99, 2.999, 1e-9)
	near(t, s.RTTStdDev, 0.1, 1e-9)
	near(t, s.RTTVariance, 0.01, 1e-12)
	near(t, s.RTTMAD, 0.05, 1e-9)
	near(t, s.JitterAvg, 0.02, 1e-9)
	near(t, s.JitterEWMA, 0.015, 1e-9)
	near(t, s.JitterDeltaStdDev, 0.005, 1e-9)
	near(t, s.JitterPeakToPeak, 0.008, 1e-9)
	near(t, s.JitterMax, 0.006, 1e-9)
}

func TestTelemetry_Data_Aggregate(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 123_456_789)

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := Aggregate("C1", nil, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("per_sample_with_losses_and_jitter", func(t *testing.T) {
		t.Parallel()
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, time.Second), RTT: 1000},
			{Timestamp: ts(now, 1, time.Second), RTT: 0},
			{Timestamp: ts(now, 2, time.Second), RTT: 1500},
			{Timestamp: ts(now, 3, time.Second), RTT: 1700},
		}
		out, err := Aggregate("C1", samples, 0, 0)
		require.NoError(t, err)
		require.Len(t, out, 4)

		assert.Equal(t, "C1", out[0].Circuit)
		assert.Equal(t, samples[0].Timestamp.Format(time.RFC3339Nano), out[0].Timestamp)
		near(t, out[0].RTTMean, 1000, 0)
		near(t, out[0].RTTStdDev, 0, 0)
		near(t, out[0].JitterAvg, 0, 0)
		assert.False(t, math.IsNaN(out[0].JitterDeltaStdDev))
		assert.Equal(t, uint64(1), out[0].SuccessCount)
		near(t, out[0].SuccessRate, 1, 0)

		assert.Equal(t, uint64(1), out[1].LossCount)
		near(t, out[1].LossRate, 1, 0)

		near(t, out[2].RTTMean, 1500, 0)
		near(t, out[2].JitterAvg, 500, 0)
		assert.True(t, out[2].JitterDeltaStdDev >= 0) // impl may set |Δ| or 0 for a single delta
		near(t, out[2].JitterMax, 500, 0)

		near(t, out[3].RTTMean, 1700, 0)
		near(t, out[3].JitterAvg, 200, 0)
		assert.True(t, out[3].JitterDeltaStdDev >= 0)
	})

	t.Run("maxPoints_equals_len_per_sample", func(t *testing.T) {
		t.Parallel()
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, time.Millisecond), RTT: 1000},
			{Timestamp: ts(now, 1, time.Millisecond), RTT: 2000},
		}
		out, err := Aggregate("C", samples, uint64(len(samples)), 0)
		require.NoError(t, err)
		require.Len(t, out, 2)
		near(t, out[1].JitterAvg, 1000, 0)
		assert.True(t, out[1].JitterDeltaStdDev >= 0)
	})

	t.Run("single_point_aggregation", func(t *testing.T) {
		t.Parallel()
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, time.Second), RTT: 1000},
			{Timestamp: ts(now, 1, time.Second), RTT: 2000},
			{Timestamp: ts(now, 2, time.Second), RTT: 3000},
			{Timestamp: ts(now, 3, time.Second), RTT: 0},
		}
		out, err := Aggregate("C2", samples, 1, 0)
		require.NoError(t, err)
		require.Len(t, out, 1)
		s := out[0]
		near(t, s.RTTMean, (1000+2000+3000)/3.0, 1e-9)
		near(t, s.RTTMedian, 2000, 1e-9)
		near(t, s.RTTMin, 1000, 0)
		near(t, s.RTTMax, 3000, 0)
		near(t, s.RTTP90, 3000, 0)
		near(t, s.RTTP95, 3000, 0)
		near(t, s.RTTP99, 3000, 0)
		near(t, s.SuccessRate, 0.75, 1e-9)
		near(t, s.LossRate, 0.25, 1e-9)
		near(t, s.JitterAvg, (1000+1000)/2.0, 1e-9)
		assert.True(t, s.JitterDeltaStdDev >= 0)
	})

	t.Run("downsample_time_buckets_with_gap_fill", func(t *testing.T) {
		t.Parallel()
		step := 2 * time.Millisecond
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, step), RTT: 1000},
			{Timestamp: ts(now, 1, step), RTT: 1000},
			{Timestamp: ts(now, 2, step), RTT: 0},
			{Timestamp: ts(now, 3, step), RTT: 3000},
			{Timestamp: ts(now, 8, step), RTT: 4000},
		}
		out, err := Aggregate("C3", samples, 3, 0)
		require.NoError(t, err)
		require.NotEmpty(t, out)
		for i := 1; i < len(out); i++ {
			require.LessOrEqual(t, out[i-1].Timestamp, out[i].Timestamp)
			_, err := time.Parse(time.RFC3339Nano, out[i].Timestamp)
			require.NoError(t, err)
		}
	})

	t.Run("tiny_span_many_points_bucket_never_zero", func(t *testing.T) {
		t.Parallel()
		samples := make([]CircuitLatencySample, 10)
		base := now
		for i := range samples {
			samples[i] = CircuitLatencySample{Timestamp: base.Add(time.Duration(i) * time.Nanosecond), RTT: 100}
		}
		_, err := Aggregate("C", samples, 1000, 0)
		require.NoError(t, err)
	})

	t.Run("interval_sparse_singletons_carryover_jitter", func(t *testing.T) {
		t.Parallel()
		samples := []CircuitLatencySample{
			{Timestamp: now, RTT: 2000},
			{Timestamp: now.Add(91 * time.Second), RTT: 500}, // 91s → span>90 → 4 buckets
		}
		out, err := Aggregate("Cj", samples, 0, 30*time.Second)
		require.NoError(t, err)
		require.Len(t, out, 4)

		t.Run("bucket0_single_no_prev", func(t *testing.T) {
			near(t, out[0].RTTMean, 2000, 0)
			near(t, out[0].JitterAvg, 0, 0)
		})
		t.Run("bucket1_gap_fill", func(t *testing.T) {
			assert.Equal(t, uint64(0), out[1].SuccessCount)
			assert.Equal(t, uint64(0), out[1].LossCount)
		})
		t.Run("bucket2_gap_fill", func(t *testing.T) {
			assert.Equal(t, uint64(0), out[2].SuccessCount)
			assert.Equal(t, uint64(0), out[2].LossCount)
		})
		t.Run("bucket3_single_with_carryover", func(t *testing.T) {
			near(t, out[3].RTTMean, 500, 0)
			near(t, out[3].JitterAvg, 1500, 0)
			near(t, out[3].JitterEWMA, 1500, 0)
			near(t, out[3].JitterDeltaStdDev, 0, 0)
			near(t, out[3].JitterPeakToPeak, 0, 0)
			near(t, out[3].JitterMax, 1500, 0)
		})
	})
}

func TestTelemetry_Data_AggregateIntoTimeBuckets(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_100, 0)

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := AggregateIntoTimeBuckets("C", nil, time.Millisecond, true)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("no_span_or_bad_bucket", func(t *testing.T) {
		t.Parallel()
		// Ensure span>0 so the function evaluates the bucket validity path.
		series := []CircuitLatencySample{
			{Timestamp: now, RTT: 100},
			{Timestamp: now.Add(time.Millisecond), RTT: 200},
		}
		out, err := AggregateIntoTimeBuckets("C", series, -time.Millisecond, true)
		require.Error(t, err)
		assert.Nil(t, out)

		// span<=0 short-circuits to empty slice + nil error
		out, err = AggregateIntoTimeBuckets("C", []CircuitLatencySample{{Timestamp: now, RTT: 100}}, time.Millisecond, true)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("gap_fill_true_vs_false", func(t *testing.T) {
		t.Parallel()
		// Create an interior gap: buckets [0..20), [20..40), [40..60); samples at 0ms and 60ms,
		// and one at 40ms goes to bucket #2, leaving bucket #1 empty.
		series := []CircuitLatencySample{
			{Timestamp: now.Add(0), RTT: 1000},                     // bucket 0
			{Timestamp: now.Add(40 * time.Millisecond), RTT: 2000}, // bucket 2
			{Timestamp: now.Add(60 * time.Millisecond), RTT: 3000}, // clamped to bucket 2 (right edge)
		}
		outFill, err := AggregateIntoTimeBuckets("C", series, 20*time.Millisecond, true)
		require.NoError(t, err)
		require.Len(t, outFill, 3) // [0..20), [20..40) (gap filled), [40..60)
		assert.Greater(t, outFill[0].SuccessCount, uint64(0))
		assert.Equal(t, uint64(0), outFill[1].SuccessCount) // gap filled with zeros
		assert.Equal(t, uint64(0), outFill[1].LossCount)

		outNoFill, err := AggregateIntoTimeBuckets("C", series, 20*time.Millisecond, false)
		require.NoError(t, err)
		require.Len(t, outNoFill, 2) // gap removed
	})

	t.Run("right_edge_clamped_to_last_bucket", func(t *testing.T) {
		t.Parallel()
		series := []CircuitLatencySample{
			{Timestamp: now.Add(0), RTT: 1000},
			{Timestamp: now.Add(20 * time.Millisecond), RTT: 2000}, // exactly on edge -> clamped to last bucket
			{Timestamp: now.Add(39 * time.Millisecond), RTT: 3000},
		}
		out, err := AggregateIntoTimeBuckets("C", series, 20*time.Millisecond, true)
		require.NoError(t, err)
		require.Len(t, out, 2)
	})

	t.Run("sparse_singletons_carryover_jitter", func(t *testing.T) {
		t.Parallel()
		series := []CircuitLatencySample{
			{Timestamp: now, RTT: 1000},
			{Timestamp: now.Add(61 * time.Second), RTT: 2500}, // 61s → span>60 → 3 buckets
		}
		out, err := AggregateIntoTimeBuckets("C", series, 30*time.Second, true)
		require.NoError(t, err)
		require.Len(t, out, 3)

		t.Run("bucket0_single_no_prev", func(t *testing.T) {
			near(t, out[0].RTTMean, 1000, 0)
			near(t, out[0].JitterAvg, 0, 0)
			near(t, out[0].JitterEWMA, 0, 0)
		})
		t.Run("bucket1_gap_fill", func(t *testing.T) {
			assert.Equal(t, uint64(0), out[1].SuccessCount)
			assert.Equal(t, uint64(0), out[1].LossCount)
		})
		t.Run("bucket2_single_with_carryover", func(t *testing.T) {
			near(t, out[2].RTTMean, 2500, 0)
			near(t, out[2].JitterAvg, 1500, 0)
			near(t, out[2].JitterEWMA, 1500, 0)
			near(t, out[2].JitterDeltaStdDev, 0, 0)
			near(t, out[2].JitterPeakToPeak, 0, 0)
			near(t, out[2].JitterMax, 1500, 0)
		})
	})
}

func TestTelemetry_Data_AggregateIntoOne(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_200, 0)

	t.Run("all_loss", func(t *testing.T) {
		t.Parallel()
		s := AggregateIntoOne(now, []float64{0, 0, 0})
		assert.Equal(t, uint64(0), s.SuccessCount)
		assert.Equal(t, uint64(3), s.LossCount)
		near(t, s.LossRate, 1, 0)
	})

	t.Run("single_value", func(t *testing.T) {
		t.Parallel()
		s := AggregateIntoOne(now, []float64{1500})
		near(t, s.RTTMean, 1500, 0)
		near(t, s.RTTStdDev, 0, 0)
		near(t, s.RTTMAD, 0, 0)
		near(t, s.JitterAvg, 0, 0)
	})

	t.Run("monotone_sequence_jitter_and_percentiles", func(t *testing.T) {
		t.Parallel()
		s := AggregateIntoOne(now, []float64{1000, 2000, 3000, 4000})
		near(t, s.RTTMean, 2500, 1e-9)
		near(t, s.RTTMedian, (2000+3000)/2.0, 1e-9)
		near(t, s.RTTP90, 4000, 0)
		near(t, s.RTTP95, 4000, 0)
		near(t, s.RTTP99, 4000, 0)
		near(t, s.JitterAvg, (1000+1000+1000)/3.0, 1e-9)
		near(t, s.JitterDeltaStdDev, 0, 0)
		near(t, s.JitterMax, 1000, 0)
		near(t, s.JitterPeakToPeak, 0, 0)
	})

	t.Run("variance_numerical_stability_non_negative", func(t *testing.T) {
		t.Parallel()
		// For identical values, variance should be ~0. Accept tiny negative due to fp and ensure stddev is finite and ~0.
		s := AggregateIntoOne(now, []float64{1234.5678, 1234.5678, 1234.5678, 1234.5678})
		assert.False(t, math.IsNaN(s.RTTStdDev))
		near(t, s.RTTVariance, 0, 1e-9)
		near(t, s.RTTStdDev, 0, 1e-9)
	})

	t.Run("interval_bucketed_gap_fill", func(t *testing.T) {
		t.Parallel()
		now := time.Unix(1_700_000_000, 0)
		step := 5 * time.Millisecond
		interval := 10 * time.Millisecond
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, step), RTT: 1000}, // 0ms -> bucket [0..10)
			// gap in [10..20)
			{Timestamp: ts(now, 5, step), RTT: 2000}, // 25ms -> bucket [20..30)
		}
		out, err := Aggregate("C_int", samples, 0, interval)
		require.NoError(t, err)
		require.Len(t, out, 3)

		assert.Equal(t, "C_int", out[0].Circuit)
		assert.Equal(t, samples[0].Timestamp.Format(time.RFC3339Nano), out[0].Timestamp)
		assert.Greater(t, out[0].SuccessCount, uint64(0))

		// Middle bucket (gap) starts at 10ms from the first timestamp
		expectMid := samples[0].Timestamp.Add(10 * time.Millisecond).Format(time.RFC3339Nano)
		assert.Equal(t, expectMid, out[1].Timestamp)
		assert.Equal(t, uint64(0), out[1].SuccessCount)
		assert.Equal(t, uint64(0), out[1].LossCount)

		// Last bucket starts at 20ms and contains the 25ms sample
		expectLast := samples[0].Timestamp.Add(20 * time.Millisecond).Format(time.RFC3339Nano)
		assert.Equal(t, expectLast, out[2].Timestamp)
		assert.Greater(t, out[2].SuccessCount, uint64(0))
	})

	t.Run("interval_ignored_when_maxPoints_1", func(t *testing.T) {
		t.Parallel()
		now := time.Unix(1_700_000_050, 0)
		samples := []CircuitLatencySample{
			{Timestamp: ts(now, 0, time.Second), RTT: 1000},
			{Timestamp: ts(now, 1, time.Second), RTT: 2000},
			{Timestamp: ts(now, 2, time.Second), RTT: 3000},
			{Timestamp: ts(now, 3, time.Second), RTT: 4000},
			{Timestamp: ts(now, 4, time.Second), RTT: 5000},
		}
		out, err := Aggregate("C_int2", samples, 1, 2*time.Second)
		require.NoError(t, err)

		// With maxPoints==1, we should *not* bucket by interval; single aggregated point expected.
		require.Len(t, out, 1)
		assert.Equal(t, samples[0].Timestamp.Format(time.RFC3339Nano), out[0].Timestamp)
		near(t, out[0].RTTMean, (1000+2000+3000+4000+5000)/5.0, 1e-9)
		near(t, out[0].SuccessRate, 1, 0)
	})
}
