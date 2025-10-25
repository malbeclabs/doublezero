package probing

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

const (
	defaultRouteEventBufferSize  = 1024
	defaultProbeResultBufferSize = 4096
)

type Config struct {
	// Required object fields.
	Logger   *slog.Logger
	Context  context.Context
	Netlink  routing.Netlinker
	Liveness LivenessPolicy

	// Required scalar fields.
	Interval      time.Duration
	ProbeTimeout  time.Duration
	InterfaceName string
	TunnelSrc     net.IP

	// Optional fields.
	RouteEventBufferSize  int
	ProbeResultBufferSize int
}

func (cfg *Config) Validate() error {
	// Required object fields.
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.Context == nil {
		return errors.New("context is required")
	}
	if cfg.Netlink == nil {
		return errors.New("netlink is required")
	}
	if cfg.Liveness == nil {
		return errors.New("liveness policy is required")
	}

	// Required scalar fields.
	if cfg.Interval <= 0 {
		return errors.New("interval is required")
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

	// Optional fields.
	if cfg.RouteEventBufferSize == 0 {
		cfg.RouteEventBufferSize = defaultRouteEventBufferSize
	}
	if cfg.ProbeResultBufferSize == 0 {
		cfg.ProbeResultBufferSize = defaultProbeResultBufferSize
	}
	if cfg.RouteEventBufferSize <= 0 {
		return errors.New("route event buffer size must be greater than 0")
	}
	if cfg.ProbeResultBufferSize <= 0 {
		return errors.New("probe result buffer size must be greater than 0")
	}
	return nil
}
