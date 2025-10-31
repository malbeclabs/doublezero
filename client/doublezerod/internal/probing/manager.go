//go:build linux

package probing

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// RouteManager coordinates route probing and kernel synchronization.
// It maintains a route store and a background probing worker.
type RouteManager struct {
	log *slog.Logger
	cfg *Config

	store  *routeStore
	worker *probingWorker
}

// NewRouteManager constructs a new RouteManager after validating the config.
// It initializes the route store and its associated worker.
func NewRouteManager(cfg *Config) (*RouteManager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("probing: error validating config: %w", err)
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

// PeerOnEstablished starts the probing worker when a peer connection is established.
func (m *RouteManager) PeerOnEstablished() error {
	m.store.Clear()
	if !m.worker.IsRunning() {
		m.worker.Start(m.cfg.Context)
	}
	return nil
}

// PeerOnClose stops the probing worker when the peer connection closes.
func (m *RouteManager) PeerOnClose() error {
	if m.worker.IsRunning() {
		m.worker.Stop()
	}
	m.store.Clear()
	return nil
}

// RouteAdd adds a route to the manager. If the worker is active, it’s added to
// the managed store; otherwise it’s directly applied to the kernel.
func (m *RouteManager) RouteAdd(route *routing.Route) error {
	if m.worker.IsRunning() {
		err := m.handleRouteAdd(route)
		if err != nil {
			return fmt.Errorf("probing: error adding route: %w", err)
		}
		return nil
	}
	return m.cfg.Netlink.RouteAdd(route)
}

// RouteDelete removes a route from the manager or directly from the kernel,
// depending on whether the worker is active.
func (m *RouteManager) RouteDelete(route *routing.Route) error {
	if m.worker.IsRunning() {
		err := m.handleRouteDelete(route)
		if err != nil {
			return fmt.Errorf("probing: error deleting route: %w", err)
		}
		return nil
	}
	return m.cfg.Netlink.RouteDelete(route)
}

// RouteByProtocol retrieves kernel routes filtered by protocol number.
func (m *RouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.cfg.Netlink.RouteByProtocol(protocol)
}

// handleRouteAdd validates and registers a route in the managed store.
// Routes in the store will be probed periodically.
func (m *RouteManager) handleRouteAdd(route *routing.Route) error {
	if err := validateRoute(route); err != nil {
		return fmt.Errorf("invalid route: %w", err)
	}

	// Add the route to managed route store.
	key := newRouteKey(route)
	m.store.Set(key, managedRoute{
		route:    route,
		liveness: m.cfg.Liveness.NewTracker(),
	})

	m.log.Debug("probing: route added to managed routes", "route", route.String(), "routes", m.store.Len())
	return nil
}

// handleRouteDelete validates and removes a route from both the managed store
// and the kernel routing table.
func (m *RouteManager) handleRouteDelete(route *routing.Route) error {
	if err := validateRoute(route); err != nil {
		return fmt.Errorf("invalid route: %w", err)
	}

	// Delete the route from managed route store.
	key := newRouteKey(route)
	m.store.Del(key)

	// Delete the route from kernel immediately.
	err := m.cfg.Netlink.RouteDelete(route)
	if err != nil && !errors.Is(err, routing.ErrRouteNotFound) {
		return fmt.Errorf("error deleting route from kernel: %w", err)
	}

	m.log.Debug("probing: route deleted", "route", route.String(), "routes", m.store.Len())
	return nil
}
