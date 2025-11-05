package liveness

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteKey struct {
	Iface     string
	SrcIP     string
	Table     int
	DstPrefix string
	NextHop   string
}

type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

type routeReaderWriter struct {
	ctx    context.Context
	cancel context.CancelFunc

	log *slog.Logger
	nlr RouteReaderWriter
	lm  *Manager
	cfg *Config
}

func NewRouteReaderWriter(ctx context.Context, log *slog.Logger, lm *Manager, nlr RouteReaderWriter, cfg *Config) (*routeReaderWriter, error) {
	ctx, cancel := context.WithCancel(ctx)
	m := &routeReaderWriter{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
		cfg:    cfg,
		lm:     lm,
		nlr:    nlr,
	}
	return m, nil
}

func (m *routeReaderWriter) Close() error { m.cancel(); return m.lm.Close() }

func (m *routeReaderWriter) RouteAdd(r *routing.Route) error {
	// TODO(snormore): Remove this hard-coded check.
	if r.Dst.IP.String() == "8.8.8.8" {
		return m.nlr.RouteAdd(r)
	}

	// TODO(snormore): Note this is the the expected peer port, which we are overloading with our
	// own port. Maybe that's fine, maybe it should be configured separately.
	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}

	return m.lm.RegisterRoute(r, peerAddr, m.cfg.Iface, m.cfg.TxMin, m.cfg.RxMin, m.cfg.DetectMult)
}

func (m *routeReaderWriter) RouteDelete(r *routing.Route) error {
	// TODO(snormore): Remove this hard-coded check.
	if r.Dst.IP.String() == "8.8.8.8" {
		return m.nlr.RouteDelete(r)
	}

	return m.lm.WithdrawRoute(r, m.cfg.Iface)
}

func (m *routeReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.nlr.RouteByProtocol(protocol)
}
