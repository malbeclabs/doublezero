package liveness

import (
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

type routeReaderWriter struct {
	lm    *Manager
	rrw   RouteReaderWriter
	iface string
}

func NewRouteReaderWriter(lm *Manager, rrw RouteReaderWriter, iface string) *routeReaderWriter {
	return &routeReaderWriter{
		lm:    lm,
		rrw:   rrw,
		iface: iface,
	}
}

func (m *routeReaderWriter) RouteAdd(r *routing.Route) error {
	return m.lm.RegisterRoute(r, m.iface)
}

func (m *routeReaderWriter) RouteDelete(r *routing.Route) error {
	return m.lm.WithdrawRoute(r, m.iface)
}

func (m *routeReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.rrw.RouteByProtocol(protocol)
}
