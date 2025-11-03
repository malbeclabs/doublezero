//go:build linux

package probing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// probingWorker periodically probes managed routes for liveness and reconciles
// desired route state with the kernel (via Netlink) based on probe results.
// Scheduling (when to probe) and concurrency control are delegated to
// Scheduler and Limiter provided through Config.
type probingWorker struct {
	log     *slog.Logger
	cfg     *Config
	store   *routeStore
	wg      sync.WaitGroup
	running atomic.Bool

	// cancel/cancelMu guard the worker's lifecycle; Start installs a cancel
	// tied to the run-loop's context, Stop invokes it, and IsRunning checks it.
	cancel   context.CancelFunc
	cancelMu sync.RWMutex
}

// newWorker wires a worker to the shared store and config.
// Call Start to begin the run loop.
func newWorker(log *slog.Logger, cfg *Config, store *routeStore) *probingWorker {
	return &probingWorker{log: log, cfg: cfg, store: store}
}

// Start launches the worker if not already running. The run loop exits when
// the provided context is canceled or Stop is called. Safe to call concurrently
// with IsRunning/Stop.
func (w *probingWorker) Start(ctx context.Context) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancelMu.Lock()
	w.cancel = cancel
	w.cancelMu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.Run(ctx)
		w.running.Store(false)
	}()
}

// Stop cancels the worker (if running) and blocks until Run returns.
// Safe and idempotent.
func (w *probingWorker) Stop() {
	w.cancelMu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.cancelMu.Unlock()
	w.wg.Wait()
}

// IsRunning reports whether Start was called and the run loop hasn't exited yet.
func (w *probingWorker) IsRunning() bool {
	return w.running.Load()
}

// Run is the main loop for the probing worker.
// It checks the scheduler for due work, launches probes when they’re due,
// and exits on context cancel. Work that’s already due is handled immediately
// instead of relying on a zero-duration timer.
func (w *probingWorker) Run(ctx context.Context) {
	w.log.Info("probing: worker started", "limiter", w.cfg.Limiter.String(), "scheduler", w.cfg.Scheduler.String())

	// Start the listener in the background. If it fails, cancel everything.
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		if err := w.cfg.ListenFunc(ctx); err != nil {
			w.log.Error("listener error", "error", err)
			w.cancelMu.Lock()
			if w.cancel != nil {
				w.cancel()
			}
			w.cancelMu.Unlock()
		}
	}()

	// Reusable timer.
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	var tc <-chan time.Time
	wakeCh := w.cfg.Scheduler.Wake()

	reset := func(next time.Time) {
		if next.IsZero() {
			tc = nil
			return
		}
		d := time.Until(next)
		if d < 0 {
			d = 0
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)
		tc = timer.C
	}

	launchDue := func(now time.Time) {
		for _, rk := range w.cfg.Scheduler.PopDue(now) {
			mr, ok := w.store.Get(rk)
			if !ok {
				w.cfg.Scheduler.Complete(rk, ProbeOutcome{OK: false, Err: context.Canceled, When: now})
				continue
			}
			w.wg.Add(1)
			go func(rk RouteKey, mr managedRoute) {
				defer w.wg.Done()
				w.runProbe(ctx, rk, mr)
			}(rk, mr)
		}
	}

	for {
		// Handle “due now” immediately to avoid Reset(0) races.
		if next, ok := w.cfg.Scheduler.Peek(); ok {
			if !next.After(w.cfg.NowFunc()) {
				launchDue(w.cfg.NowFunc())
				// After processing, loop to re-peek and re-arm.
				continue
			}
			reset(next)
		} else {
			reset(time.Time{})
		}

		select {
		case <-ctx.Done():
			w.log.Debug("probing: worker stopped", "error", ctx.Err())
			return

		case <-wakeCh:
			// The scheduler signaled a change; get the next wake channel
			// and loop to re-check due work.
			wakeCh = w.cfg.Scheduler.Wake()

		case <-tc:
			launchDue(w.cfg.NowFunc())
		}
	}
}

// runProbe executes one probe for (rk, mr).
// Behavior:
//   - Per-probe context with 10s timeout.
//   - Unconditional Scheduler.Complete in a defer (also on early return or panic).
//   - Acquires concurrency from Limiter inside the goroutine.
//   - Calls ProbeFunc, records RTT on success, and applies liveness→kernel reconciliation.
//   - If the probe context is canceled before reconciliation, skips state mutation.
func (w *probingWorker) runProbe(parent context.Context, rk RouteKey, mr managedRoute) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	outcome := ProbeOutcome{When: w.cfg.NowFunc()}
	defer func() {
		if r := recover(); r != nil {
			outcome.OK = false
			outcome.Err = fmt.Errorf("panic: %v", r)
		}
		w.cfg.Scheduler.Complete(rk, outcome)
	}()

	rel, ok := w.cfg.Limiter.Acquire(ctx)
	if !ok {
		outcome.OK = false
		outcome.Err = ctx.Err()
		return
	}
	defer rel()

	res, err := w.cfg.ProbeFunc(ctx, mr.route)
	outcome.When = w.cfg.NowFunc()
	outcome.OK = (err == nil && res.OK)
	outcome.Err = err
	outcome.RTT = res.RTTMean

	if ctx.Err() != nil {
		return
	}

	w.applyProbeResult(&mr, outcome.OK)
}

// applyProbeResult updates liveness state and reconciles kernel routes:
//
//	Up   -> Netlink.RouteAdd
//	Down -> Netlink.RouteDelete (routing.ErrRouteNotFound is benign)
//	NoChange -> no kernel op
//
// Idempotency: multiple identical transitions should be safe for Netlink.
func (w *probingWorker) applyProbeResult(mr *managedRoute, ok bool) {
	key := mr.Key()

	cur, exists := w.store.Get(key)
	if !exists {
		// Route removed between scheduling and completion.
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
				// Benign: kernel already reflects desired state.
				w.log.Debug("probing: route not found in kernel for deletion", "route", cur.String())
			} else {
				w.log.Error("probing: failed to delete route from kernel", "route", cur.String(), "error", err)
			}
		} else {
			w.log.Info("probing: route transitioned to DOWN", "route", cur.String(), "failures", cur.liveness.ConsecutiveFail())
		}

	case LivenessTransitionNoChange:
		// No kernel operation required.
	}

	// No need to store.Set since we mutated the managedRoute in place.
}

// validateRoute enforces IPv4-only Src/Dst/NextHop. This worker currently
// targets the IPv4 kernel path; extend as needed for v6.
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
