package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/config"
	"github.com/stretchr/testify/require"
)

type mockNetlinker struct {
	RouteGetFunc func(net.IP) ([]*routing.Route, error)
	mu           sync.Mutex
}

func (m *mockNetlinker) TunnelAdd(*routing.Tunnel) error                  { return nil }
func (m *mockNetlinker) TunnelDelete(*routing.Tunnel) error               { return nil }
func (m *mockNetlinker) TunnelAddrAdd(*routing.Tunnel, string, int) error { return nil }
func (m *mockNetlinker) TunnelUp(*routing.Tunnel) error                   { return nil }
func (m *mockNetlinker) RouteAdd(*routing.Route) error                    { return nil }
func (m *mockNetlinker) RouteDelete(*routing.Route) error                 { return nil }
func (m *mockNetlinker) RuleAdd(*routing.IPRule) error                    { return nil }
func (m *mockNetlinker) RuleDel(*routing.IPRule) error                    { return nil }
func (m *mockNetlinker) RouteByProtocol(int) ([]*routing.Route, error)    { return nil, nil }

func (m *mockNetlinker) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteGetFunc == nil {
		return nil, nil
	}
	return m.RouteGetFunc(ip)
}

func TestServeResolveRouteHandler_Success(t *testing.T) {
	t.Parallel()

	dstIP := net.ParseIP("192.0.2.1")
	srcIP := net.ParseIP("10.0.0.1")
	nextHop := net.ParseIP("203.0.113.1")

	nlr := &mockNetlinker{
		RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
			require.Equal(t, dstIP, ip)
			return []*routing.Route{
				{
					Dst:     &net.IPNet{IP: dstIP, Mask: net.CIDRMask(32, 32)},
					Src:     srcIP,
					NextHop: nextHop,
				},
			}, nil
		},
	}

	reqBody := ResolveRouteRequest{Dst: dstIP}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got ResolveRouteResponse
	err = json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, srcIP, got.Src)
}

func TestServeResolveRouteHandler_NotFound(t *testing.T) {
	t.Parallel()

	dstIP := net.ParseIP("192.0.2.1")
	otherIP := net.ParseIP("192.0.2.2")

	nlr := &mockNetlinker{
		RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
			require.Equal(t, dstIP, ip)
			return []*routing.Route{
				{
					Dst: &net.IPNet{IP: otherIP, Mask: net.CIDRMask(32, 32)},
					Src: net.ParseIP("10.0.0.1"),
				},
			}, nil
		},
	}

	reqBody := ResolveRouteRequest{Dst: dstIP}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "route not found")
}

func TestServeResolveRouteHandler_EmptyRoutes(t *testing.T) {
	t.Parallel()

	dstIP := net.ParseIP("192.0.2.1")

	nlr := &mockNetlinker{
		RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
			require.Equal(t, dstIP, ip)
			return nil, nil
		},
	}

	reqBody := ResolveRouteRequest{Dst: dstIP}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "route not found")
}

func TestServeResolveRouteHandler_RouteGetError(t *testing.T) {
	t.Parallel()

	dstIP := net.ParseIP("192.0.2.1")

	nlr := &mockNetlinker{
		RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
			require.Equal(t, dstIP, ip)
			return nil, errors.New("netlink error")
		},
	}

	reqBody := ResolveRouteRequest{Dst: dstIP}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Contains(t, rr.Body.String(), "failed to resolve route")
}

func TestServeResolveRouteHandler_MalformedJSON(t *testing.T) {
	t.Parallel()

	nlr := &mockNetlinker{}

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "malformed request")
}

func TestServeResolveRouteHandler_MissingDst(t *testing.T) {
	t.Parallel()

	nlr := &mockNetlinker{}

	reqBody := ResolveRouteRequest{Dst: nil}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "invalid request")
}

func TestServeResolveRouteHandler_EmptyBody(t *testing.T) {
	t.Parallel()

	nlr := &mockNetlinker{}

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "malformed request")
}

func TestServeResolveRouteHandler_MultipleRoutes_FirstMatch(t *testing.T) {
	t.Parallel()

	dstIP := net.ParseIP("192.0.2.1")
	srcIP1 := net.ParseIP("10.0.0.1")
	srcIP2 := net.ParseIP("10.0.0.2")

	nlr := &mockNetlinker{
		RouteGetFunc: func(ip net.IP) ([]*routing.Route, error) {
			require.Equal(t, dstIP, ip)
			return []*routing.Route{
				{
					Dst: &net.IPNet{IP: dstIP, Mask: net.CIDRMask(32, 32)},
					Src: srcIP1,
				},
				{
					Dst: &net.IPNet{IP: dstIP, Mask: net.CIDRMask(32, 32)},
					Src: srcIP2,
				},
			}, nil
		},
	}

	reqBody := ResolveRouteRequest{Dst: dstIP}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/resolve-route", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := ServeResolveRouteHandler(nlr, &config.NetworkConfig{})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got ResolveRouteResponse
	err = json.NewDecoder(rr.Body).Decode(&got)
	require.NoError(t, err)
	require.Equal(t, srcIP1, got.Src)
}
