package probing

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type Config struct {
	Logger         *slog.Logger
	Context        context.Context
	Netlink        routing.Netlinker
	Interval       time.Duration
	MaxConcurrency uint
	ProbeTimeout   time.Duration
	InterfaceName  string
	TunnelSrc      net.IP

	// Liveness policy: consecutive probe results required before flipping kernel state.
	// If zero, defaults will be applied in NewProbingManager.
	UpThreshold   uint // consecutive successes to mark UP
	DownThreshold uint // consecutive failures to mark DOWN
}

func (cfg *Config) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.Context == nil {
		return errors.New("context is required")
	}
	if cfg.Netlink == nil {
		return errors.New("netlink is required")
	}
	if cfg.Interval <= 0 {
		return errors.New("interval is required")
	}
	if cfg.MaxConcurrency == 0 {
		return errors.New("max concurrency is required")
	}
	if cfg.ProbeTimeout <= 0 {
		return errors.New("probe timeout is required")
	}
	if cfg.InterfaceName == "" {
		return errors.New("interface name is required")
	}
	if cfg.TunnelSrc == nil {
		return errors.New("tunnel src is required")
	}
	if cfg.TunnelSrc.IsUnspecified() {
		return errors.New("tunnel src is unspecified")
	}
	if cfg.UpThreshold == 0 {
		return errors.New("up threshold is required")
	}
	if cfg.DownThreshold == 0 {
		return errors.New("down threshold is required")
	}
	return nil
}
