//go:build linux

package probing

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

func TestProbing_RouteManager_PeerLifecycle_StartsAndStopsWorker_Idempotent(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = newFakeScheduler()
	})
	m, err := NewRouteManager(cfg)
	require.NoError(t, err)

	require.False(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnEstablished())
	require.True(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnEstablished())

	require.Equal(t, 0, m.store.Len())

	require.NoError(t, m.PeerOnClose())
	require.False(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnClose())

	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_RouteAdd_WorkerRunning_StoresButNoKernelAdd(t *testing.T) {
	t.Parallel()

	var addCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = newFakeScheduler()
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCalls, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { return nil },
		}
	})
	m, err := NewRouteManager(cfg)
	require.NoError(t, err)

	require.NoError(t, m.PeerOnEstablished())
	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 10))

	require.NoError(t, m.RouteAdd(r))
	require.Equal(t, 1, m.store.Len())
	require.Equal(t, int64(0), atomic.LoadInt64(&addCalls))
}

func TestProbing_RouteManager_RouteAdd_WorkerStopped_CallsKernelAdd(t *testing.T) {
	t.Parallel()

	var addCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		// No scheduler/limiter needed since worker won't be started
		c.Netlink = &MockNetlinker{
			RouteAddFunc:    func(*routing.Route) error { atomic.AddInt64(&addCalls, 1); return nil },
			RouteDeleteFunc: func(*routing.Route) error { return nil },
		}
	})
	m, err := NewRouteManager(cfg)
	require.NoError(t, err)

	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 11))
	require.NoError(t, m.RouteAdd(r))
	require.Equal(t, int64(1), atomic.LoadInt64(&addCalls))
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_RouteAdd_InvalidRoute_Err(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(1)
		c.Scheduler = newFakeScheduler()
	})
	m, _ := NewRouteManager(cfg)
	_ = m.PeerOnEstablished()
	err := m.RouteAdd(&routing.Route{}) // invalid
	require.Error(t, err)
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_RouteDelete_WorkerRunning_RemovesAndKernelDelete(t *testing.T) {
	t.Parallel()

	var delCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = newFakeScheduler()
		c.Netlink = &MockNetlinker{
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return nil },
		}
	})
	m, _ := NewRouteManager(cfg)
	require.NoError(t, m.PeerOnEstablished())

	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 20))
	require.NoError(t, m.RouteAdd(r))
	require.Equal(t, 1, m.store.Len())

	require.NoError(t, m.RouteDelete(r))
	require.Equal(t, 0, m.store.Len())
	require.Equal(t, int64(1), atomic.LoadInt64(&delCalls))
}

func TestProbing_RouteManager_RouteDelete_WorkerRunning_RouteNotFoundInKernel_DoesNotReturnError(t *testing.T) {
	t.Parallel()

	var delCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = newFakeScheduler()
		c.Netlink = &MockNetlinker{
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return routing.ErrRouteNotFound },
		}
	})
	m, _ := NewRouteManager(cfg)
	require.NoError(t, m.PeerOnEstablished())

	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 20))
	require.NoError(t, m.RouteAdd(r))
	require.Equal(t, 1, m.store.Len())

	require.NoError(t, m.RouteDelete(r))
	require.Equal(t, 0, m.store.Len())
	require.Equal(t, int64(1), atomic.LoadInt64(&delCalls))
}

func TestProbing_RouteManager_RouteDelete_WorkerStopped_CallsKernelDelete(t *testing.T) {
	t.Parallel()

	var delCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Netlink = &MockNetlinker{
			RouteDeleteFunc: func(*routing.Route) error { atomic.AddInt64(&delCalls, 1); return nil },
		}
	})
	m, _ := NewRouteManager(cfg)

	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 21))
	require.NoError(t, m.RouteDelete(r))
	require.Equal(t, 0, m.store.Len())
	require.Equal(t, int64(1), atomic.LoadInt64(&delCalls))
}

func TestProbing_RouteManager_RouteDelete_InvalidRoute_Err(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(1)
		c.Scheduler = newFakeScheduler()
	})
	m, _ := NewRouteManager(cfg)
	_ = m.PeerOnEstablished()
	err := m.RouteDelete(&routing.Route{})
	require.Error(t, err)
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_RouteByProtocol_Passthrough(t *testing.T) {
	t.Parallel()

	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 30))

	cfg := newTestConfig(t, func(c *Config) {
		c.Netlink = &MockNetlinker{
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) {
				return []*routing.Route{r}, nil
			},
		}
	})
	m, _ := NewRouteManager(cfg)

	rs, err := m.RouteByProtocol(123)
	require.NoError(t, err)
	require.Equal(t, 1, len(rs))
	require.Equal(t, r.String(), rs[0].String())
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_NewRouteManager_ConfigValidateErrorBubbles(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) { c.Logger = nil })
	_, err := NewRouteManager(cfg)
	require.Error(t, err)
}

func TestProbing_RouteManager_PeerOnEstablished_StartsWorkerAndProbes(t *testing.T) {
	t.Parallel()

	probed := make(chan struct{}, 1)
	sched := newFakeScheduler()
	cfg := newTestConfig(t, func(c *Config) {
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			select {
			case probed <- struct{}{}:
			default:
			}
			return ProbeResult{OK: true}, nil
		}
		c.Limiter, _ = NewSemaphoreLimiter(4)
		c.Scheduler = sched
	})
	m, _ := NewRouteManager(cfg)

	r1 := newTestRouteWithDst(net.IPv4(10, 0, 0, 40))
	require.NoError(t, m.RouteAdd(r1))
	require.NoError(t, m.PeerOnEstablished())
	require.Eventually(t, func() bool { return m.worker.IsRunning() }, 2*time.Second, 10*time.Millisecond)

	r2 := newTestRouteWithDst(net.IPv4(10, 0, 0, 41))
	require.NoError(t, m.RouteAdd(r2))

	stop := startNudger(t, sched, 5*time.Millisecond)
	t.Cleanup(stop)
	waitEdge(t, probed, 2*time.Second, "probe did not start")

	require.NoError(t, m.PeerOnClose())
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_PeerOnEstablished_ClearsStore(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(1)
		c.Scheduler = newFakeScheduler()
	})
	m, _ := NewRouteManager(cfg)
	require.NoError(t, m.PeerOnEstablished())
	require.NoError(t, m.RouteAdd(newTestRouteWithDst(net.IPv4(10, 0, 0, 41))))
	require.Equal(t, 1, m.store.Len())
	require.NoError(t, m.PeerOnEstablished())
	require.Equal(t, 0, m.store.Len())
}

func TestProbing_RouteManager_PeerOnClose_ClearsStore(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) {
		c.Limiter, _ = NewSemaphoreLimiter(1)
		c.Scheduler = newFakeScheduler()
	})
	m, _ := NewRouteManager(cfg)
	require.NoError(t, m.PeerOnEstablished())
	require.NoError(t, m.RouteAdd(newTestRouteWithDst(net.IPv4(10, 0, 0, 41))))
	require.Equal(t, 1, m.store.Len())
	require.NoError(t, m.PeerOnClose())
	require.Equal(t, 0, m.store.Len())
}
