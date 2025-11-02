//go:build linux

package probing

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestProbing_Worker_StartStopIdempotent(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = newFakeScheduler()
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())

	require.False(t, w.IsRunning())
	w.Start(cfg.Context)
	require.True(t, w.IsRunning())
	w.Start(cfg.Context)
	require.True(t, w.IsRunning())
	w.Stop()
	require.False(t, w.IsRunning())
	w.Stop()
	require.False(t, w.IsRunning())
}

func TestProbing_Worker_SuccessThenFailure_TransitionsAndKernel(t *testing.T) {
	t.Parallel()

	sched := newFakeScheduler()
	var modeOK atomic.Bool
	modeOK.Store(true)

	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness, _ = NewHysteresisLivenessPolicy(2, 2)
		c.Limiter, _ = NewSemaphoreLimiter(8)
		c.Scheduler = sched
		c.ProbeFunc = func(ctx context.Context, r *routing.Route) (ProbeResult, error) {
			select {
			case <-ctx.Done():
				return ProbeResult{}, ctx.Err()
			default:
				return ProbeResult{OK: modeOK.Load()}, nil
			}
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())

	r1 := newTestRouteWithDst(net.IPv4(10, 0, 0, 99))
	r2 := newTestRouteWithDst(net.IPv4(10, 0, 0, 100))
	k1, k2 := newRouteKey(r1), newRouteKey(r2)
	w.store.Set(k1, managedRoute{route: r1, liveness: cfg.Liveness.NewTracker()})
	w.store.Set(k2, managedRoute{route: r2, liveness: cfg.Liveness.NewTracker()})
	sched.Add(k1, time.Now())
	sched.Add(k2, time.Now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	require.Eventually(t, func() bool { return w.IsRunning() }, 2*time.Second, 10*time.Millisecond)

	// small periodic nudge
	stopNudge := startNudger(t, sched, 20*time.Millisecond)
	t.Cleanup(stopNudge)

	runWave := func() {
		t.Helper()
		sched.Trigger()
		sched.waitDrained(t, 3*time.Second)
	}

	runWave()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 1, 0) &&
			hasRouteLiveness(w, r2, LivenessStatusDown, 1, 0)
	}, 3*time.Second, 20*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})

	runWave()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0) &&
			hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0)
	}, 3*time.Second, 20*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	modeOK.Store(false)
	runWave()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 0, 1) &&
			hasRouteLiveness(w, r2, LivenessStatusUp, 0, 1)
	}, 3*time.Second, 20*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	runWave()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2) &&
			hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2)
	}, 3*time.Second, 20*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_ErrorCountsAsFailure(t *testing.T) {
	t.Parallel()

	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness, _ = NewHysteresisLivenessPolicy(2, 2)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
		c.Limiter, _ = NewSemaphoreLimiter(8)
		c.Scheduler = sched
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())

	r1 := newTestRouteWithDst(net.IPv4(10, 0, 1, 1))
	r2 := newTestRouteWithDst(net.IPv4(10, 0, 1, 2))
	w.store.Set(newRouteKey(r1), managedRoute{route: r1, liveness: cfg.Liveness.NewTracker()})
	w.store.Set(newRouteKey(r2), managedRoute{route: r2, liveness: cfg.Liveness.NewTracker()})
	sched.Add(newRouteKey(r1), time.Now())
	sched.Add(newRouteKey(r2), time.Now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	// Two waves of success to go UP
	sched.Trigger()
	sched.waitDrained(t, time.Second)
	sched.Trigger()
	sched.waitDrained(t, time.Second)
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0) &&
			hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0)
	}, 2*time.Second, 25*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// Now errors
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
		return ProbeResult{}, errors.New("probe error")
	}

	// Two waves to go DOWN
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2) &&
			hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2)
	}, 2*time.Second, 25*time.Millisecond)
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_RespectsLimiterConcurrency(t *testing.T) {
	t.Parallel()

	const N, capC = 16, 3
	var cur, maxSeen int64
	started := make(chan struct{}, N)
	block := make(chan struct{})

	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			v := atomic.AddInt64(&cur, 1)
			for {
				old := atomic.LoadInt64(&maxSeen)
				if v <= old || atomic.CompareAndSwapInt64(&maxSeen, old, v) {
					break
				}
			}
			select {
			case started <- struct{}{}:
			default:
			}
			<-block
			atomic.AddInt64(&cur, -1)
			return ProbeResult{OK: true}, nil
		}
		c.Limiter, _ = NewSemaphoreLimiter(capC)
		c.Scheduler = sched
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	for i := 0; i < N; i++ {
		r := newTestRouteWithDst(net.IPv4(10, 0, 2, byte(i+1)))
		rk := newRouteKey(r)
		w.store.Set(rk, managedRoute{route: r, liveness: cfg.Liveness.NewTracker()})
		sched.Add(rk, time.Now())
	}

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	require.Eventually(t, func() bool { return w.IsRunning() }, 2*time.Second, 10*time.Millisecond)

	stop := startNudger(t, sched, 5*time.Millisecond)
	t.Cleanup(stop)

	// Wait until exactly capC probes have *started* (edge-driven, not polling maxSeen).
	for i := 0; i < capC; i++ {
		waitEdge(t, started, 5*time.Second, "probe did not start before deadline")
	}
	require.Equal(t, int64(capC), atomic.LoadInt64(&maxSeen), "limiter not respected")

	// Ensure no more start while blocked
	select {
	case <-started:
		t.Fatalf("limiter exceeded capacity")
	case <-time.After(100 * time.Millisecond):
	}

	close(block)
}

