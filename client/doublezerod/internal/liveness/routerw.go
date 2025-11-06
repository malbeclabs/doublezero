package liveness

import (
	"context"
	"log/slog"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

type routeReaderWriter struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    *slog.Logger
	lm     *Manager
	rrw    RouteReaderWriter
	iface  string
}

func NewRouteReaderWriter(ctx context.Context, log *slog.Logger, lm *Manager, rrw RouteReaderWriter, iface string) (*routeReaderWriter, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &routeReaderWriter{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
		lm:     lm,
		rrw:    rrw,
		iface:  iface,
	}, nil
}

func (m *routeReaderWriter) Close() error { m.cancel(); return m.lm.Close() }

func (m *routeReaderWriter) RouteAdd(r *routing.Route) error {
	return m.lm.RegisterRoute(r, m.iface)
}

func (m *routeReaderWriter) RouteDelete(r *routing.Route) error {
	return m.lm.WithdrawRoute(r, m.iface)
}

func (m *routeReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.rrw.RouteByProtocol(protocol)
}
