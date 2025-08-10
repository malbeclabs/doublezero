package stats

import (
	"context"
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
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := Aggregate(ctx, "C1", nil, 0)
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
		out, err := Aggregate(ctx, "C1", samples, 0)
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
		out, err := Aggregate(ctx, "C", samples, uint64(len(samples)))
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
		out, err := Aggregate(ctx, "C2", samples, 1)
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
		out, err := Aggregate(ctx, "C3", samples, 3)
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
		_, err := Aggregate(ctx, "C", samples, 1000)
		require.NoError(t, err)
	})
}

func TestTelemetry_Data_AggregateIntoTimeBuckets(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_100, 0)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		out, err := AggregateIntoTimeBuckets(ctx, "C", nil, time.Millisecond, true)
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
		out, err := AggregateIntoTimeBuckets(ctx, "C", series, -time.Millisecond, true)
		require.Error(t, err)
		assert.Nil(t, out)

		// span<=0 short-circuits to empty slice + nil error
		out, err = AggregateIntoTimeBuckets(ctx, "C", []CircuitLatencySample{{Timestamp: now, RTT: 100}}, time.Millisecond, true)
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
		outFill, err := AggregateIntoTimeBuckets(ctx, "C", series, 20*time.Millisecond, true)
		require.NoError(t, err)
		require.Len(t, outFill, 3) // [0..20), [20..40) (gap filled), [40..60)
		assert.Greater(t, outFill[0].SuccessCount, uint64(0))
		assert.Equal(t, uint64(0), outFill[1].SuccessCount) // gap filled with zeros
		assert.Equal(t, uint64(0), outFill[1].LossCount)

		outNoFill, err := AggregateIntoTimeBuckets(ctx, "C", series, 20*time.Millisecond, false)
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
		out, err := AggregateIntoTimeBuckets(ctx, "C", series, 20*time.Millisecond, true)
		require.NoError(t, err)
		require.Len(t, out, 2)
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
}
