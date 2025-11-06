package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

type RouteReaderWriterConfig struct {
	Logger    *slog.Logger
	Liveness  *Manager
	Netlinker RouteReaderWriter

	Interface string
	Port      int

	TxMin      time.Duration
	RxMin      time.Duration
	DetectMult uint8
}

func (c *RouteReaderWriterConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Liveness == nil {
		return errors.New("liveness manager is required")
	}
	if c.Netlinker == nil {
		return errors.New("netlinker is required")
	}

	if c.Interface == "" {
		return errors.New("interface is required")
	}
	if c.Port <= 0 {
		return errors.New("port must be greater than 0")
	}

	if c.TxMin <= 0 {
		return errors.New("txMin must be greater than 0")
	}
	if c.RxMin <= 0 {
		return errors.New("rxMin must be greater than 0")
	}
	if c.DetectMult <= 0 {
		return errors.New("detectMult must be greater than 0")
	}
	return nil
}

type routeReaderWriter struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    *slog.Logger
	cfg    *RouteReaderWriterConfig
}

func NewRouteReaderWriter(ctx context.Context, cfg *RouteReaderWriterConfig) (*routeReaderWriter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error validating route reader writer config: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &routeReaderWriter{
		ctx:    ctx,
		cancel: cancel,
		log:    cfg.Logger,
		cfg:    cfg,
	}
	return m, nil
}

func (m *routeReaderWriter) Close() error { m.cancel(); return m.cfg.Liveness.Close() }

func (m *routeReaderWriter) RouteAdd(r *routing.Route) error {
	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}

	return m.cfg.Liveness.RegisterRoute(r, peerAddr, m.cfg.Interface, m.cfg.TxMin, m.cfg.RxMin, m.cfg.DetectMult)
}

func (m *routeReaderWriter) RouteDelete(r *routing.Route) error {
	return m.cfg.Liveness.WithdrawRoute(r, m.cfg.Interface)
}

func (m *routeReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.cfg.Netlinker.RouteByProtocol(protocol)
}

func peerAddrFor(r *routing.Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.String(), port)
}
