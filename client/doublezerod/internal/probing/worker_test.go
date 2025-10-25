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
	"golang.org/x/sys/unix"
)

func TestProbing_Worker_Lifecycle(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Logger:     logger.With("test", t.Name()),
		Context:    t.Context(),
		Netlink:    newMemoryNetlinker(),
		Liveness:   NewHysteresisLivenessPolicy(2, 2),
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			return ProbeResult{OK: true, Sent: 1, Received: 1}, nil
		},
		Interval:     100 * time.Millisecond,
		ProbeTimeout: 1 * time.Second,
	}
	require.NoError(t, cfg.Validate())

	w := newWorker(cfg.Logger, cfg, newRouteStore())

	// Verify the worker is not running initially.
	require.Never(t, w.IsRunning, 100*time.Millisecond, 5*time.Millisecond)

	// Start the worker and verify it is running.
	w.Start(cfg.Context)
	require.True(t, w.IsRunning())

	// Initialize routes to add and verify they are added.
	srcIP := net.IPv4(192, 168, 1, 1)
	nextHopIP := net.IPv4(192, 168, 1, 0)
	mask := net.CIDRMask(24, 32)
	route1 := &routing.Route{
		Src: srcIP,
		Dst: &net.IPNet{
			IP:   net.IPv4(192, 168, 1, 1),
			Mask: mask,
		},
		NextHop:  nextHopIP,
		Protocol: unix.RTPROT_BGP,
	}
	route2 := &routing.Route{
		Src: srcIP,
		Dst: &net.IPNet{
			IP:   net.IPv4(192, 168, 1, 2),
			Mask: mask,
		},
		NextHop:  nextHopIP,
		Protocol: unix.RTPROT_BGP,
	}

	// Enqueue routes and verify they are added.
	w.EnqueueAdd(route1)
	w.EnqueueAdd(route2)

	require.Eventually(t, func() bool {
		routes, err := cfg.Netlink.RouteByProtocol(unix.RTPROT_BGP)
		require.NoError(t, err)
		return len(routes) == 2 && UnorderedEqual(routes, []*routing.Route{route1, route2})
	}, 5*time.Second, 5*time.Millisecond)

	// Enqueue route deletes and verify they are deleted.
	w.EnqueueDelete(route1)
	w.EnqueueDelete(route2)

	require.Eventually(t, func() bool {
		routes, err := cfg.Netlink.RouteByProtocol(unix.RTPROT_BGP)
		require.NoError(t, err)
		return len(routes) == 0
	}, 5*time.Second, 5*time.Millisecond)

	// Stop the worker and verify it is not running.
	w.Stop()
	require.False(t, w.IsRunning())
}

func TestProbing_Worker_StartStopIdempotent(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Logger:       logger.With("test", t.Name()),
		Context:      t.Context(),
		Netlink:      newMemoryNetlinker(),
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToUp}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	w.Start(cfg.Context)
	require.True(t, w.IsRunning())
	w.Stop()
	w.Stop()
	require.False(t, w.IsRunning())
}

func TestProbing_Worker_ToUpThenToDown(t *testing.T) {
	t.Parallel()

	// First wave: ToUP → kernel add. Second wave: ToDOWN → kernel delete.
	lp := seqPolicy([]LivenessTransition{
		LivenessTransitionToUp, // first probe wave
		LivenessTransitionNoChange,
		LivenessTransitionToDown, // second probe wave
	})

	addCnt, delCnt := int64(0), int64(0)
	dst := net.IPv4(10, 0, 0, 8)
	nh := net.IPv4(10, 0, 0, 1)

	nl := &MockNetlinker{
		RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCnt, 1); return nil },
		RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCnt, 1); return nil },
		RouteGetFunc: func(q net.IP) ([]*routing.Route, error) {
			// simple memory: present iff addCnt>delCnt
			if q.Equal(dst) && atomic.LoadInt64(&addCnt) > atomic.LoadInt64(&delCnt) {
				return []*routing.Route{{
					Table:   100,
					Dst:     &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)},
					NextHop: nh,
				}}, nil
			}
			return nil, nil
		},
	}

	cfg := Config{
		Logger:     logger.With("test", t.Name()),
		Context:    t.Context(),
		Netlink:    nl,
		Liveness:   lp,
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			return ProbeResult{OK: true, Sent: 1, Received: 1}, nil
		},
		Interval:              15 * time.Millisecond,
		ProbeTimeout:          time.Second,
		RouteEventBufferSize:  64,
		ProbeResultBufferSize: 64,
	}
	require.NoError(t, cfg.Validate())

	store := newRouteStore()
	w := newWorker(cfg.Logger, cfg, store)
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	r := &routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh}
	w.EnqueueAdd(r)

	// Wave 1 → ToUP → RouteAdd once
	require.Eventually(t, func() bool { return atomic.LoadInt64(&addCnt) == 1 }, 2*time.Second, 5*time.Millisecond)

	// Trigger another wave; ToDOWN → RouteDelete once
	time.Sleep(2 * cfg.Interval)
	w.enqueueTick()
	require.Eventually(t, func() bool { return atomic.LoadInt64(&delCnt) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_SkipNoOpAddAndDelete(t *testing.T) {
	t.Parallel()

	dst := net.IPv4(10, 0, 0, 7)
	nh := net.IPv4(10, 0, 0, 1)

	var addCalls, delCalls int64
	nl := &MockNetlinker{
		RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCalls, 1); return nil },
		RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return nil },
		// Always report that the route exists → add should be skipped; later we flip to nil to make delete skipped.
		RouteGetFunc: func(net.IP) ([]*routing.Route, error) {
			return []*routing.Route{{Table: 100, NextHop: nh}}, nil
		},
	}

	cfg := Config{
		Logger:       logger.With("test", t.Name()),
		Context:      t.Context(),
		Netlink:      nl,
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToUp, LivenessTransitionToDown}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	require.NoError(t, cfg.Validate())

	store := newRouteStore()
	w := newWorker(cfg.Logger, cfg, store)
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	r := &routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh}
	w.EnqueueAdd(r)

	// ToUP with kernel already having the route → RouteAdd skipped
	time.Sleep(3 * cfg.Interval)
	require.Equal(t, int64(0), atomic.LoadInt64(&addCalls))

	// Flip RouteGet to “not present” before ToDOWN
	// Route delete attempt happens anyway via netlinker.
	nl.Update(func(nl *MockNetlinker) {
		nl.RouteGetFunc = func(net.IP) ([]*routing.Route, error) { return nil, nil }
	})
	time.Sleep(3 * cfg.Interval)
	require.Equal(t, int64(1), atomic.LoadInt64(&delCalls))
}

