package probing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/tools/uping/pkg/uping"
	promprobing "github.com/prometheus-community/pro-bing"
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

type ProbingManager struct {
	log *slog.Logger
	cfg Config

	routes   map[routeKey]managedRoute
	routesMu sync.RWMutex

	// The context cancel function for the worker.
	// Indicates that the worker is running.
	cancel   context.CancelFunc
	cancelMu sync.RWMutex
}

type routeKey struct {
	table   int
	dst     netip.Addr
	nextHop netip.Addr
}

func newRouteKey(route *routing.Route) routeKey {
	var dk, nk netip.Addr
	if a, ok := netip.AddrFromSlice(route.Dst.IP.To4()); ok {
		dk = a
	}
	if a, ok := netip.AddrFromSlice(route.NextHop.To4()); ok {
		nk = a
	}
	return routeKey{table: route.Table, dst: dk, nextHop: nk}
}

type managedRoute struct {
	route    *routing.Route
	liveness LivenessState
}

func (r *managedRoute) String() string {
	return r.route.String()
}

func (r *managedRoute) Key() routeKey {
	return newRouteKey(r.route)
}

func NewProbingManager(cfg Config) (*ProbingManager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error creating probing manager: %w", err)
	}
	return &ProbingManager{
		log: cfg.Logger,
		cfg: cfg,

		routes: make(map[routeKey]managedRoute),
	}, nil
}

func (m *ProbingManager) PeerOnEstablished() error {
	if !m.isRunning() {
		m.start()
	}
	return nil
}

func (m *ProbingManager) PeerOnClose() error {
	if m.isRunning() {
		m.stop()
	}
	return nil
}

func (m *ProbingManager) RouteAdd(route *routing.Route) error {
	if !m.cfg.TunnelSrc.Equal(route.Src) {
		m.log.Warn("probing: route src does not match tunnel src", "route", route.String(), "tunnel src", m.cfg.TunnelSrc.String())
		return nil
	}

	if m.isRunning() {
		return m.handleRouteAdd(route)
	}

	// If not running, pass the route to the netlinker.
	return m.cfg.Netlink.RouteAdd(route)
}

func (m *ProbingManager) RouteDelete(route *routing.Route) error {
	if !m.cfg.TunnelSrc.Equal(route.Src) {
		m.log.Warn("probing: route src does not match tunnel src", "route", route.String(), "tunnel src", m.cfg.TunnelSrc.String())
		return nil
	}

	if m.isRunning() {
		return m.handleRouteDelete(route)
	}

	// If not running, pass the route to the netlinker.
	return m.cfg.Netlink.RouteDelete(route)
}

func (m *ProbingManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	return m.cfg.Netlink.RouteByProtocol(protocol)
}

func (m *ProbingManager) Run(ctx context.Context) {
	m.log.Info("probing: worker started", "interval", m.cfg.Interval.String(), "max_concurrency", m.cfg.MaxConcurrency)

	listener, err := uping.NewListener(uping.ListenerConfig{
		Logger:    m.log,
		Interface: m.cfg.InterfaceName,
		IP:        m.cfg.TunnelSrc,
	})
	if err != nil {
		m.log.Error("probing: error creating listener", "error", err)
		return
	}

	go func() {
		if err := listener.Listen(ctx); err != nil {
			m.log.Error("probing: error listening", "error", err)
		}
	}()

	if err := m.Tick(ctx); err != nil {
		m.log.Error("probing: worker tick failed", "error", err)
	}

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.log.Debug("probing: worker stopped", "error", ctx.Err())
			return
		case <-ticker.C:
			if err := m.Tick(ctx); err != nil {
				m.log.Error("probing: worker tick failed", "error", err)
			}
		}
	}
}

