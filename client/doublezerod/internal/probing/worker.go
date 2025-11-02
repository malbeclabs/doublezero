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

// Run drives the main loop:
//   - arms a single reusable timer for the current earliest due time,
//   - waits on: timer fire, Scheduler.Wake(), or ctx cancel,
//   - launches due probes (bounded by Limiter).
//
// Each completed probe feeds back via Scheduler.Complete(rk, outcome) which
// re-arms cadence externally.
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
		// If next is zero, disable the timer (sit idle until Wake()).
		if next.IsZero() {
			tc = nil
			return
		}
		d := time.Until(next)
		d = max(d, 0) // fire immediately if overdue
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

		// Fast path: if something is already due, process immediately instead of waiting for
		// a re-armed timer tick.
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
					// Route disappeared after scheduling; Complete with a canceled outcome so the
					// scheduler can advance its cadence and not wedge this key.
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
func (w *probingWorker) runProbe(parent context.Context, rk RouteKey, mr managedRoute) {
	// Per-probe deadline to bound work and limiter holds.
	// TODO(snormore): Consider exposing as a configurable field.
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	var outcome ProbeOutcome
	defer func() {
		if outcome.When.IsZero() {
			outcome.When = time.Now()
		}
		// Ensure Scheduler.Complete is called exactly once for this rk, even on early returns.
		w.cfg.Scheduler.Complete(rk, outcome)
	}()

	rel, ok := w.cfg.Limiter.Acquire(ctx)
	if !ok {
		outcome = ProbeOutcome{OK: false, Err: ctx.Err(), When: time.Now()}
		return
	}
	defer rel()

	start := time.Now()
	res, err := w.cfg.ProbeFunc(ctx, mr.route)

	outcome.OK = err == nil && res.OK
	outcome.Err = err
	outcome.When = time.Now()
	if err == nil {
		outcome.RTT = outcome.When.Sub(start)
	}

	// If shutting down, skip state mutation (but Complete still runs via defer).
	if ctx.Err() != nil {
		return
	}

	w.applyProbeResult(&mr, outcome.OK)
}

// listen runs cfg.ListenFunc until it returns nil or ctx is canceled, retrying
// with exponential backoff on error. Backoff sleeps are ctx-cancelable.
// Contract: ListenFunc should return nil on a clean, permanent exit; transient
// failures should be reported as errors to trigger backoff/retry.
func (w *probingWorker) listen(ctx context.Context) {
	backoff := w.cfg.ListenBackoff
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}

		if err := w.cfg.ListenFunc(ctx); err == nil {
			// Listener exited cleanly; we’re done.
			return
		} else {
			w.log.Error("listener error", "error", err)
		}

		// Calculate the backoff duration.
		attempt++
		d := backoff.Initial
		for i := 1; i < attempt; i++ {
			d = time.Duration(float64(d) * backoff.Multiplier)
			if d > backoff.Max {
				d = backoff.Max
				break
			}
		}

		// Cancelable sleep between retries.
		t := time.NewTimer(d)
		select {
		case <-t.C:
			// Backoff timer fired; retry.
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}
			return
		}
	}
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

	// Persist the updated snapshot back into the store.
	w.store.Set(key, cur)
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
