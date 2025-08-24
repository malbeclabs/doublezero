package serviceability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMonitor_Serviceability_Watcher(t *testing.T) {
	t.Parallel()

	t.Run("new_watcher_validates_config", func(t *testing.T) {
		t.Parallel()
		_, err := NewServiceabilityWatcher(&Config{Logger: nil, Serviceability: nil, Interval: 0})
		require.Error(t, err)

		cfg := &Config{Logger: newTestLogger(t), Serviceability: &mockServiceabilityClient{}, Interval: 10 * time.Millisecond}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)
		require.NotNil(t, w)
		require.Equal(t, watcherName, w.Name())
	})

	t.Run("tick_success_sets_build_info", func(t *testing.T) {
		t.Parallel()
		version := serviceability.ProgramVersion{Major: 1, Minor: 2, Patch: 3}
		got := &serviceability.ProgramData{ProgramConfig: serviceability.ProgramConfig{Version: version}}
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:       10 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		err = w.Tick(context.Background())
		require.NoError(t, err)

		lbl := programVersionString(version)
		val := testutil.ToFloat64(MetricProgramBuildInfo.WithLabelValues(lbl))
		require.Equal(t, float64(1), val)
	})

	t.Run("tick_error_increments_error_metric", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }},
			Interval:       10 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		before := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		err = w.Tick(context.Background())
		require.Error(t, err)
		after := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		require.GreaterOrEqual(t, after-before, float64(1))
	})

	t.Run("run_stops_on_context_cancel", func(t *testing.T) {
		t.Parallel()
		got := &serviceability.ProgramData{ProgramConfig: serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 9, Minor: 9, Patch: 9}}}
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:       5 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()

		time.Sleep(15 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(250 * time.Millisecond):
			t.Fatal("Run did not return after cancel")
		}
	})

	t.Run("programVersionString_formats", func(t *testing.T) {
		t.Parallel()
		s := programVersionString(serviceability.ProgramVersion{Major: 0, Minor: 10, Patch: 7})
		require.Equal(t, "0.10.7", s)
	})
}
