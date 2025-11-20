package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
	"github.com/stretchr/testify/require"
)

func TestServeRoutesHandler_NoLiveness_EmptyRoutes(t *testing.T) {
	t.Parallel()

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	ct := rr.Header().Get("Content-Type")
	require.Equal(t, "application/json", ct)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Len(t, got, 0)
}

func TestServeRoutesHandler_NoLiveness_WithIPv4AndIPv6(t *testing.T) {
	t.Parallel()

	ip4Src1 := net.ParseIP("10.0.0.1")
	ip4Dst1 := net.ParseIP("192.0.2.1")
	ip4Src2 := net.ParseIP("10.0.0.2")
	ip4Dst2 := net.ParseIP("192.0.2.2")
	ip6Src := net.ParseIP("2001:db8::1")
	ip6Dst := net.ParseIP("2001:db8::2")

	routes := []*routing.Route{
		{
			Src:     ip4Src2,
			Dst:     &net.IPNet{IP: ip4Dst2, Mask: net.CIDRMask(32, 32)},
			NextHop: net.ParseIP("203.0.113.2"),
		},
		{
			Src:     ip4Src1,
			Dst:     &net.IPNet{IP: ip4Dst1, Mask: net.CIDRMask(32, 32)},
			NextHop: net.ParseIP("203.0.113.1"),
		},
		{
			Src:     ip6Src,
			Dst:     &net.IPNet{IP: ip6Dst, Mask: net.CIDRMask(128, 128)},
			NextHop: net.ParseIP("2001:db8::dead"),
		},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return routes, nil
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	ct := rr.Header().Get("Content-Type")
	require.Equal(t, "application/json", ct)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)

	require.Len(t, got, 2)

	want := []Route{
		{
			Network: config.EnvLocalnet,
			LocalIP: "10.0.0.1",
			PeerIP:  "192.0.2.1",
			RTState: RTStatePresent,
		},
		{
			Network: config.EnvLocalnet,
			LocalIP: "10.0.0.2",
			PeerIP:  "192.0.2.2",
			RTState: RTStatePresent,
		},
	}

	for i := range want {
		require.Equalf(t, want[i].Network, got[i].Network, "route[%d] Network", i)
		require.Equalf(t, want[i].LocalIP, got[i].LocalIP, "route[%d] LocalIP", i)
		require.Equalf(t, want[i].PeerIP, got[i].PeerIP, "route[%d] PeerIP", i)
		require.Equalf(t, want[i].RTState, got[i].RTState, "route[%d] RTState", i)
		require.Emptyf(t, got[i].LivenessLastUpdated, "route[%d] LivenessLastUpdated", i)
		require.Emptyf(t, got[i].LivenessState, "route[%d] LivenessState", i)
	}
}

func TestServeRoutesHandler_RouteByProtocolError(t *testing.T) {
	t.Parallel()

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return nil, errors.New("boom")
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)

	body := rr.Body.String()
	require.Contains(t, body, "failed to get routes")
}

func TestClient_API_ServeRoutesHandler_WithLiveness_KernelOnly(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")

	routes := []*routing.Route{
		{
			Src:     ipSrc,
			Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
			NextHop: net.ParseIP("203.0.113.1"),
		},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return routes, nil
		},
	}
	lm := &mockLivenessManager{sessions: nil}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Len(t, got, 1)

	rt := got[0]
	require.Equal(t, config.EnvLocalnet, rt.Network)
	require.Equal(t, "10.0.0.1", rt.LocalIP)
	require.Equal(t, "192.0.2.1", rt.PeerIP)
	require.Equal(t, RTStatePresent, rt.RTState)
	require.Empty(t, rt.LivenessLastUpdated)
	require.Empty(t, rt.LivenessState)
}

func TestClient_API_ServeRoutesHandler_WithLiveness_PresentInBoth(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")

	route := &routing.Route{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: net.ParseIP("203.0.113.1"),
	}

	now := time.Now().UTC()
	sess := liveness.SessionSnapshot{
		Route:       *route,
		LastUpdated: now,
		State:       liveness.StateUp,
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return []*routing.Route{route}, nil
		},
	}
	lm := &mockLivenessManager{sessions: []liveness.SessionSnapshot{sess}}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Len(t, got, 1)

	rt := got[0]
	require.Equal(t, config.EnvLocalnet, rt.Network)
	require.Equal(t, "10.0.0.1", rt.LocalIP)
	require.Equal(t, "192.0.2.1", rt.PeerIP)
	require.Equal(t, RTStatePresent, rt.RTState)
	require.Equal(t, liveness.StateUp.String(), rt.LivenessState)
	require.NotEmpty(t, rt.LivenessLastUpdated)
}

func TestClient_API_ServeRoutesHandler_WithLiveness_AbsentInKernel(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")

	route := &routing.Route{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: net.ParseIP("203.0.113.1"),
	}

	now := time.Now().UTC()
	sess := liveness.SessionSnapshot{
		Route:       *route,
		LastUpdated: now,
		State:       liveness.StateDown,
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	lm := &mockLivenessManager{sessions: []liveness.SessionSnapshot{sess}}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Len(t, got, 1)

	rt := got[0]
	require.Equal(t, RTStateAbsent, rt.RTState)
	require.Equal(t, liveness.StateDown.String(), rt.LivenessState)
	require.NotEmpty(t, rt.LivenessLastUpdated)
}

type mockLivenessManager struct {
	sessions []liveness.SessionSnapshot
}

func (m *mockLivenessManager) GetSessions() []liveness.SessionSnapshot {
	return m.sessions
}

type mockRouteReaderWriter struct {
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteGetFunc        func(net.IP) ([]*routing.Route, error)
	RouteByProtocolFunc func(int) ([]*routing.Route, error)

	mu sync.Mutex
}

func (m *mockRouteReaderWriter) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteAddFunc == nil {
		return nil
	}
	return m.RouteAddFunc(r)
}

func (m *mockRouteReaderWriter) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteDeleteFunc == nil {
		return nil
	}
	return m.RouteDeleteFunc(r)
}

func (m *mockRouteReaderWriter) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteGetFunc == nil {
		return nil, nil
	}
	return m.RouteGetFunc(ip)
}

func (m *mockRouteReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteByProtocolFunc == nil {
		return nil, nil
	}
	return m.RouteByProtocolFunc(protocol)
}
