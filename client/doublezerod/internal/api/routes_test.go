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
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

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

	nh1 := net.ParseIP("203.0.113.1")
	nh2 := net.ParseIP("203.0.113.2")
	nh6 := net.ParseIP("2001:db8::dead")

	routes := []*routing.Route{
		{
			Src:     ip4Src2,
			Dst:     &net.IPNet{IP: ip4Dst2, Mask: net.CIDRMask(32, 32)},
			NextHop: nh2,
		},
		{
			Src:     ip4Src1,
			Dst:     &net.IPNet{IP: ip4Dst1, Mask: net.CIDRMask(32, 32)},
			NextHop: nh1,
		},
		{
			Src:     ip6Src,
			Dst:     &net.IPNet{IP: ip6Dst, Mask: net.CIDRMask(128, 128)},
			NextHop: nh6,
		},
	}

	userType1 := UserTypeIBRL
	userType2 := UserTypeMulticast

	svc1 := &ProvisionRequest{
		UserType:     userType1,
		DoubleZeroIP: ip4Src1,
		TunnelSrc:    ip4Src1,
		TunnelNet:    &net.IPNet{IP: nh1, Mask: net.CIDRMask(32, 32)},
	}
	svc2 := &ProvisionRequest{
		UserType:     userType2,
		DoubleZeroIP: ip4Src2,
		TunnelSrc:    ip4Src2,
		TunnelNet:    &net.IPNet{IP: nh2, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return routes, nil
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc1, svc2}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)

	// Two IPv4 routes that match services; IPv6 is filtered out.
	require.Len(t, got, 2)

	want := []Route{
		{
			Network:     config.EnvLocalnet,
			LocalIP:     "10.0.0.1",
			PeerIP:      "192.0.2.1",
			KernelState: liveness.KernelStatePresent.String(),
		},
		{
			Network:     config.EnvLocalnet,
			LocalIP:     "10.0.0.2",
			PeerIP:      "192.0.2.2",
			KernelState: liveness.KernelStatePresent.String(),
		},
	}

	for i := range want {
		require.Equalf(t, want[i].Network, got[i].Network, "route[%d] Network", i)
		require.Equalf(t, want[i].LocalIP, got[i].LocalIP, "route[%d] LocalIP", i)
		require.Equalf(t, want[i].PeerIP, got[i].PeerIP, "route[%d] PeerIP", i)
		require.Equalf(t, want[i].KernelState, got[i].KernelState, "route[%d] KernelState", i)
		require.Emptyf(t, got[i].LivenessLastUpdated, "route[%d] LivenessLastUpdated", i)
		require.Emptyf(t, got[i].LivenessState, "route[%d] LivenessState", i)
		require.Emptyf(t, got[i].LivenessStateReason, "route[%d] LivenessStateReason", i)
		require.Emptyf(t, got[i].LivenessExpectedKernelState, "route[%d] LivenessExpectedKernelState", i)
		require.Emptyf(t, got[i].LivenessPeerMode, "route[%d] LivenessPeerMode", i)
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
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Contains(t, rr.Body.String(), "failed to get routes")
}

func TestClient_API_ServeRoutesHandler_WithLiveness_KernelOnly(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")
	userType := UserTypeIBRL

	routes := []*routing.Route{
		{
			Src:     ipSrc,
			Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
			NextHop: nextHop,
		},
	}

	svc := &ProvisionRequest{
		UserType:     userType,
		DoubleZeroIP: ipSrc,
		TunnelSrc:    ipSrc,
		TunnelNet:    &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return routes, nil
		},
	}
	lm := &mockLivenessManager{
		GetSessionsFunc: func() []liveness.SessionSnapshot {
			return nil
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, db, nc)
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
	require.Equal(t, liveness.KernelStatePresent.String(), rt.KernelState)
	require.Empty(t, rt.LivenessLastUpdated)
	require.Empty(t, rt.LivenessState)
	require.Empty(t, rt.LivenessStateReason)
	require.Empty(t, rt.LivenessExpectedKernelState)
	require.Empty(t, rt.LivenessPeerMode)
	require.Empty(t, rt.PeerClientVersion)
}

