package probing

import (
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteManager struct {
	log *slog.Logger
	cfg Config

	store  *routeStore
	worker *probingWorker
}

func NewRouteManager(cfg Config) (*RouteManager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error creating probing manager: %w", err)
	}
	store := newRouteStore()
	worker := newWorker(cfg.Logger, cfg, store)
	return &RouteManager{
		log: cfg.Logger,
		cfg: cfg,

		store:  store,
		worker: worker,
	}, nil
}

func (m *RouteManager) PeerOnEstablished() error {
	if !m.worker.IsRunning() {
		m.worker.Start(m.cfg.Context)
	}
	return nil
}

func (m *RouteManager) PeerOnClose() error {
	if m.worker.IsRunning() {
		m.worker.Stop()
	}
	return nil
}

func (m *RouteManager) RouteAdd(route *routing.Route) error {
	if !m.cfg.TunnelSrc.Equal(route.Src) {
		m.log.Warn("probing: route src does not match tunnel src", "route", route.String(), "tunnel src", m.cfg.TunnelSrc.String())
		return nil
	}
	if m.worker.IsRunning() {
		m.worker.EnqueueAdd(route)
		return nil
	}
	return m.cfg.Netlink.RouteAdd(route)
}

func (m *RouteManager) RouteDelete(route *routing.Route) error {
	if !m.cfg.TunnelSrc.Equal(route.Src) {
		m.log.Warn("probing: route src does not match tunnel src", "route", route.String(), "tunnel src", m.cfg.TunnelSrc.String())
		return nil
	}
	if m.worker.IsRunning() {
		m.worker.EnqueueDelete(route)
		return nil
	}
	return m.cfg.Netlink.RouteDelete(route)
}

func (m *RouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.cfg.Netlink.RouteByProtocol(protocol)
}
