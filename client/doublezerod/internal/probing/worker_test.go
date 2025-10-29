//go:build linux

package probing

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestProbing_Worker_StartStopIdempotent(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) { c.Interval = 24 * time.Hour })
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

func TestProbing_Worker_Tick_EmptyStore_NoPanic(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t, nil)
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Tick() // should not panic
}

func TestProbing_Worker_TickToUpThenToDown_OnProbeFailure(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour // drive ticks manually
		c.MaxConcurrency = 8
		c.Liveness = NewHysteresisLivenessPolicy(2, 2)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
	})
	require.NoError(t, cfg.Validate())

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r1 := newTestRouteWithDst(net.IPv4(10, 0, 0, 99))
	r2 := newTestRouteWithDst(net.IPv4(10, 0, 0, 100))
	w.store.Set(newRouteKey(r1), managedRoute{route: r1, liveness: w.cfg.Liveness.NewTracker()})
	w.store.Set(newRouteKey(r2), managedRoute{route: r2, liveness: w.cfg.Liveness.NewTracker()})

	// Tick and verify that the routes are still DOWN but with a successful probe count of 1.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 1, 0) && hasRouteLiveness(w, r2, LivenessStatusDown, 1, 0)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are not in the kernel yet.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})

	// Tick again and verify that the routes are now UP with a successful probe count of 2.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0) && hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are now in the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// Set probe func to return false.
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: false}, nil }

	// Tick and verify that the routes are still UP with a failed probe count of 1.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 0, 1) && hasRouteLiveness(w, r2, LivenessStatusUp, 0, 1)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are still in the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// Tick again and verify that the routes are now DOWN with a failed probe count of 2.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2) && hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are removed from the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_TickToUpThenToDown_OnProbeError(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour // drive ticks manually
		c.MaxConcurrency = 8
		c.Liveness = NewHysteresisLivenessPolicy(2, 2)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
	})
	require.NoError(t, cfg.Validate())

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r1 := newTestRouteWithDst(net.IPv4(10, 0, 0, 99))
	r2 := newTestRouteWithDst(net.IPv4(10, 0, 0, 100))
	w.store.Set(newRouteKey(r1), managedRoute{route: r1, liveness: w.cfg.Liveness.NewTracker()})
	w.store.Set(newRouteKey(r2), managedRoute{route: r2, liveness: w.cfg.Liveness.NewTracker()})

	// Tick and verify that the routes are still DOWN but with a successful probe count of 1.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 1, 0) && hasRouteLiveness(w, r2, LivenessStatusDown, 1, 0)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are not in the kernel yet.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})

	// Tick again and verify that the routes are now UP with a successful probe count of 2.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 2, 0) && hasRouteLiveness(w, r2, LivenessStatusUp, 2, 0)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are now in the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// Set probe func to return an error.
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
		return ProbeResult{}, errors.New("probe error")
	}

	// Tick and verify that the routes are still UP with a failed probe count of 1.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusUp, 0, 1) && hasRouteLiveness(w, r2, LivenessStatusUp, 0, 1)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are still in the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{r1, r2})

	// Tick again and verify that the routes are now DOWN with a failed probe count of 2.
	w.Tick()
	require.Eventually(t, func() bool {
		return hasRouteLiveness(w, r1, LivenessStatusDown, 0, 2) && hasRouteLiveness(w, r2, LivenessStatusDown, 0, 2)
	}, 2*time.Second, 25*time.Millisecond)

	// Verify that the routes are removed from the kernel.
	requireNetlinkRoutes(t, cfg.Netlink, []*routing.Route{})
}

func TestProbing_Worker_TickRespectsMaxConcurrency(t *testing.T) {
	t.Parallel()

	const N, capC = 16, 3
	var cur, maxSeen int64
	block := make(chan struct{})

	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = capC
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			v := atomic.AddInt64(&cur, 1)
			for {
				old := atomic.LoadInt64(&maxSeen)
				if v <= old || atomic.CompareAndSwapInt64(&maxSeen, old, v) {
					break
				}
			}
			<-block
			atomic.AddInt64(&cur, -1)
			return ProbeResult{OK: true}, nil
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	for i := range N {
		r := newTestRouteWithDst(net.IPv4(10, 0, 1, byte(i+1)))
		w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})
	}

	done := make(chan struct{})
	go func() { w.Tick(); close(done) }()

	require.Eventually(t, func() bool { return atomic.LoadInt64(&maxSeen) == capC }, 2*time.Second, 10*time.Millisecond)
	close(block)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick did not wait for probes to finish")
	}
}

