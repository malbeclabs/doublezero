package probing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// Worker owns the event loop. It delegates all business logic
// back to the ProbingManager's existing methods.
type probingWorker struct {
	log   *slog.Logger
	cfg   Config
	store *routeStore

	// lifecycle
	cancel   context.CancelFunc
	cancelMu sync.RWMutex

	// events
	evRouteAdd    chan *routing.Route
	evRouteDelete chan *routing.Route
	evTick        chan struct{}
	evProbeRes    chan probeResult
}

type probeResult struct {
	ProbeResult
	route *managedRoute
}

func newWorker(log *slog.Logger, cfg Config, store *routeStore) *probingWorker {
	return &probingWorker{
		log:           log,
		cfg:           cfg,
		store:         store,
		evRouteAdd:    make(chan *routing.Route, cfg.RouteEventBufferSize),
		evRouteDelete: make(chan *routing.Route, cfg.RouteEventBufferSize),
		evTick:        make(chan struct{}, 1),
		evProbeRes:    make(chan probeResult, cfg.ProbeResultBufferSize),
	}
}

func (w *probingWorker) Start(parent context.Context) {
	if w.IsRunning() {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancelMu.Lock()
	w.cancel = cancel
	w.cancelMu.Unlock()
	go w.run(ctx)
}

func (w *probingWorker) Stop() {
	w.cancelMu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.cancelMu.Unlock()
}

func (w *probingWorker) IsRunning() bool {
	w.cancelMu.RLock()
	defer w.cancelMu.RUnlock()
	return w.cancel != nil
}

func (w *probingWorker) EnqueueAdd(r *routing.Route) {
	// Non-blocking unless the buffer is full.
	w.evRouteAdd <- r
}

func (w *probingWorker) EnqueueDelete(r *routing.Route) {
	// Non-blocking unless the buffer is full.
	w.evRouteDelete <- r
}

func (w *probingWorker) enqueueTick() {
	select {
	case w.evTick <- struct{}{}:
	default:
		// Drop if there's already a tick in the buffer.
	}
}

func (w *probingWorker) run(ctx context.Context) {
	w.log.Info("probing: worker started", "interval", w.cfg.Interval.String())

	go func() {
		if err := w.cfg.ListenFunc(ctx); err != nil {
			w.log.Error("probing: error listening", "error", err)
		}
	}()

	// Tick immediately to start probing.
	w.enqueueTick()

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	var inFlight int

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("probing: worker stopped", "error", ctx.Err())
			return

		case <-ticker.C:
			if inFlight == 0 {
				inFlight += w.startProbes(ctx)
			}

		case <-w.evTick:
			if inFlight == 0 {
				inFlight += w.startProbes(ctx)
			}

		case r := <-w.evRouteAdd:
			if err := w.handleRouteAdd(r); err != nil {
				w.log.Error("probing: route add failed", "route", r.String(), "error", err)
			}

		case r := <-w.evRouteDelete:
			if err := w.handleRouteDelete(r); err != nil {
				w.log.Error("probing: route delete failed", "route", r.String(), "error", err)
			}

		case pr := <-w.evProbeRes:
			w.applyProbeResult(pr.route, pr.OK)
			if inFlight > 0 {
				inFlight--
			}
		}
	}
}

// startProbes spawns probes using existing probeRoute; results return on evProbeRes.
func (w *probingWorker) startProbes(ctx context.Context) (spawned int) {
	for _, route := range w.store.Clone() {
		spawned++
		go func(mr managedRoute) {
			res, err := w.cfg.ProbeFunc(ctx, mr.route)
			if err != nil {
				w.log.Error("probing: probe error", "route", mr.String(), "error", err)
				w.evProbeRes <- probeResult{
					ProbeResult: ProbeResult{OK: false},
					route:       &mr,
				}
			} else {
				if res.OK {
					w.log.Debug("probing: route probe success", "route", mr.String(), "probe_ok", res.OK, "packets_sent", res.Sent, "packets_recv", res.Received)
				} else {
					w.log.Debug("probing: route probe failure", "route", mr.String(), "probe_ok", res.OK, "packets_sent", res.Sent, "packets_recv", res.Received)
				}
				w.evProbeRes <- probeResult{
					ProbeResult: res,
					route:       &mr,
				}
			}
		}(route)
	}
	return spawned
}