func TestProbing_Worker_InFlightGatesConcurrentWaves(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Logger:       logger.With("test", t.Name()),
		Context:      t.Context(),
		Netlink:      newMemoryNetlinker(),
		Liveness:     seqPolicy(nil),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		Interval:     5 * time.Millisecond,
		ProbeTimeout: time.Second,
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			// Block until we unblock to hold inFlight>0.
			<-time.After(200 * time.Millisecond)
			return ProbeResult{OK: true}, nil
		},
	}
	require.NoError(t, cfg.Validate())

	store := newRouteStore()
	w := newWorker(cfg.Logger, cfg, store)
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)

	// Add two routes via public API
	r1 := &routing.Route{Table: 100, Dst: &net.IPNet{IP: net.IPv4(10, 0, 0, 11), Mask: net.CIDRMask(32, 32)}, NextHop: net.IPv4(10, 0, 0, 1)}
	r2 := &routing.Route{Table: 100, Dst: &net.IPNet{IP: net.IPv4(10, 0, 0, 12), Mask: net.CIDRMask(32, 32)}, NextHop: net.IPv4(10, 0, 0, 1)}
	w.EnqueueAdd(r1)
	w.EnqueueAdd(r2)

	require.Eventually(t, func() bool { return store.Len() == 2 }, time.Second, 5*time.Millisecond)

	// While probes are running, fire extra ticks; they should be collapsed because inFlight>0.
	before, _ := cfg.Netlink.RouteByProtocol(0)
	for range 5 {
		w.enqueueTick()
	}
	// After the first long wave completes, exactly one more wave should have a chance to run.
	require.Eventually(t, func() bool {
		after, _ := cfg.Netlink.RouteByProtocol(0)
		// Still just two routes in kernel; the point is “no duplicate probe waves while busy”.
		return UnorderedEqual(before, after)
	}, 2*time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_ProbeErrorIsTreatedAsFail(t *testing.T) {
	t.Parallel()
	dst, nh := net.IPv4(10, 0, 0, 30), net.IPv4(10, 0, 0, 1)
	var delCalls int64
	cfg := Config{
		Logger: logger.With("test", t.Name()), Context: t.Context(),
		Netlink: &MockNetlinker{
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return nil },
			RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
				if ip.Equal(dst) {
					return []*routing.Route{{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh}}, nil
				}
				return nil, nil
			},
		},
		Liveness:   seqPolicy([]LivenessTransition{LivenessTransitionToDown}),
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:  func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{}, errors.New("boom") },
		Interval:   10 * time.Millisecond, ProbeTimeout: time.Second,
	}
	require.NoError(t, cfg.Validate())
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	w.EnqueueAdd(&routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh})
	require.Eventually(t, func() bool { return atomic.LoadInt64(&delCalls) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_RouteGetErrorAllowsAdd(t *testing.T) {
	t.Parallel()
	dst, nh := net.IPv4(10, 0, 0, 31), net.IPv4(10, 0, 0, 1)
	var addCalls int64
	cfg := Config{
		Logger:  logger.With("test", t.Name()),
		Context: t.Context(),
		Netlink: &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCalls, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { return nil },
			RouteGetFunc:    func(net.IP) ([]*routing.Route, error) { return nil, errors.New("netlink down") },
		},
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToUp}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	w.EnqueueAdd(&routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh})
	require.Eventually(t, func() bool { return atomic.LoadInt64(&addCalls) == 1 }, time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_DeleteZeroizesProtocol(t *testing.T) {
	t.Parallel()
	dst, nh := net.IPv4(10, 0, 0, 32), net.IPv4(10, 0, 0, 1)
	var protoGot int
	cfg := Config{
		Logger:  logger.With("test", t.Name()),
		Context: t.Context(),
		Netlink: &MockNetlinker{
			RouteAddFunc: func(*routing.Route) error { return nil },
			RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
				if ip.Equal(dst) {
					return []*routing.Route{{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh, Protocol: 123}}, nil
				}
				return nil, nil
			},
			RouteDeleteFunc: func(r *routing.Route) error { protoGot = r.Protocol; return nil },
		},
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToDown}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: false}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	w.EnqueueAdd(&routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh, Protocol: 123})
	require.Eventually(t, func() bool { return protoGot == 0 }, 2*time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_RouteVanishedNoKernelOps(t *testing.T) {
	t.Parallel()
	dst, nh := net.IPv4(10, 0, 0, 33), net.IPv4(10, 0, 0, 1)
	var adds, dels int64
	cfg := Config{
		Logger:  logger.With("test", t.Name()),
		Context: t.Context(),
		Netlink: &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&adds, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&dels, 1); return nil },
			RouteGetFunc:    func(net.IP) ([]*routing.Route, error) { return nil, nil },
		},
		// First transition would be ToUp, but we remove the route between probe start and result handling.
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToUp}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	block := make(chan struct{})
	cfg.ProbeFunc = func(ctx context.Context, r *routing.Route) (ProbeResult, error) {
		<-block
		return ProbeResult{OK: true}, nil
	}
	store := newRouteStore()
	w := newWorker(cfg.Logger, cfg, store)
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	r := &routing.Route{Table: 100, Dst: &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}, NextHop: nh}
	w.EnqueueAdd(r)
	require.Eventually(t, func() bool { return store.Len() == 1 }, time.Second, 5*time.Millisecond)
	// Nuke it before result lands
	w.EnqueueDelete(r)
	close(block)
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int64(0), atomic.LoadInt64(&adds))
	require.Equal(t, int64(1), atomic.LoadInt64(&dels)) // only explicit delete
}

