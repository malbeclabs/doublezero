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
	sched.Add(k1, now())
	sched.Add(k2, now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	require.Eventually(t, func() bool { return w.IsRunning() }, 2*time.Second, 10*time.Millisecond)

	// wave 1 -> still DOWN; ok=1
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusDown, 1, 0))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusDown, 1, 0))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})

	// wave 2 -> UP; ok=2
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// switch to failures
	modeOK.Store(false)

	// wave 3 -> still UP; fail=1
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusUp, 0, 1))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusUp, 0, 1))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// wave 4 -> DOWN; fail=2
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_ErrorCountsAsFailure(t *testing.T) {
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
	sched.Add(newRouteKey(r1), now())
	sched.Add(newRouteKey(r2), now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	// wave 1 -> still DOWN; ok=1
	sched.Trigger()
	sched.waitDrained(t, 2*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusDown, 1, 0))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusDown, 1, 0))

	// wave 2 -> UP; ok=2
	sched.Trigger()
	sched.waitDrained(t, 2*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// errors now count as failures
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
		return ProbeResult{}, errors.New("probe error")
	}

	// wave 3 -> still UP; fail=1
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusUp, 0, 1))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusUp, 0, 1))

	// wave 4 -> DOWN; fail=2
	sched.Trigger()
	sched.waitDrained(t, 3*time.Second)
	require.True(t, hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2))
	require.True(t, hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2))
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_RespectsLimiterConcurrency(t *testing.T) {
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
		sched.Add(rk, now())
	}

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	require.Eventually(t, func() bool { return w.IsRunning() }, 2*time.Second, 10*time.Millisecond)

	sched.Trigger()
	for i := 0; i < capC; i++ {
		waitEdge(t, started, 2*time.Second, "probe did not start before deadline")
	}
	require.Equal(t, int64(capC), atomic.LoadInt64(&maxSeen))

	select {
	case <-started:
		t.Fatalf("limiter exceeded capacity")
	case <-time.After(150 * time.Millisecond):
	}

	close(block)
}

func TestProbing_Worker_IgnoresResultIfRouteRemoved(t *testing.T) {
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
	sched.Add(key, now())

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	sched.Trigger()
	waitEdge(t, started, 2*time.Second, "probe did not start")

	w.store.Del(key)
	sched.Del(key)

	close(unblock)
	waitEdge(t, exited, 2*time.Second, "probe goroutine did not exit")

	require.Zero(t, atomic.LoadInt64(&adds))
	require.Zero(t, atomic.LoadInt64(&dels))
}

func TestProbing_Worker_ContextCancelIsNoop(t *testing.T) {
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
	sched.Add(key, now())

	ctx, cancel := context.WithCancel(cfg.Context)
	cfg.Context = ctx

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	sched.Trigger()
	waitEdge(t, started, 2*time.Second, "probe did not start")
	cancel()
	waitEdge(t, done, 2*time.Second, "probe did not exit after cancel")

	require.True(t, hasRouteLiveness(w, r, LivenessStatusDown, 0, 0))
}

func TestProbing_Worker_KernelError_DoesNotBlockStateAdvance(t *testing.T) {
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
	sched.Add(key, now())

	w.Start(cfg.Context)
	defer w.Stop()

	sched.Trigger()
	sched.waitDrained(t, 2*time.Second)
	require.True(t, hasRouteLiveness(w, r, LivenessStatusUp, 1, 0))
	require.Equal(t, int64(1), atomic.LoadInt64(&addErrs))

	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: false}, nil }
	sched.Trigger()
	sched.waitDrained(t, 2*time.Second)
	require.True(t, hasRouteLiveness(w, r, LivenessStatusDown, 0, 1))
	require.Equal(t, int64(1), atomic.LoadInt64(&delErrs))
}
