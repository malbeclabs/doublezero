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
	return Config{
		Logger:         logger,
		Context:        context.Background(),
		Netlink:        &MockNetlinker{},
		Liveness:       NewHysteresisLivenessPolicy(2, 2),
		ListenFunc:     func(ctx context.Context) error { return nil },
		ProbeFunc:      func(ctx context.Context, route *routing.Route) (ProbeResult, error) { return ProbeResult{}, nil },
		Interval:       200 * time.Millisecond,
		ProbeTimeout:   500 * time.Millisecond,
		MaxConcurrency: 10,
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

	t.Run("zero_interval", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.Interval = 0
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "interval is required")
	})

	t.Run("nonpositive_probe_timeout", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.ProbeTimeout = 0
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "probe timeout is required")
	})

	t.Run("nonpositive_max_concurrency", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.MaxConcurrency = 0
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "max concurrency must be greater than 0")
	})
}
