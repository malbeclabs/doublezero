//go:build linux

package probing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// probingWorker periodically probes managed routes for liveness and reconciles
// desired route state with the kernel (via Netlink) based on probe results.
type probingWorker struct {
	log   *slog.Logger
	cfg   *Config
	store *routeStore

	cancel   context.CancelFunc
	cancelMu sync.RWMutex
}

// newWorker wires a worker to the shared store and config.
// Call Start to begin the run loop.
func newWorker(log *slog.Logger, cfg *Config, store *routeStore) *probingWorker {
	return &probingWorker{
		log:   log,
		cfg:   cfg,
		store: store,
	}
}

// Start launches the worker if not already running.
// The worker stops when the provided context is canceled or Stop is called.
func (w *probingWorker) Start(ctx context.Context) {
	if w.IsRunning() {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancelMu.Lock()
	w.cancel = cancel
	w.cancelMu.Unlock()
	go w.Run(ctx)
}

// Stop cancels the worker if running and waits for Run to exit.
func (w *probingWorker) Stop() {
	w.cancelMu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.cancelMu.Unlock()
}

// IsRunning reports whether the worker has been started and not yet stopped.
func (w *probingWorker) IsRunning() bool {
	w.cancelMu.RLock()
	defer w.cancelMu.RUnlock()
	return w.cancel != nil
}

// Run drives the periodic probe loop and the listener retry loop.
// It exits when the context is canceled.
func (w *probingWorker) Run(ctx context.Context) {
	w.log.Info("probing: worker started", "interval", w.cfg.Interval.String())

	go w.listen(ctx)

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("probing: worker stopped", "error", ctx.Err())
			return

		case <-ticker.C:
			w.Tick()
		}
	}
}

// listen runs the configured listener with exponential backoff on error.
// Backoff sleep is cancelable via ctx.
func (w *probingWorker) listen(ctx context.Context) {
	bo := w.cfg.ListenBackoff.Initial
	for {
		if ctx.Err() != nil {
			w.log.Debug("probing: listener stopped by context", "reason", ctx.Err())
			return
		}
		err := w.cfg.ListenFunc(ctx)
		if err == nil || ctx.Err() != nil {
			w.log.Debug("probing: listener exited", "reason", ctx.Err())
			return
		}
		w.log.Error("probing: listener error", "error", err)

		if !sleepCtx(ctx, bo) { // cancelable sleep
			w.log.Debug("probing: listener backoff canceled", "reason", ctx.Err())
			return
		}
		next := time.Duration(float64(bo) * w.cfg.ListenBackoff.Multiplier)
		next = min(next, w.cfg.ListenBackoff.Max)
		bo = next
	}
}

// Tick performs one probe wave across the current managed routes.
// Concurrency is limited by MaxConcurrency and the call blocks until all
// per-route probes in this wave complete.
func (w *probingWorker) Tick() {
	var wg sync.WaitGroup
	sem := make(chan struct{}, int(w.cfg.MaxConcurrency))
	for _, route := range w.store.Clone() {
		sem <- struct{}{} // limit concurrency
		wg.Add(1)
		go func(mr managedRoute) {
			defer wg.Done()
			defer func() { <-sem }()

			ctx := w.cfg.Context
			res, err := w.cfg.ProbeFunc(ctx, mr.route)
			if err != nil {
				// Treat worker-stop cancellation as a no-op; other errors count as failures.
				if errors.Is(err, ctx.Err()) || errors.Is(err, context.Canceled) && ctx.Err() != nil {
					w.log.Debug("probing: probe aborted by context", "route", mr.String(), "reason", ctx.Err())
					return
				}
				w.log.Error("probing: probe error", "route", mr.String(), "error", err)
				w.applyProbeResult(&mr, false)
				return
			}
			if res.OK {
				w.log.Debug("probing: route probe success", "route", mr.String(), "probe_ok", res.OK, "packets_sent", res.Sent, "packets_recv", res.Received)
			} else {
				w.log.Debug("probing: route probe failure", "route", mr.String(), "probe_ok", res.OK, "packets_sent", res.Sent, "packets_recv", res.Received)
			}
			w.applyProbeResult(&mr, res.OK)
		}(route)
	}
	wg.Wait()
}

// applyProbeResult evolves liveness state and reconciles kernel state based on
// the resulting transition (ToUp/ToDown/NoChange).
func (w *probingWorker) applyProbeResult(mr *managedRoute, ok bool) {
	key := mr.Key()

	cur, exists := w.store.Get(key)
	if !exists {
		w.log.Debug("probing: route vanished before update", "route", mr.String())
		return
	}

	tr := cur.liveness.OnProbe(ok)

	switch tr {
	case LivenessTransitionToUp:
		if err := w.cfg.Netlink.RouteAdd(cur.route); err != nil {
			w.log.Error("probing: failed to add route to kernel", "route", cur.String(), "error", err)
		} else {
			w.log.Info("probing: route transitioned to UP", "route", cur.String(), "successes", cur.liveness.ConsecutiveOK())
		}
	case LivenessTransitionToDown:
		if err := w.cfg.Netlink.RouteDelete(cur.route); err != nil {
			if errors.Is(err, routing.ErrRouteNotFound) {
				w.log.Debug("probing: route not found in kernel for deletion", "route", cur.String())
			} else {
				w.log.Error("probing: failed to delete route from kernel", "route", cur.String(), "error", err)
			}
		} else {
			w.log.Info("probing: route transitioned to DOWN", "route", cur.String(), "failures", cur.liveness.ConsecutiveFail())
		}
	case LivenessTransitionNoChange:
		// nothing
	}

	w.store.Set(key, cur)
}

// validateRoute enforces IPv4 source/destination/next-hop presence and shape.
func validateRoute(route *routing.Route) error {
	if route == nil {
		return fmt.Errorf("route is nil")
	}

	if route.Src == nil {
		return fmt.Errorf("src IP is nil")
	}
	if route.Src.To4() == nil {
		return fmt.Errorf("src IP is not an IPv4 address")
	}

	if route.Dst == nil || route.Dst.IP == nil {
		return fmt.Errorf("dst IP is nil")
	}
	if route.Dst.IP.To4() == nil {
		return fmt.Errorf("dst IP is not an IPv4 address")
	}

	if route.NextHop == nil {
		return fmt.Errorf("next hop is nil")
	}
	if route.NextHop.To4() == nil {
		return fmt.Errorf("next hop is not an IPv4 address")
	}

	return nil
}

// sleepCtx waits for d, or returns early if ctx is canceled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
