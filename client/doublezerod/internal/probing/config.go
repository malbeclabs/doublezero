package probing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

const (
	defaultRouteEventBufferSize  = 1024
	defaultProbeResultBufferSize = 4096
)

type ListenFunc func(context.Context) error

type ProbeFunc func(context.Context, *routing.Route) (ProbeResult, error)

type ProbeResult struct {
	OK       bool
	Sent     int
	Received int
}

type Config struct {
	// Required object fields.
	Logger     *slog.Logger
	Context    context.Context
	Netlink    routing.Netlinker
	Liveness   LivenessPolicy
	ListenFunc ListenFunc
	ProbeFunc  ProbeFunc

	// Required scalar fields.
	Interval     time.Duration
	ProbeTimeout time.Duration

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
	if cfg.ListenFunc == nil {
		return errors.New("listen func is required")
	}
	if cfg.ProbeFunc == nil {
		return errors.New("probe func is required")
	}

	// Required scalar fields.
	if cfg.Interval <= 0 {
		return errors.New("interval is required")
	}
	if cfg.ProbeTimeout <= 0 {
		return errors.New("probe timeout is required")
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