func (w *probingWorker) handleRouteAdd(route *routing.Route) error {
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
	w.store.Set(key, managedRoute{
		route:    route,
		liveness: w.cfg.Liveness.NewTracker(),
	})

	w.log.Debug("probing: route added to managed routes", "route", route.String(), "routes", w.store.Len())
	return nil
}

func (w *probingWorker) handleRouteDelete(route *routing.Route) error {
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
	w.store.Del(key)

	// Delete the route from the kernel immediately.
	err := w.cfg.Netlink.RouteDelete(route)
	if err != nil {
		return fmt.Errorf("error deleting route from kernel: %w", err)
	}

	w.log.Debug("probing: route deleted", "route", route.String(), "routes", w.store.Len())
	return nil
}

func (w *probingWorker) addRouteToKernel(mr *managedRoute) error {
	// Protect against the race condition where the route is deleted between probing start and now.
	key := newRouteKey(mr.route)
	if _, ok := w.store.Get(key); !ok {
		w.log.Debug("probing: route not found in managed routes, skipping add", "route", mr.String())
		return nil
	}

	// If the route is already in the kernel routing table, we skip adding it.
	if w.routeExistsInKernel(mr) {
		w.log.Debug("probing: route already in kernel routing table, skipping add", "route", mr.String())
		return nil
	}

	// Add the route to the kernel routing table.
	w.log.Debug("probing: adding route to kernel routing table", "route", mr.String())
	return w.cfg.Netlink.RouteAdd(mr.route)
}

func (w *probingWorker) deleteRouteFromKernel(mr *managedRoute) error {
	// Protect against the race condition where the route is deleted between probing start and now.
	key := newRouteKey(mr.route)
	if _, ok := w.store.Get(key); !ok {
		w.log.Debug("probing: route not found in managed routes, skipping delete", "route", mr.String())
		return nil
	}

	// If the route is not in the kernel routing table, we skip deleting it.
	if !w.routeExistsInKernel(mr) {
		w.log.Debug("probing: route not found in kernel routing table, skipping delete", "route", mr.String())
		return nil
	}

	// Copy the route and set the protocol to 0, which seems to be needed by netlink on delete.
	route := *mr.route
	route.Protocol = 0

	// Delete the route from the kernel routing table.
	w.log.Debug("probing: deleting route from kernel routing table", "route", mr.String())
	return w.cfg.Netlink.RouteDelete(&route)
}

func (w *probingWorker) routeExistsInKernel(mr *managedRoute) bool {
	routes, err := w.cfg.Netlink.RouteGet(mr.route.Dst.IP)
	if err != nil {
		w.log.Debug("probing: route get failed", "dst", mr.route.Dst.IP, "error", err)
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

func (w *probingWorker) applyProbeResult(snap *managedRoute, ok bool) {
	key := snap.Key()

	cur, exists := w.store.Get(key)
	if !exists {
		w.log.Debug("probing: route vanished before update", "route", snap.String())
		return
	}

	tr := cur.liveness.OnProbe(ok)

	switch tr {
	case LivenessTransitionToUp:
		if err := w.addRouteToKernel(&cur); err != nil {
			w.log.Error("probing: kernel add failed", "route", cur.String(), "error", err)
		} else {
			w.log.Info("probing: route transitioned to UP", "route", cur.String(),
				"successes", cur.liveness.ConsecutiveOK())
		}
	case LivenessTransitionToDown:
		if err := w.deleteRouteFromKernel(&cur); err != nil {
			w.log.Error("probing: kernel delete failed", "route", cur.String(), "error", err)
		} else {
			w.log.Info("probing: route transitioned to DOWN", "route", cur.String(),
				"failures", cur.liveness.ConsecutiveFail())
		}
	case LivenessTransitionNoChange:
		// nothing
	}

	w.store.Set(key, cur)
}