func (m *ProbingManager) Tick(ctx context.Context) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, int(m.cfg.MaxConcurrency))
	for _, route := range m.getRoutes() {
		sem <- struct{}{} // limit concurrency
		wg.Add(1)
		go func(route managedRoute) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := m.probeRoute(ctx, &route)
			if err != nil {
				m.log.Error("probing: probe error", "route", route.String(), "error", err)
				// Treat probe error as a failed probe.
				m.applyProbeResult(&route, false)
				return
			}

			ok := res.PacketsSent > 0 && res.PacketsRecv == res.PacketsSent
			if !ok {
				m.log.Debug("probing: route probe failure", "route", route.String(), "packets_sent", res.PacketsSent, "packets_recv", res.PacketsRecv, "packet_loss", res.PacketLoss)
			} else {
				m.log.Debug("probing: route probe success", "route", route.String(), "packets_sent", res.PacketsSent, "packets_recv", res.PacketsRecv, "packet_loss", res.PacketLoss)
			}
			// Apply hysteresis policy and perform kernel ops only on transitions.
			m.applyProbeResult(&route, ok)
		}(route)
	}
	wg.Wait()

	return nil
}

func (m *ProbingManager) getRoutes() map[routeKey]managedRoute {
	m.routesMu.RLock()
	defer m.routesMu.RUnlock()
	return maps.Clone(m.routes)
}

func (m *ProbingManager) getRoutesLen() int {
	m.routesMu.RLock()
	defer m.routesMu.RUnlock()
	return len(m.routes)
}

func (m *ProbingManager) getRoute(key routeKey) (managedRoute, bool) {
	m.routesMu.RLock()
	defer m.routesMu.RUnlock()
	route, ok := m.routes[key]
	if !ok {
		return managedRoute{}, false
	}
	return route, true
}

func (m *ProbingManager) setRoute(key routeKey, route managedRoute) {
	m.routesMu.Lock()
	defer m.routesMu.Unlock()
	m.routes[key] = route
}

func (m *ProbingManager) deleteRoute(key routeKey) {
	m.routesMu.Lock()
	defer m.routesMu.Unlock()
	delete(m.routes, key)
}

func (m *ProbingManager) start() {
	ctx, cancel := context.WithCancel(m.cfg.Context)

	m.cancelMu.Lock()
	m.cancel = cancel
	m.cancelMu.Unlock()

	go m.Run(ctx)
}

func (m *ProbingManager) stop() {
	m.cancelMu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.cancelMu.Unlock()
}

func (m *ProbingManager) isRunning() bool {
	m.cancelMu.RLock()
	defer m.cancelMu.RUnlock()
	return m.cancel != nil
}

func (m *ProbingManager) probeRoute(ctx context.Context, mr *managedRoute) (*promprobing.Statistics, error) {
	m.log.Debug("probing: sending route probe", "route", mr.String())

	pinger, err := promprobing.NewPinger(mr.route.Dst.IP.String())
	if err != nil {
		return nil, fmt.Errorf("error creating route probe pinger: %w", err)
	}
	pinger.Count = 1
	pinger.Timeout = m.cfg.ProbeTimeout
	pinger.Source = mr.route.Src.String()
	pinger.InterfaceName = m.cfg.InterfaceName

	err = pinger.RunWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("probing: error probing route: %w", err)
	}

	return pinger.Statistics(), nil
}

func (m *ProbingManager) handleRouteAdd(route *routing.Route) error {
	if route.Dst.IP == nil {
		return fmt.Errorf("dst IP is nil")
	}
	if route.NextHop == nil {
		return fmt.Errorf("next hop is nil")
	}

	if route.Dst.IP.To4() == nil {
		return fmt.Errorf("dst IP is not an IPv4 address")
	}
	if route.NextHop.To4() == nil {
		return fmt.Errorf("next hop is not an IPv4 address")
	}

	key := newRouteKey(route)
	// Start conservatively as DOWN; we'll promote to UP after UpThreshold successes.
	m.setRoute(key, managedRoute{
		route: route,
		liveness: LivenessState{
			state: stateDown,
		},
	})

	// Just keep track of the route in memory. The liveness strategy will add it to the kernel when ready.
	m.log.Info("probing: route added to managed routes", "route", route.String(), "routes", m.getRoutesLen())
	return nil
}

func (m *ProbingManager) handleRouteDelete(route *routing.Route) error {
	if route.Dst.IP == nil {
		return fmt.Errorf("dst IP is nil")
	}
	if route.NextHop == nil {
		return fmt.Errorf("next hop is nil")
	}

	if route.Dst.IP.To4() == nil {
		return fmt.Errorf("dst IP is not an IPv4 address")
	}
	if route.NextHop.To4() == nil {
		return fmt.Errorf("next hop is not an IPv4 address")
	}

	// Delete the route from the managed routes map.
	key := newRouteKey(route)
	m.deleteRoute(key)

	// Delete the route from the kernel immediately.
	err := m.cfg.Netlink.RouteDelete(route)
	if err != nil {
		return fmt.Errorf("error deleting route from kernel: %w", err)
	}

	m.log.Info("probing: route deleted", "route", route.String(), "routes", m.getRoutesLen())
	return nil
}

