//go:build linux

package probing

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// RouteManager coordinates route probing and kernel synchronization.
// It holds the in-memory route store and a background worker.
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

// PeerOnEstablished clears any prior managed state and starts the worker.
// Safe to call repeatedly; the worker will only be started once.
func (m *RouteManager) PeerOnEstablished() error {
	m.cfg.Scheduler.Clear()
	m.store.Clear()
	if !m.worker.IsRunning() {
		m.worker.Start(m.cfg.Context)
	}
	return nil
}

// PeerOnClose stops the worker, clears the scheduler, and resets the store.
// Idempotent: calling multiple times is safe.
func (m *RouteManager) PeerOnClose() error {
	if m.worker.IsRunning() {
		m.worker.Stop()
	}
	m.cfg.Scheduler.Clear()
	m.store.Clear()
	return nil
}

// RouteAdd adds a route to managed probing if the worker is running;
// otherwise it installs the route directly into the kernel.
func (m *RouteManager) RouteAdd(route *routing.Route) error {
	if m.worker.IsRunning() {
		if err := m.handleRouteAdd(route); err != nil {
			return fmt.Errorf("probing: error adding route: %w", err)
		}
		return nil
	}
	return m.cfg.Netlink.RouteAdd(route)
}

// RouteDelete removes a route from managed probing (and kernel) if the worker
// is running; otherwise it deletes the route directly from the kernel.
func (m *RouteManager) RouteDelete(route *routing.Route) error {
	if m.worker.IsRunning() {
		if err := m.handleRouteDelete(route); err != nil {
			return fmt.Errorf("probing: error deleting route: %w", err)
		}
		return nil
	}
	return m.cfg.Netlink.RouteDelete(route)
}

// RouteByProtocol passes through to Netlink to list routes by protocol number.
func (m *RouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.cfg.Netlink.RouteByProtocol(protocol)
}

// handleRouteAdd validates and registers a route in the managed store,
// and schedules it for periodic probing.
func (m *RouteManager) handleRouteAdd(route *routing.Route) error {
	if err := validateRoute(route); err != nil {
		return fmt.Errorf("invalid route: %w", err)
	}

	// Add the route to managed route store.
	key := newRouteKey(route)
	now := m.cfg.NowFunc()
	m.cfg.Scheduler.Add(key, now)
	m.store.Set(key, managedRoute{
		route:    route,
		liveness: m.cfg.Liveness.NewTracker(),
	})

	m.log.Debug("probing: route added to managed routes", "route", route.String(), "routes", m.store.Len())
	return nil
}

// handleRouteDelete validates and removes a route from both the managed store
// and the kernel routing table. ErrRouteNotFound is tolerated as benign.
func (m *RouteManager) handleRouteDelete(route *routing.Route) error {
	if err := validateRoute(route); err != nil {
		return fmt.Errorf("invalid route: %w", err)
	}

	// Delete the route from managed route store and scheduler.
	key := newRouteKey(route)
	m.cfg.Scheduler.Del(key)
	m.store.Del(key)

	// Delete the route from kernel immediately.
	if err := m.cfg.Netlink.RouteDelete(route); err != nil && !errors.Is(err, routing.ErrRouteNotFound) {
		return fmt.Errorf("error deleting route from kernel: %w", err)
	}

	m.log.Debug("probing: route deleted", "route", route.String(), "routes", m.store.Len())
	return nil
}
