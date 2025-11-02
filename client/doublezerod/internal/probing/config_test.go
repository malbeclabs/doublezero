//go:build linux

package probing

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	liveness, err := NewHysteresisLivenessPolicy(2, 2)
	if err != nil {
		panic(err)
	}
	limiter, err := NewSemaphoreLimiter(10)
	if err != nil {
		panic(err)
	}
	scheduler, err := NewIntervalScheduler(200*time.Millisecond, 20*time.Millisecond, false)
	if err != nil {
		panic(err)
	}
	return Config{
		Logger:     logger,
		Context:    context.Background(),
		Netlink:    &MockNetlinker{},
		Liveness:   liveness,
		Limiter:    limiter,
		Scheduler:  scheduler,
		ListenFunc: func(ctx context.Context) error { return nil },
		ProbeFunc:  func(ctx context.Context, route *routing.Route) (ProbeResult, error) { return ProbeResult{}, nil },
	}
}

func TestProbing_ConfigValidate(t *testing.T) {
	t.Parallel()
	_ = t.Context()

	t.Run("valid_config_passes", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		require.NoError(t, cfg.Validate())
	})

	t.Run("nil_logger", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Logger = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "logger is required")
	})

	t.Run("nil_context", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Context = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "context is required")
	})

	t.Run("nil_netlink", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Netlink = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "netlink is required")
	})

	t.Run("nil_liveness", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Liveness = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "liveness policy is required")
	})

	t.Run("nil_limiter", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Limiter = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "limiter is required")
	})

	t.Run("nil_scheduler", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Scheduler = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "scheduler is required")
	})

	t.Run("nil_listen_func", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.ListenFunc = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "listen func is required")
	})

	t.Run("nil_probe_func", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.ProbeFunc = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "probe func is required")
	})
}