func TestClient_API_ServeRoutesHandler_WithLiveness_PresentInBoth(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")
	userType := UserTypeIBRL

	route := &routing.Route{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: nextHop,
	}

	now := time.Now().UTC()
	sess := liveness.SessionSnapshot{
		Route:               liveness.Route{Route: *route},
		LastUpdated:         now,
		State:               liveness.StateUp,
		ExpectedKernelState: liveness.KernelStatePresent,
		PeerAdvertisedMode:  liveness.PeerModeActive,
		PeerClientVersion:   liveness.ClientVersion{Major: 1, Minor: 2, Patch: 3, Channel: liveness.VersionChannelStable},
	}

	svc := &ProvisionRequest{
		UserType:     userType,
		DoubleZeroIP: ipSrc,
		TunnelSrc:    ipSrc,
		TunnelNet:    &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return []*routing.Route{route}, nil
		},
	}
	lm := &mockLivenessManager{
		GetSessionsFunc: func() []liveness.SessionSnapshot {
			return []liveness.SessionSnapshot{sess}
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, db, nc)
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
	require.Equal(t, liveness.KernelStatePresent.String(), rt.KernelState)
	require.Equal(t, liveness.StateUp.String(), rt.LivenessState)
	require.NotEmpty(t, rt.LivenessLastUpdated)
	require.Empty(t, rt.LivenessStateReason)
	require.Equal(t, liveness.KernelStatePresent.String(), rt.LivenessExpectedKernelState)
	require.Equal(t, LivenessPeerModeActive.String(), rt.LivenessPeerMode)
	require.Equal(t, sess.PeerClientVersion.String(), rt.PeerClientVersion)
}

func TestClient_API_ServeRoutesHandler_WithLiveness_AbsentInKernel(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")
	userType := UserTypeIBRL

	route := &routing.Route{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: nextHop,
	}

	now := time.Now().UTC()
	sess := liveness.SessionSnapshot{
		Route:               liveness.Route{Route: *route},
		LastUpdated:         now,
		State:               liveness.StateDown,
		ExpectedKernelState: liveness.KernelStateAbsent,
		PeerAdvertisedMode:  liveness.PeerModePassive,
		PeerClientVersion:   liveness.ClientVersion{Major: 1, Minor: 2, Patch: 3, Channel: liveness.VersionChannelStable},
	}

	svc := &ProvisionRequest{
		UserType:     userType,
		DoubleZeroIP: ipSrc,
		TunnelSrc:    ipSrc,
		TunnelNet:    &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	lm := &mockLivenessManager{
		GetSessionsFunc: func() []liveness.SessionSnapshot {
			return []liveness.SessionSnapshot{sess}
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	err := json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Len(t, got, 1)

	rt := got[0]
	require.Equal(t, liveness.KernelStateAbsent.String(), rt.KernelState)
	require.Equal(t, liveness.StateDown.String(), rt.LivenessState)
	require.NotEmpty(t, rt.LivenessLastUpdated)
	require.Equal(t, liveness.KernelStateAbsent.String(), rt.LivenessExpectedKernelState)
	require.Equal(t, LivenessPeerModePassive.String(), rt.LivenessPeerMode)
	require.Equal(t, liveness.DownReasonNone.String(), rt.LivenessStateReason)
	require.Equal(t, sess.PeerClientVersion.String(), rt.PeerClientVersion)
}

func TestClient_API_ServeRoutesHandler_WithLiveness_SetsLivenessStateReason(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")
	userType := UserTypeIBRL

	route := &routing.Route{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: nextHop,
	}

	now := time.Now().UTC()
	sess := liveness.SessionSnapshot{
		Route:               liveness.Route{Route: *route},
		LastUpdated:         now,
		State:               liveness.StateDown,
		LastDownReason:      liveness.DownReasonRemoteAdmin,
		ExpectedKernelState: liveness.KernelStateAbsent,
		PeerAdvertisedMode:  liveness.PeerModePassive,
		PeerClientVersion:   liveness.ClientVersion{Major: 1, Minor: 2, Patch: 3, Channel: liveness.VersionChannelStable},
	}

	svc := &ProvisionRequest{
		UserType:     userType,
		DoubleZeroIP: ipSrc,
		TunnelSrc:    ipSrc,
		TunnelNet:    &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	lm := &mockLivenessManager{
		GetSessionsFunc: func() []liveness.SessionSnapshot {
			return []liveness.SessionSnapshot{sess}
		},
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, lm, db, nc)
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
	require.Equal(t, liveness.KernelStateAbsent.String(), rt.KernelState)
	require.Equal(t, liveness.StateDown.String(), rt.LivenessState)
	require.NotEmpty(t, rt.LivenessLastUpdated)
	require.Equal(t, liveness.KernelStateAbsent.String(), rt.LivenessExpectedKernelState)
	require.Equal(t, LivenessPeerModePassive.String(), rt.LivenessPeerMode)
	require.Equal(t, liveness.DownReasonRemoteAdmin.String(), rt.LivenessStateReason)
	require.Equal(t, sess.PeerClientVersion.String(), rt.PeerClientVersion)
}

func TestServeRoutesHandler_UsesDoubleZeroIP_NotTunnelSrc(t *testing.T) {
	t.Parallel()

	dzIP := net.ParseIP("10.10.10.10")   // DoubleZeroIP — should match rt.Src
	tunnelSrc := net.ParseIP("10.0.0.1") // TunnelSrc — intentionally different
	dst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")

	route := &routing.Route{
		Src:     dzIP,
		Dst:     &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)},
		NextHop: nextHop,
	}

	svc := &ProvisionRequest{
		UserType:     UserTypeIBRL,
		DoubleZeroIP: dzIP,
		TunnelSrc:    tunnelSrc,
		TunnelNet:    &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) {
			return []*routing.Route{route}, nil
		},
	}

	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}

	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest {
			return []*ProvisionRequest{svc}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))

	require.Len(t, got, 1)

	require.Equal(t, "10.10.10.10", got[0].LocalIP) // proves DoubleZeroIP was used
	require.Equal(t, "192.0.2.1", got[0].PeerIP)
	require.Equal(t, liveness.KernelStatePresent.String(), got[0].KernelState)
}

