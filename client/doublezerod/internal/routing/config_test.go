package routing

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_RoutingConfig_InitialExcludeBlocksRoute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, []string{"10.0.0.0/8"})

	var adds atomic.Int64
	nlr := newMockNetlinker()
	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteAddFunc = func(*Route) error { adds.Add(1); return nil }
	})

	cr := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NotNil(t, cr)
	t.Cleanup(func() { _ = cr.Close() })

	require.NoError(t, cr.RouteAdd(cidr(t, "10.0.0.0/8")))
	require.Equal(t, int64(0), adds.Load())

	require.NoError(t, cr.RouteAdd(cidr(t, "192.168.0.0/16")))
	require.Eventually(t, func() bool { return adds.Load() == 1 }, 3*time.Second, 100*time.Millisecond)
}

func TestClient_RoutingConfig_ReloadUpdatesExclude(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, []string{"10.0.0.0/8"})

	var adds atomic.Int64
	nlr := newMockNetlinker()
	nlr.Update(func(nl *MockNetlinker) {
		nl.RouteAddFunc = func(*Route) error { adds.Add(1); return nil }
	})

	cr := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NotNil(t, cr)
	t.Cleanup(func() { _ = cr.Close() })

	// Sanity: 10/8 excluded, 172.16/12 allowed
	require.NoError(t, cr.RouteAdd(cidr(t, "10.0.0.0/8")))
	require.Equal(t, int64(0), adds.Load())

	require.NoError(t, cr.RouteAdd(cidr(t, "172.16.0.0/12")))
	require.Eventually(t, func() bool { return adds.Load() == 1 }, 3*time.Second, 100*time.Millisecond)

	// Flip exclusion to 172.16/12 and allow 10/8
	writeConfig(t, cfgPath, []string{"172.16.0.0/12"})

	// Wait until 10/8 becomes allowed (indicates reload applied)
	require.Eventually(t, func() bool {
		_ = cr.RouteAdd(cidr(t, "10.0.0.0/8"))
		return adds.Load() == 2
	}, 3*time.Second, 100*time.Millisecond)

	// Now 172.16/12 should be blocked (no further increment)
	_ = cr.RouteAdd(cidr(t, "172.16.0.0/12"))
	require.Eventually(t, func() bool { return adds.Load() == 2 }, 3*time.Second, 100*time.Millisecond)
}

func TestClient_RoutingConfig_CloseStopsWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	writeConfig(t, cfgPath, []string{"10.0.0.0/8"})

	nlr := newMockNetlinker()
	cr := NewConfiguredRouteReaderWriter(discardLogger(), nlr, cfgPath)
	require.NotNil(t, cr)

	require.NoError(t, cr.Close())

	// After close, touching the file should not crash; RouteAdd should still behave.
	writeConfig(t, cfgPath, []string{"192.168.0.0/16"})
	_ = cr.RouteAdd(cidr(t, "10.0.0.0/8"))
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
	// Default no-ops for everything except RouteAdd, which tests override.
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

func (m *MockNetlinker) Update(f func(nl *MockNetlinker)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m)
}

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
