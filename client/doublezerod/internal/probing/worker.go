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
	w.running.Store(true)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.Run(ctx)
		w.running.Store(false)
	}()
}

// Stop cancels the worker if running and returns once Run has exited.
// Safe to call multiple times.
func (w *probingWorker) Stop() {
	w.cancelMu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.cancelMu.Unlock()
	w.wg.Wait()
}

// IsRunning reports whether the worker has been started and not yet stopped.
func (w *probingWorker) IsRunning() bool {
	return w.running.Load()
}

// Run drives the main loop: it arms a timer to the scheduler's next due time,
// waits on either the timer, a scheduler wake, or context cancel, and then
// launches probes (subject to the limiter). Each completed probe feeds back
// into liveness (kernel add/del) and re-arms scheduling via Scheduler.Complete.
func (w *probingWorker) Run(ctx context.Context) {
	w.log.Info("probing: worker started",
		"limiter", w.cfg.Limiter.String(),
		"scheduler", w.cfg.Scheduler.String(),
	)

	// Listener runs in parallel and is retried with backoff on failure.
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.listen(ctx)
	}()

	// Single reusable timer; we re-arm it whenever the earliest due changes.
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var tc <-chan time.Time

	reset := func(next time.Time) {
		if next.IsZero() {
			tc = nil
			return
		} // no timer when empty
		d := time.Until(next)
		d = max(d, 0)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)
		tc = timer.C
	}

	for {
		// Arm/disarm based on current earliest due. If empty, we sit idle on Wake().
		next, ok := w.cfg.Scheduler.Peek()
		if ok {
			reset(next)
		} else {
			reset(time.Time{})
		}

		// Fast-path: if something is already due, handle it immediately.
		now := time.Now()
		if ok && !next.After(now) {
			for _, rk := range w.cfg.Scheduler.PopDue(now) {
				if mr, ok := w.store.Get(rk); ok {
					w.wg.Add(1)
					go func(rk RouteKey, mr managedRoute) {
						defer w.wg.Done()
						w.runProbe(ctx, rk, mr)
					}(rk, mr)
				} else {
					w.cfg.Scheduler.Complete(rk, ProbeOutcome{OK: false, Err: context.Canceled, When: now})
				}
			}
			continue
		}

		select {
		case <-ctx.Done():
			w.log.Debug("probing: worker stopped", "error", ctx.Err())
			return

		case <-w.cfg.Scheduler.Wake():
			// earliest due changed; loop to re-arm
			continue

		case <-tc:
			// Timer fired; pop all routes due at or before now and launch probes.
			now := time.Now()
			for _, rk := range w.cfg.Scheduler.PopDue(now) {
				mr, ok := w.store.Get(rk)
				if !ok {
					// Route disappeared after being scheduled; re-arm cadence so it doesn't stick.
					w.log.Debug("probing: scheduled route vanished before probe", "route", rk)
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
	}
}

// runProbe executes a single probe for rk/mr. It acquires the limiter inside
// the goroutine so the run-loop never blocks on capacity. It re-arms the
// scheduler via Complete() and applies liveness/kernel reconciliation.
func (w *probingWorker) runProbe(ctx context.Context, rk RouteKey, mr managedRoute) {
	// Wrap the context with a conservative timeout to prevent edge case blocking.
	// TODO(snormore): Should this be configurable or based on some other config?
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	release, got := w.cfg.Limiter.Acquire(ctx)
	if !got {
		// If we're shutting down, do nothing; otherwise re-arm so the key doesn't stay leased.
		if ctx.Err() == nil {
			w.cfg.Scheduler.Complete(rk, ProbeOutcome{OK: false, Err: context.Canceled, When: time.Now()})
		}
		return
	}
	defer release()

	route := mr.route
	start := time.Now()
	res, err := w.cfg.ProbeFunc(ctx, route)

	// If the worker is shutting down, don't mutate state or schedule.
	if ctx.Err() != nil {
		return
	}

	ok := err == nil && res.OK
	w.applyProbeResult(&mr, ok)

	out := ProbeOutcome{OK: ok, Err: err, When: time.Now()}
	if err == nil {
		out.RTT = out.When.Sub(start)
	}
	w.cfg.Scheduler.Complete(rk, out)
}

// listen runs the configured listener with exponential backoff on error.
// Backoff sleep is cancelable via ctx; when ctx is done, the goroutine exits.
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

		// Cancelable sleep between retries.
		if !sleepCtx(ctx, bo) {
			w.log.Debug("probing: listener backoff canceled", "reason", ctx.Err())
			return
		}
		next := time.Duration(float64(bo) * w.cfg.ListenBackoff.Multiplier)
		if next > w.cfg.ListenBackoff.Max {
			next = w.cfg.ListenBackoff.Max
		}
		bo = next
	}
}

// applyProbeResult advances liveness for the route and reconciles kernel state.
// On Up transition → RouteAdd; on Down transition → RouteDelete; otherwise no-op.
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

	// Persist updated liveness tracker back into the store (immutable-by-value).
	w.store.Set(key, cur)
}

// validateRoute enforces IPv4 presence/shape for Src/Dst/NextHop.
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

// sleepCtx waits for duration d or returns early if ctx is canceled.
// Helper used by the listener backoff loop.
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