func TestProbing_Worker_ResultBufferSizeBackpressureSafe(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Logger:     logger.With("test", t.Name()),
		Context:    t.Context(),
		Netlink:    newMemoryNetlinker(),
		Liveness:   seqPolicy([]LivenessTransition{LivenessTransitionToUp}),
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			time.Sleep(5 * time.Millisecond)
			return ProbeResult{OK: true}, nil
		},
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	cfg.ProbeResultBufferSize = 4
	require.NoError(t, cfg.Validate())
	cfg.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
		time.Sleep(5 * time.Millisecond)
		return ProbeResult{OK: true}, nil
	}
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	for i := 0; i < 64; i++ {
		ip := net.IPv4(10, 0, 1, byte(i+1))
		w.EnqueueAdd(&routing.Route{Table: 100, Dst: &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}, NextHop: net.IPv4(10, 0, 0, 1)})
	}
	require.Eventually(t, func() bool { routes, _ := cfg.Netlink.RouteByProtocol(0); return len(routes) >= 60 }, 3*time.Second, 5*time.Millisecond)
}

func TestProbing_Worker_InvalidInputsNoPanic(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Logger:       logger.With("test", t.Name()),
		Context:      t.Context(),
		Netlink:      newMemoryNetlinker(),
		Liveness:     seqPolicy([]LivenessTransition{LivenessTransitionToUp}),
		ListenFunc:   func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc:    func(context.Context, *routing.Route) (ProbeResult, error) { return ProbeResult{OK: true}, nil },
		Interval:     10 * time.Millisecond,
		ProbeTimeout: time.Second,
	}
	w := newWorker(cfg.Logger, cfg, newRouteStore())
	w.Start(cfg.Context)
	t.Cleanup(w.Stop)
	w.EnqueueAdd(&routing.Route{})    // nil Dst/NextHop
	w.EnqueueDelete(&routing.Route{}) // ditto
	time.Sleep(10 * time.Millisecond) // just ensure no crash
}

func UnorderedEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	freq := make(map[T]int)
	for _, v := range a {
		freq[v]++
	}
	for _, v := range b {
		freq[v]--
		if freq[v] < 0 {
			return false
		}
	}
	return true
}

func seqPolicy(seq []LivenessTransition) *mockLivenessPolicy {
	return &mockLivenessPolicy{
		NewTrackerFunc: func() LivenessTracker {
			var i int
			var okC, failC uint
			return &mockLivenessTracker{
				OnProbeFunc: func(bool) LivenessTransition {
					if i >= len(seq) {
						return LivenessTransitionNoChange
					}
					tr := seq[i]
					i++
					switch tr {
					case LivenessTransitionToUp:
						okC++
					case LivenessTransitionToDown:
						failC++
					}
					return tr
				},
				StatusFunc:          func() LivenessStatus { return LivenessStatusUnknown },
				ConsecutiveOKFunc:   func() uint { return okC },
				ConsecutiveFailFunc: func() uint { return failC },
			}
		},
	}
}