func TestProbing_Worker_IgnoresResultIfRouteRemoved(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	exited := make(chan struct{}, 1)
	unblock := make(chan struct{})

	var adds, dels int64

	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness, _ = NewHysteresisLivenessPolicy(1, 1)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-unblock
			select {
			case exited <- struct{}{}:
			default:
			}
			return ProbeResult{OK: true}, nil
		}
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&adds, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&dels, 1); return nil },
		}
		lim, _ := NewSemaphoreLimiter(1)
		c.Limiter = lim
		c.Scheduler = sched
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 3, 1))
	key := newRouteKey(r)
	w.store.Set(key, managedRoute{route: r, liveness: cfg.Liveness.NewTracker()})
	sched.Add(key, time.Now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	stopNudge := startNudger(t, sched, 5*time.Millisecond)
	t.Cleanup(stopNudge)

	// Kick a wave and wait until the probe goroutine has actually started.
	sched.Trigger()
	waitEdge(t, started, 2*time.Second, "probe did not start")

	// Now remove the route before the probe completes.
	w.store.Del(key)
	sched.Del(key)

	// Let the probe return and then confirm it exited.
	close(unblock)
	waitEdge(t, exited, 2*time.Second, "probe goroutine did not exit")

	require.Zero(t, atomic.LoadInt64(&adds))
	require.Zero(t, atomic.LoadInt64(&dels))
}

func TestProbing_Worker_ContextCancelIsNoop(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	done := make(chan struct{}, 1)

	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness, _ = NewHysteresisLivenessPolicy(1, 1)
		c.ProbeFunc = func(ctx context.Context, _ *routing.Route) (ProbeResult, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			select {
			case done <- struct{}{}:
			default:
			}
			return ProbeResult{}, ctx.Err()
		}
		c.Limiter, _ = NewSemaphoreLimiter(2)
		c.Scheduler = sched
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 4, 1))
	key := newRouteKey(r)
	w.store.Set(key, managedRoute{route: r, liveness: cfg.Liveness.NewTracker()})
	sched.Add(key, time.Now())

	ctx, cancel := context.WithCancel(cfg.Context)
	cfg.Context = ctx

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	stop := startNudger(t, sched, 5*time.Millisecond)
	t.Cleanup(stop)

	sched.Trigger()
	waitEdge(t, started, 2*time.Second, "probe did not start")
	cancel()
	waitEdge(t, done, 2*time.Second, "probe did not exit after cancel")

	// No liveness change
	require.True(t, hasRouteLiveness(w, r, LivenessStatusDown, 0, 0))
}

func TestProbing_Worker_ListenRetry_UntilContextDone(t *testing.T) {
	t.Parallel()

	fourthStarted := make(chan struct{}, 1)
	var listenCalls atomic.Int64

	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
		c.ListenFunc = func(ctx context.Context) error {
			n := listenCalls.Add(1)
			if n <= 3 {
				return errors.New("synthetic listen error")
			}
			select {
			case fourthStarted <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil
		}
		c.ListenBackoff = ListenBackoffConfig{Initial: time.Millisecond, Max: time.Millisecond, Multiplier: 1}
		c.Limiter, _ = NewSemaphoreLimiter(2)
		c.Scheduler = sched
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 5, 10))
	rk := newRouteKey(r)
	w.store.Set(rk, managedRoute{route: r, liveness: cfg.Liveness.NewTracker()})
	sched.Add(rk, time.Now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	requireEventuallyDump(t, func() bool { return w.IsRunning() }, 2*time.Second, 10*time.Millisecond, "worker did not start")

	// Wait until the 4th listen attempt has occurred
	requireEventuallyDump(t, func() bool {
		select {
		case <-fourthStarted:
			return true
		default:
			return false
		}
	}, 5*time.Second, 5*time.Millisecond, "4th listen attempt did not start")

	require.True(t, w.IsRunning())
}

func TestProbing_Worker_KernelError_DoesNotBlockStateAdvance(t *testing.T) {
	t.Parallel()

	var addErrs, delErrs int64
	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness, _ = NewHysteresisLivenessPolicy(1, 1)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addErrs, 1); return errors.New("add fail") },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delErrs, 1); return errors.New("del fail") },
		}
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = sched
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())

	r := newTestRouteWithDst(net.IPv4(10, 0, 6, 1))
	key := newRouteKey(r)
	w.store.Set(key, managedRoute{route: r, liveness: cfg.Liveness.NewTracker()})
	sched.Add(key, time.Now())

	w.Start(cfg.Context)
	defer w.Stop()

	// Wave 1: success → UP (add fails)
	sched.Trigger()
	sched.waitDrained(t, 2*time.Second) // ensure probe finished and Complete() ran
	require.True(t, hasRouteLiveness(w, r, LivenessStatusUp, 1, 0))
	require.Equal(t, int64(1), atomic.LoadInt64(&addErrs))

	// Wave 2: failure → DOWN (delete fails)
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: false}, nil }
	sched.Trigger()
	sched.waitDrained(t, 2*time.Second)
	require.True(t, hasRouteLiveness(w, r, LivenessStatusDown, 0, 1))
	require.Equal(t, int64(1), atomic.LoadInt64(&delErrs))
}
