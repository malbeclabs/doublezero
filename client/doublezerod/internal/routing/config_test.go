package routing

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClient_RoutingConfig_InitialExcludeBlocksRoute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, []string{"10.0.0.0"})

	var adds atomic.Int64
	var deletes atomic.Int64

	nlr := newMockNetlinker()
	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteAddFunc = func(*Route) error { adds.Add(1); return nil }
	})

	cr, err := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cr)

	require.NoError(t, cr.RouteAdd(cidr(t, "10.0.0.0/8"))) // excluded → blocked
	require.Equal(t, int64(0), adds.Load())

	require.NoError(t, cr.RouteAdd(cidr(t, "192.168.0.0/16"))) // allowed → forwarded
	require.Equal(t, int64(1), adds.Load())

	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteDeleteFunc = func(*Route) error { deletes.Add(1); return nil }
	})

	require.NoError(t, cr.RouteDelete(cidr(t, "10.0.0.0/8"))) // excluded → blocked
	require.Equal(t, int64(0), deletes.Load())

	require.NoError(t, cr.RouteDelete(cidr(t, "192.168.0.0/16"))) // allowed → forwarded
	require.Equal(t, int64(1), deletes.Load())

}

func TestClient_RoutingConfig_InvalidIPs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		exclude []string
	}{
		{
			name:    "invalid-string",
			exclude: []string{"invalid"},
		},
		{
			name:    "CIDR-string",
			exclude: []string{"10.0.0.0/8"},
		},
		{
			name:    "extra-octet-1",
			exclude: []string{"10.0.0.0.0"},
		},
		{
			name:    "extra-octet-2",
			exclude: []string{"10.0.0.0.0.0"},
		},
		{
			name:    "extra-octet-3",
			exclude: []string{"10.0.0.0.0.0.0"},
		},
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			writeConfig(t, cfgPath, tc.exclude)
			cr, err := NewConfiguredRouteReaderWriter(discardLogger(), newMockNetlinker(), cfgPath)
			require.EqualError(t, err, fmt.Sprintf("error loading route config: invalid ip: %s", tc.exclude[0]))
			require.Nil(t, cr)
		})
	}
}

func TestClient_RoutingConfig_ReinitWithNewConfigUpdatesExclude(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, []string{"10.0.0.0"})

	var adds atomic.Int64
	nlr := newMockNetlinker()
	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteAddFunc = func(*Route) error { adds.Add(1); return nil }
	})

	// First instance: 10/8 excluded; 172.16/12 allowed
	cr1, err := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cr1)
	require.NoError(t, cr1.RouteAdd(cidr(t, "10.0.0.0/8")))
	require.Equal(t, int64(0), adds.Load())
	require.NoError(t, cr1.RouteAdd(cidr(t, "172.16.0.0/12")))
	require.Equal(t, int64(1), adds.Load())

	// Change config and create a new instance
	writeConfig(t, cfgPath, []string{"172.16.0.0"})
	cr2, err := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cr2)

	// Now 10/8 should be allowed; 172.16/12 should be blocked
	require.NoError(t, cr2.RouteAdd(cidr(t, "10.0.0.0/8")))
	require.Equal(t, int64(2), adds.Load())
	require.NoError(t, cr2.RouteAdd(cidr(t, "172.16.0.0/12")))
	require.Equal(t, int64(2), adds.Load())
}

func TestClient_RoutingConfig_NoExcludesForwardsAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, nil) // empty exclude list

	var adds atomic.Int64
	nlr := newMockNetlinker()
	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteAddFunc = func(*Route) error { adds.Add(1); return nil }
	})

	cr, err := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cr)

	require.NoError(t, cr.RouteAdd(cidr(t, "10.0.0.0/8")))
	require.NoError(t, cr.RouteAdd(cidr(t, "172.16.0.0/12")))
	require.NoError(t, cr.RouteAdd(cidr(t, "192.168.0.0/16")))
	require.Equal(t, int64(3), adds.Load())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func cidr(t *testing.T, s string) *Route {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return &Route{Dst: n}
}

func writeConfig(t *testing.T, path string, excludes []string) {
	t.Helper()
	cfg := RouteConfig{Exclude: excludes}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func newMockNetlinker() *MockNetlinker {
	m := &MockNetlinker{}
	m.Update(func(nl *MockNetlinker) {
		nl.TunnelAddFunc = func(*Tunnel) error { return nil }
		nl.TunnelDeleteFunc = func(*Tunnel) error { return nil }
		nl.TunnelAddrAddFunc = func(*Tunnel, string) error { return nil }
		nl.TunnelUpFunc = func(*Tunnel) error { return nil }
		nl.RouteAddFunc = func(*Route) error { return nil }
		nl.RouteDeleteFunc = func(*Route) error { return nil }
		nl.RouteGetFunc = func(net.IP) ([]*Route, error) { return nil, nil }
		nl.RuleAddFunc = func(*IPRule) error { return nil }
		nl.RuleDelFunc = func(*IPRule) error { return nil }
		nl.RouteByProtocolFunc = func(int) ([]*Route, error) { return nil, nil }
	})
	return m
}

type MockNetlinker struct {
	TunnelAddFunc       func(*Tunnel) error
	TunnelDeleteFunc    func(*Tunnel) error
	TunnelAddrAddFunc   func(*Tunnel, string) error
	TunnelUpFunc        func(*Tunnel) error
	RouteAddFunc        func(*Route) error
	RouteDeleteFunc     func(*Route) error
	RouteGetFunc        func(net.IP) ([]*Route, error)
	RuleAddFunc         func(*IPRule) error
	RuleDelFunc         func(*IPRule) error
	RouteByProtocolFunc func(int) ([]*Route, error)

	mu sync.Mutex
}

func (m *MockNetlinker) Update(f func(nl *MockNetlinker)) { m.mu.Lock(); defer m.mu.Unlock(); f(m) }
func (m *MockNetlinker) TunnelAdd(t *Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelAddFunc(t)
}
func (m *MockNetlinker) TunnelDelete(t *Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelDeleteFunc(t)
}
func (m *MockNetlinker) TunnelAddrAdd(t *Tunnel, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelAddrAddFunc(t, ip)
}
func (m *MockNetlinker) TunnelUp(t *Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelUpFunc(t)
}
func (m *MockNetlinker) RouteAdd(r *Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteAddFunc(r)
}
func (m *MockNetlinker) RouteDelete(r *Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteDeleteFunc(r)
}
func (m *MockNetlinker) RouteGet(ip net.IP) ([]*Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteGetFunc(ip)
}
func (m *MockNetlinker) RuleAdd(r *IPRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RuleAddFunc(r)
}
func (m *MockNetlinker) RuleDel(r *IPRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RuleDelFunc(r)
}
func (m *MockNetlinker) RouteByProtocol(protocol int) ([]*Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteByProtocolFunc(protocol)
}
