package probing

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Config{
		Logger:        logger,
		Context:       context.Background(),
		Netlink:       &MockNetlinker{},
		Liveness:      NewHysteresisLivenessPolicy(2, 2),
		Interval:      200 * time.Millisecond,
		ProbeTimeout:  500 * time.Millisecond,
		InterfaceName: "eth0",
		TunnelSrc:     net.ParseIP("10.0.0.1"),
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

	t.Run("empty_interface_name", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.InterfaceName = ""
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "interface name is required")
	})

	t.Run("nil_tunnel_src", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.TunnelSrc = nil
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "tunnel src is required")
	})

	t.Run("unspecified_tunnel_src", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		cfg.TunnelSrc = net.IPv4zero // 0.0.0.0 triggers IsUnspecified()
		err := cfg.Validate()
		require.Error(t, err)
		require.EqualError(t, err, "tunnel src is unspecified")
	})
}