func TestServeRoutesHandler_RequiresDoubleZeroIPForKernelMatch(t *testing.T) {
	t.Parallel()

	ipSrc := net.ParseIP("10.0.0.1")
	ipDst := net.ParseIP("192.0.2.1")
	nextHop := net.ParseIP("203.0.113.1")

	routes := []*routing.Route{{
		Src:     ipSrc,
		Dst:     &net.IPNet{IP: ipDst, Mask: net.CIDRMask(32, 32)},
		NextHop: nextHop,
	}}

	svc := &ProvisionRequest{
		UserType:  UserTypeIBRL,
		TunnelSrc: ipSrc,
		TunnelNet: &net.IPNet{IP: nextHop, Mask: net.CIDRMask(32, 32)},
		// DoubleZeroIP intentionally nil: should not match
	}

	rrw := &mockRouteReaderWriter{
		RouteByProtocolFunc: func(_ int) ([]*routing.Route, error) { return routes, nil },
	}
	nc := &config.NetworkConfig{Moniker: config.EnvLocalnet}
	db := &mockServiceStateReader{
		GetProvisionedServicesFunc: func() []*ProvisionRequest { return []*ProvisionRequest{svc} },
	}

	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	rr := httptest.NewRecorder()

	handler := ServeRoutesHandler(rrw, nil, db, nc)
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got []Route
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	require.Len(t, got, 0)
}

type mockLivenessManager struct {
	GetSessionsFunc func() []liveness.SessionSnapshot
}

func (m *mockLivenessManager) GetSessions() []liveness.SessionSnapshot {
	if m.GetSessionsFunc == nil {
		return nil
	}
	return m.GetSessionsFunc()
}

type mockServiceStateReader struct {
	GetProvisionedServicesFunc func() []*ProvisionRequest
}

func (m *mockServiceStateReader) GetProvisionedServices() []*ProvisionRequest {
	if m.GetProvisionedServicesFunc == nil {
		return nil
	}
	return m.GetProvisionedServicesFunc()
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
