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

	cfg := newTestConfig(t, func(c *Config) { c.Interval = 24 * time.Hour })
	m, err := NewRouteManager(cfg)
	require.NoError(t, err)

	require.False(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnEstablished())
	require.True(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnEstablished())

	require.NoError(t, m.PeerOnClose())
	require.False(t, m.worker.IsRunning())
	require.NoError(t, m.PeerOnClose())
}

func TestProbing_RouteManager_RouteAdd_WorkerRunning_StoresButNoKernelAdd(t *testing.T) {
	t.Parallel()

	var addCalls int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = time.Hour
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
		c.Interval = time.Hour
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

	cfg := newTestConfig(t, nil)
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
		c.Interval = time.Hour
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
	require.Equal(t, int64(1), atomic.LoadInt64(&delCalls))
}

func TestProbing_RouteManager_RouteDelete_InvalidRoute_Err(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, nil)
	m, _ := NewRouteManager(cfg)
	_ = m.PeerOnEstablished()
	err := m.RouteDelete(&routing.Route{})
	require.Error(t, err)
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
}

func TestProbing_RouteManager_NewRouteManager_ConfigValidateErrorBubbles(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t, func(c *Config) { c.Interval = 0 })
	_, err := NewRouteManager(cfg)
	require.Error(t, err)
}

func TestProbing_RouteManager_PeerOnEstablished_StartsWorker_TicksRun(t *testing.T) {
	t.Parallel()

	var started int64
	cfg := newTestConfig(t, func(c *Config) {
		c.Interval = 10 * time.Millisecond
		c.Liveness = seqPolicy([]LivenessTransition{LivenessTransitionNoChange})
		c.ProbeFunc = func(context.Context, *routing.Route) (ProbeResult, error) {
			atomic.AddInt64(&started, 1)
			return ProbeResult{OK: true}, nil
		}
	})
	m, _ := NewRouteManager(cfg)
	r := newTestRouteWithDst(net.IPv4(10, 0, 0, 40))
	require.NoError(t, m.RouteAdd(r))
	require.NoError(t, m.PeerOnEstablished())
	require.NoError(t, m.RouteAdd(newTestRouteWithDst(net.IPv4(10, 0, 0, 41))))
	require.Eventually(t, func() bool { return atomic.LoadInt64(&started) > 0 }, time.Second, 10*time.Millisecond)
	require.NoError(t, m.PeerOnClose())
}