func (m *ProbingManager) addRouteToKernel(mr *managedRoute) error {
	// Protect against the race condition where the route is deleted between probing start and now.
	key := newRouteKey(mr.route)
	if _, ok := m.getRoute(key); !ok {
		m.log.Debug("probing: route not found in managed routes, skipping add", "route", mr.String())
		return nil
	}

	// If the route is already in the kernel routing table, we skip adding it.
	if m.routeExistsInKernel(mr) {
		m.log.Debug("probing: route already in kernel routing table, skipping add", "route", mr.String())
		return nil
	}

	// Add the route to the kernel routing table.
	m.log.Info("probing: adding route to kernel routing table", "route", mr.String())
	return m.cfg.Netlink.RouteAdd(mr.route)
}

func (m *ProbingManager) deleteRouteFromKernel(mr *managedRoute) error {
	// Protect against the race condition where the route is deleted between probing start and now.
	key := newRouteKey(mr.route)
	if _, ok := m.getRoute(key); !ok {
		m.log.Debug("probing: route not found in managed routes, skipping delete", "route", mr.String())
		return nil
	}

	// If the route is not in the kernel routing table, we skip deleting it.
	if !m.routeExistsInKernel(mr) {
		m.log.Debug("probing: route not found in kernel routing table, skipping delete", "route", mr.String())
		return nil
	}

	// Copy the route and set the protocol to 0, which seems to be needed by netlink on delete.
	route := *mr.route
	route.Protocol = 0

	// Delete the route from the kernel routing table.
	m.log.Info("probing: deleting route from kernel routing table", "route", mr.String())
	return m.cfg.Netlink.RouteDelete(&route)
}

func (m *ProbingManager) routeExistsInKernel(mr *managedRoute) bool {
	routes, err := m.cfg.Netlink.RouteGet(mr.route.Dst.IP)
	if err != nil {
		m.log.Debug("probing: route get failed", "dst", mr.route.Dst.IP, "error", err)
		return false
	}
	for _, route := range routes {
		// The netlink RouteGet returns routes with a nil NextHop when the requested IP is not in
		// the routing table.
		if route.NextHop == nil {
			continue
		}
		sameTable := route.Table == mr.route.Table
		sameNextHop := route.NextHop.Equal(mr.route.NextHop)
		sameSrc := (route.Src == nil && mr.route.Src == nil) ||
			(route.Src != nil && mr.route.Src != nil && route.Src.Equal(mr.route.Src))
		if sameTable && sameNextHop && sameSrc {
			return true
		}
	}
	return false
}

func (m *ProbingManager) applyProbeResult(snap *managedRoute, ok bool) {
	key := snap.Key()

	// Re-read current value; it may have changed while probing.
	cur, exists := m.getRoute(key)
	if !exists {
		m.log.Debug("probing: route vanished before update", "route", snap.String())
		return
	}

	// Apply pure policy.
	pol := Policy{UpThreshold: m.cfg.UpThreshold, DownThreshold: m.cfg.DownThreshold}
	next, tr := pol.Next(cur.liveness, ok)
	cur.liveness = next

	// Act only on transitions.
	switch tr {
	case ToUp:
		if err := m.addRouteToKernel(&cur); err != nil {
			m.log.Error("probing: kernel add failed", "route", cur.String(), "error", err)
		} else {
			m.log.Info("probing: route marked UP", "route", cur.String(), "successes", cur.liveness.consecOK)
		}
	case ToDown:
		if err := m.deleteRouteFromKernel(&cur); err != nil {
			m.log.Error("probing: kernel delete failed", "route", cur.String(), "error", err)
		} else {
			m.log.Info("probing: route marked DOWN", "route", cur.String(), "failures", cur.liveness.consecFail)
		}
	case NoChange:
		// nothing
	}

	// Persist updated route entry.
	m.setRoute(key, cur)
}