func TestProbing_Worker_IgnoresResultIfRouteRemoved(t *testing.T) {
	t.Parallel()

	unblock := make(chan struct{})
	var adds, dels int64

	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = 1
		c.Liveness = NewHysteresisLivenessPolicy(1, 1)
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			<-unblock
			return ProbeResult{OK: true}, nil
		}
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&adds, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&dels, 1); return nil },
		}
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 2, 1))
	w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})

	go w.Tick()
	w.store.Del(newRouteKey(r))
	close(unblock)

	time.Sleep(100 * time.Millisecond)
	require.Zero(t, atomic.LoadInt64(&adds))
	require.Zero(t, atomic.LoadInt64(&dels))
}

func TestProbing_Worker_NoChange_NoKernelOps(t *testing.T) {
	t.Parallel()

	var addCalls, delCalls int64
	var starts int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = 8
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			atomic.AddInt64(&starts, 1)
			time.Sleep(1 * time.Millisecond)
			return ProbeResult{OK: true}, nil
		}
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCalls, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return nil },
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r1 := newTestRouteWithDst(net.IPv4(10, 0, 3, 1))
	r2 := newTestRouteWithDst(net.IPv4(10, 0, 3, 2))
	w.store.Set(newRouteKey(r1), managedRoute{route: r1, liveness: w.cfg.Liveness.NewTracker()})
	w.store.Set(newRouteKey(r2), managedRoute{route: r2, liveness: w.cfg.Liveness.NewTracker()})

	w.Tick()
	require.Eventually(t, func() bool { return atomic.LoadInt64(&starts) == 2 }, time.Second, 25*time.Millisecond)
	require.Zero(t, atomic.LoadInt64(&addCalls))
	require.Zero(t, atomic.LoadInt64(&delCalls))
}

func TestProbing_Worker_KernelError_DoesNotBlockStateAdvance(t *testing.T) {
	t.Parallel()
	var addErrs, delErrs int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = 4
		c.Liveness = NewHysteresisLivenessPolicy(1, 1) // single success → Up; single failure → Down
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil }
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addErrs, 1); return errors.New("add fail") },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delErrs, 1); return errors.New("del fail") },
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 4, 1))
	w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})

	w.Tick() // success → attempt RouteAdd (fails)
	require.Eventually(t, func() bool { return hasRouteLiveness(w, r, LivenessStatusUp, 1, 0) }, time.Second, 25*time.Millisecond)
	require.Equal(t, int64(1), atomic.LoadInt64(&addErrs))

	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: false}, nil }
	w.Tick() // failure → attempt RouteDelete (fails)
	require.Eventually(t, func() bool { return hasRouteLiveness(w, r, LivenessStatusDown, 0, 1) }, time.Second, 25*time.Millisecond)
	require.Equal(t, int64(1), atomic.LoadInt64(&delErrs))
}

func TestProbing_Worker_ContextCancelIsNoop(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = 2
		c.Liveness = NewHysteresisLivenessPolicy(1, 1)
		c.ProbeFunc = func(ctx context.Context, r *routing.Route) (ProbeResult, error) {
			close(started)
			<-ctx.Done()
			return ProbeResult{}, ctx.Err() // MUST propagate the tick ctx error
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	r := newTestRouteWithDst(net.IPv4(10, 0, 5, 1))
	w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})

	ctx, cancel := context.WithCancel(cfg.Context)
	cfg.Context = ctx
	done := make(chan struct{})
	go func() { w.Tick(); close(done) }()
	<-started
	cancel()

	<-done
	// No liveness change: still DOWN, no counts incremented.
	require.True(t, hasRouteLiveness(w, r, LivenessStatusDown, 0, 0))
}

func TestProbing_Worker_ListenError_DoesNotStop(t *testing.T) {
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = 10 * time.Millisecond
		c.ListenFunc = func(context.Context) error { return errors.New("listen failed") }
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	defer w.Stop()
	require.Eventually(t, func() bool { return w.IsRunning() }, time.Second, 10*time.Millisecond)
}

