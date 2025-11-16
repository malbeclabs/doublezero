package liveness

import (
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestClient_Liveness_RouteRW_RouteAdd_RegistersWithManager(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	backend := &MockRouteReaderWriter{}
	rrw := NewRouteReaderWriter(m, backend, "test-iface")

	r := newTestRoute(nil)
	err = rrw.RouteAdd(r)
	require.NoError(t, err)

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.sessions, 1)

	var peer Peer
	for p := range m.sessions {
		peer = p
		break
	}
	require.Equal(t, "test-iface", peer.Interface)
	require.Equal(t, r.Src.To4().String(), peer.LocalIP)
	require.Equal(t, r.Dst.IP.To4().String(), peer.PeerIP)
}

func TestClient_Liveness_RouteRW_RouteDelete_WithdrawsFromManager(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	backend := &MockRouteReaderWriter{}
	rrw := NewRouteReaderWriter(m, backend, "test-iface")

	r := newTestRoute(nil)

	require.NoError(t, m.RegisterRoute(r, "test-iface"))

	m.mu.Lock()
	require.Len(t, m.sessions, 1)
	m.mu.Unlock()

	err = rrw.RouteDelete(r)
	require.NoError(t, err)

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.sessions, 0, "session should be removed after RouteDelete/WithdrawRoute")
}

func TestClient_Liveness_RouteRW_RouteAdd_PassiveMode_PassesThroughToBackend(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)

	backend := &MockRouteReaderWriter{
		RouteAddFunc: func(r *routing.Route) error {
			addCh <- r
			return nil
		},
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = true
		cfg.Netlinker = backend
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	rrw := NewRouteReaderWriter(m, backend, "lo")
	r := newTestRoute(nil)

	err = rrw.RouteAdd(r)
	require.NoError(t, err)

	added := wait(t, addCh, time.Second, "RouteAdd passthrough in PassiveMode")
	require.Equal(t, r.Table, added.Table)
	require.Equal(t, r.Src.String(), added.Src.String())
	require.Equal(t, r.Dst.String(), added.Dst.String())
	require.Equal(t, r.NextHop.String(), added.NextHop.String())
}

func TestClient_Liveness_RouteRW_RouteDelete_PassiveMode_PassesThroughToBackend(t *testing.T) {
	t.Parallel()

	delCh := make(chan *routing.Route, 1)

	backend := &MockRouteReaderWriter{
		RouteDeleteFunc: func(r *routing.Route) error {
			delCh <- r
			return nil
		},
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = true
		cfg.Netlinker = backend
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	rrw := NewRouteReaderWriter(m, backend, "lo")
	r := newTestRoute(nil)

	// Seed a session so WithdrawRoute has something to work with.
	require.NoError(t, m.RegisterRoute(r, "lo"))

	err = rrw.RouteDelete(r)
	require.NoError(t, err)

	deleted := wait(t, delCh, time.Second, "RouteDelete passthrough in PassiveMode")
	require.Equal(t, r.Table, deleted.Table)
	require.Equal(t, r.Src.String(), deleted.Src.String())
	require.Equal(t, r.Dst.String(), deleted.Dst.String())
	require.Equal(t, r.NextHop.String(), deleted.NextHop.String())
}

func TestClient_Liveness_RouteRW_RouteByProtocol_NonBGP_DelegatesToBackend(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	expected := []*routing.Route{newTestRoute(nil)}
	var seenProtocol int

	backend := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			seenProtocol = p
			return expected, nil
		},
	}

	rrw := NewRouteReaderWriter(m, backend, "test-iface")

	const proto = 123 // anything that's not unix.RTPROT_BGP (186)
	require.NotEqual(t, unix.RTPROT_BGP, proto)
	routes, err := rrw.RouteByProtocol(proto)
	require.NoError(t, err)
	require.Equal(t, proto, seenProtocol, "backend should see the same protocol")
	require.Equal(t, expected, routes, "wrapper should return backend routes as-is")
}

func TestClient_Liveness_RouteRW_RouteByProtocol_BGP_UsesManagerSessions(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	backendCalled := false
	backend := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) {
			backendCalled = true
			return nil, nil
		},
	}

	rrw := NewRouteReaderWriter(m, backend, "lo")

	// Register two distinct routes so we have two sessions.
	r1 := newTestRoute(nil)
	r2 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{
			IP:   net.IPv4(10, 4, 0, 12),
			Mask: net.CIDRMask(32, 32),
		}
	})

	require.NoError(t, m.RegisterRoute(r1, "lo"))
	require.NoError(t, m.RegisterRoute(r2, "lo"))

	routes, err := rrw.RouteByProtocol(unix.RTPROT_BGP)
	require.NoError(t, err)

	require.False(t, backendCalled, "backend.RouteByProtocol should not be called for BGP protocol")
	require.Len(t, routes, 2)

	got := map[string]bool{}
	for _, r := range routes {
		got[r.Dst.String()] = true
	}

	require.True(t, got[r1.Dst.String()], "BGP RouteByProtocol should include first tracked route")
	require.True(t, got[r2.Dst.String()], "BGP RouteByProtocol should include second tracked route")
}
