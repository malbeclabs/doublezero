package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_ComputeStats(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		rtts := []float64{100, 110, 120, 130, 140}
		stat := computeStats(now, rtts)

		requireFloatEqual(t, stat.RTTMean, 120)
		requireFloatEqual(t, stat.RTTMedian, 120)
		requireFloatEqual(t, stat.RTTMin, 100)
		requireFloatEqual(t, stat.RTTMax, 140)
		requireFloatEqual(t, stat.RTTStdDev, 14.1421356237)
		requireFloatEqual(t, stat.RTTVariance, 200)
		require.Greater(t, stat.JitterMax, 0.0)
		require.Greater(t, stat.JitterEWMA, 0.0)
		require.Equal(t, stat.JitterPeakToPeak, 40.0)
		require.Equal(t, stat.SuccessCount, uint64(5))
		require.Equal(t, stat.LossCount, uint64(0))
	})

	t.Run("with_loss", func(t *testing.T) {
		t.Parallel()

		rtts := []float64{0, 0, 150, 160, 170}
		stat := computeStats(now, rtts)

		require.Equal(t, stat.SuccessCount, uint64(3))
		require.Equal(t, stat.LossCount, uint64(2))
		requireFloatEqual(t, stat.LossRate, 0.4)
		requireFloatEqual(t, stat.RTTMean, 160)
		requireFloatEqual(t, stat.RTTMedian, 160)
		require.Greater(t, stat.JitterMax, 0.0)
	})

	t.Run("single_sample", func(t *testing.T) {
		t.Parallel()

		rtts := []float64{200}
		stat := computeStats(now, rtts)

		require.Equal(t, stat.SuccessCount, uint64(1))
		requireFloatEqual(t, stat.RTTMean, 200)
		requireFloatEqual(t, stat.RTTMedian, 200)
		requireFloatEqual(t, stat.RTTVariance, 0)
		requireFloatEqual(t, stat.RTTStdDev, 0)
		require.Equal(t, stat.JitterAvg, 0.0)
		require.Equal(t, stat.JitterEWMA, 0.0)
		require.Equal(t, stat.JitterMax, 0.0)
	})

	t.Run("all_zero_loss", func(t *testing.T) {
		t.Parallel()

		rtts := []float64{0, 0, 0}
		stat := computeStats(now, rtts)

		require.Equal(t, stat.SuccessCount, uint64(0))
		require.Equal(t, stat.LossCount, uint64(3))
		requireFloatEqual(t, stat.RTTMean, 0)
		requireFloatEqual(t, stat.RTTMax, 0)
		requireFloatEqual(t, stat.RTTMedian, 0)
	})
}

func requireFloatEqual(t *testing.T, a, b float64) {
	require.InDelta(t, a, b, 1e-6)
}