func TestProbing_Worker_ListenRetry_UntilContextDone(t *testing.T) {
	t.Parallel()

	var listenCalls, probeStarts int64
	var fourthRunning int64 // 0=false, 1=true

	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = 20 * time.Millisecond
		c.MaxConcurrency = 2
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			atomic.AddInt64(&probeStarts, 1)
			return ProbeResult{OK: true}, nil
		}
		c.ListenFunc = func(ctx context.Context) error {
			n := atomic.AddInt64(&listenCalls, 1)
			if n <= 3 {
				return errors.New("synthetic listen error")
			}
			atomic.StoreInt64(&fourthRunning, 1)
			<-ctx.Done()
			return nil
		}
		c.ListenBackoff = ListenBackoffConfig{
			Initial:    1 * time.Millisecond,
			Max:        1 * time.Millisecond,
			Multiplier: 1,
		}
	})
	require.NoError(t, cfg.Validate())

	w := newWorker(cfg.Logger, cfg, newRouteStore())

	// Seed one route so probes run during flaps
	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 10))
	w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})

	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	// Ensure we actually entered the loop at least once.
	require.Eventually(t, func() bool { return atomic.LoadInt64(&listenCalls) >= 1 }, 500*time.Millisecond, 5*time.Millisecond)
	// Then confirm ≥3 failures (retries actually happening).
	require.Eventually(t, func() bool { return atomic.LoadInt64(&listenCalls) >= 3 }, 2*time.Second, 5*time.Millisecond)
	// And that the 4th attempt is the long-lived one (running).
	require.Eventually(t, func() bool { return atomic.LoadInt64(&fourthRunning) == 1 }, 2*time.Second, 5*time.Millisecond)
	// Probes are still happening despite earlier listen errors.
	require.Eventually(t, func() bool { return atomic.LoadInt64(&probeStarts) > 0 }, 2*time.Second, 10*time.Millisecond)
	require.True(t, w.IsRunning())

	// Stop cancels the long-lived 4th listen and ends cleanly.
	w.Stop()
	require.Eventually(t, func() bool { return !w.IsRunning() }, time.Second, 10*time.Millisecond)
}

func TestProbing_Worker_ListenBackoff_CapAndCancel(t *testing.T) {
	t.Parallel()

	var calls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.ListenBackoff = ListenBackoffConfig{Initial: 5 * time.Millisecond, Max: 10 * time.Millisecond, Multiplier: 2}
		c.ListenFunc = func(ctx context.Context) error {
			atomic.AddInt64(&calls, 1)
			return errors.New("fail")
		}
	})

	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)

	// Retry a few times quickly.
	require.Eventually(t, func() bool { return atomic.LoadInt64(&calls) >= 3 }, time.Second, 5*time.Millisecond)

	// Stop should break any in-flight backoff sleep promptly.
	w.Stop()
	require.Eventually(t, func() bool { return !w.IsRunning() }, time.Second, 10*time.Millisecond)
}

func TestProbing_Worker_Tick_ProbesEachRouteOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	starts := map[string]int{}
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
		c.MaxConcurrency = 8
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(_ context.Context, r *routing.Route) (ProbeResult, error) {
			mu.Lock()
			starts[r.String()]++
			mu.Unlock()
			time.Sleep(1 * time.Millisecond)
			return ProbeResult{OK: true}, nil
		}
	})
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	var rs []*routing.Route
	for i := 0; i < 10; i++ {
		r := newTestRouteWithDst(net.IPv4(10, 0, 10, byte(i+1)))
		rs = append(rs, r)
		w.store.Set(newRouteKey(r), managedRoute{route: r, liveness: w.cfg.Liveness.NewTracker()})
	}
	w.Tick()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(starts) != len(rs) {
			return false
		}
		for _, r := range rs {
			if starts[r.String()] != 1 {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)
}

func TestProbing_Worker_ValidateRoute(t *testing.T) {
	t.Parallel()

	ipv4 := net.IPv4(10, 0, 0, 1)
	dst := &net.IPNet{IP: net.IPv4(10, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	nexthop := net.IPv4(10, 0, 0, 3)

	tests := []struct {
		name    string
		r       *routing.Route
		wantErr bool
	}{
		{
			name:    "valid route",
			r:       &routing.Route{Src: ipv4, Dst: dst, NextHop: nexthop},
			wantErr: false,
		},
		{
			name:    "nil route",
			r:       nil,
			wantErr: true,
		},
		{
			name:    "nil src",
			r:       &routing.Route{Dst: dst, NextHop: nexthop},
			wantErr: true,
		},
		{
			name: "non-IPv4 src",
			r: &routing.Route{
				Src: net.ParseIP("2001:db8::1"),
				Dst: dst, NextHop: nexthop,
			},
			wantErr: true,
		},
		{
			name:    "nil dst",
			r:       &routing.Route{Src: ipv4, NextHop: nexthop},
			wantErr: true,
		},
		{
			name:    "nil dst IP",
			r:       &routing.Route{Src: ipv4, Dst: &net.IPNet{}, NextHop: nexthop},
			wantErr: true,
		},
		{
			name: "non-IPv4 dst",
			r: &routing.Route{
				Src:     ipv4,
				Dst:     &net.IPNet{IP: net.ParseIP("2001:db8::2"), Mask: net.CIDRMask(128, 128)},
				NextHop: nexthop,
			},
			wantErr: true,
		},
		{
			name:    "nil next hop",
			r:       &routing.Route{Src: ipv4, Dst: dst},
			wantErr: true,
		},
		{
			name: "non-IPv4 next hop",
			r: &routing.Route{
				Src: ipv4, Dst: dst, NextHop: net.ParseIP("2001:db8::3"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRoute(tt.r)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
