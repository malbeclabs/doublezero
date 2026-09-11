package reconcile

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/prometheus/client_golang/prometheus"
	prom "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// mockNetlinker implements routing.Netlinker with overridable route and tunnel
// hooks; all other methods are no-ops.
type mockNetlinker struct {
	mu sync.Mutex

	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteByProtocolFunc func(int) ([]*routing.Route, error)
	TunnelDeleteFunc    func(*routing.Tunnel) error

	addCalls    []*routing.Route
	deleteCalls []*routing.Route
}

func (m *mockNetlinker) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addCalls = append(m.addCalls, r)
	if m.RouteAddFunc != nil {
		return m.RouteAddFunc(r)
	}
	return nil
}

func (m *mockNetlinker) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, r)
	if m.RouteDeleteFunc != nil {
		return m.RouteDeleteFunc(r)
	}
	return nil
}

func (m *mockNetlinker) RouteByProtocol(p int) ([]*routing.Route, error) {
	if m.RouteByProtocolFunc != nil {
		return m.RouteByProtocolFunc(p)
	}
	return nil, nil
}

func (m *mockNetlinker) TunnelDelete(t *routing.Tunnel) error {
	if m.TunnelDeleteFunc != nil {
		return m.TunnelDeleteFunc(t)
	}
	return nil
}

func (m *mockNetlinker) addCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.addCalls)
}

func (m *mockNetlinker) TunnelAdd(*routing.Tunnel) error                  { return nil }
func (m *mockNetlinker) TunnelDown(*routing.Tunnel) error                 { return nil }
func (m *mockNetlinker) TunnelAddrAdd(*routing.Tunnel, string, int) error { return nil }
func (m *mockNetlinker) TunnelUp(*routing.Tunnel) error                   { return nil }
func (m *mockNetlinker) RouteGet(net.IP) ([]*routing.Route, error)        { return nil, nil }
func (m *mockNetlinker) RuleAdd(*routing.IPRule) error                    { return nil }
func (m *mockNetlinker) RuleDel(*routing.IPRule) error                    { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestRoute(mutate func(*routing.Route)) *routing.Route {
	r := &routing.Route{
		Table:    unix.RT_TABLE_MAIN,
		Src:      net.IP{10, 4, 0, 1},
		Dst:      &net.IPNet{IP: net.IP{10, 4, 0, 11}, Mask: net.CIDRMask(32, 32)},
		NextHop:  net.IP{10, 5, 0, 1},
		Protocol: unix.RTPROT_BGP,
	}
	if mutate != nil {
		mutate(r)
	}
	return r
}

func getCounterValue(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			if metricHasLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func metricHasLabels(m *prom.Metric, labels prometheus.Labels) bool {
	got := make(map[string]string, len(m.Label))
	for _, lp := range m.Label {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range labels {
		if got[k] != v {
			return false
		}
	}
	return true
}

func TestClient_Reconcile_ReinstallsMissing(t *testing.T) {
	t.Parallel()

	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	reg := prometheus.NewRegistry()
	rc := New(testLogger(), mock, 0, reg)

	r := newTestRoute(nil)
	require.NoError(t, rc.RouteAdd(r))
	require.Equal(t, 1, mock.addCount())

	rc.reconcile()

	require.Equal(t, 2, mock.addCount(), "missing tracked route must be reinstalled")
	reinstalls := getCounterValue(t, reg, "doublezero_route_reconcile_reinstalls_total",
		prometheus.Labels{"local_ip": "10.4.0.1"})
	require.Equal(t, float64(1), reinstalls)
}

func TestClient_Reconcile_SkipsPresent(t *testing.T) {
	t.Parallel()

	// The kernel echo is a freshly-constructed route with equal field values in
	// 16-byte net.IP form, exercising key normalization rather than pointer
	// identity.
	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) {
			return []*routing.Route{{
				Table:    unix.RT_TABLE_MAIN,
				Src:      net.ParseIP("10.4.0.1"),
				Dst:      &net.IPNet{IP: net.ParseIP("10.4.0.11").To4(), Mask: net.CIDRMask(32, 32)},
				NextHop:  net.ParseIP("10.5.0.1"),
				Protocol: unix.RTPROT_BGP,
			}}, nil
		},
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	require.NoError(t, rc.RouteAdd(newTestRoute(nil)))
	require.Equal(t, 1, mock.addCount())

	rc.reconcile()

	require.Equal(t, 1, mock.addCount(), "route present in kernel must not be reinstalled")
}

func TestClient_Reconcile_PrefixMismatchReinstalls(t *testing.T) {
	t.Parallel()

	// A same-IP kernel route with a different mask must not satisfy the tracked
	// /32.
	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) {
			return []*routing.Route{newTestRoute(func(r *routing.Route) {
				r.Dst = &net.IPNet{IP: net.IP{10, 4, 0, 11}, Mask: net.CIDRMask(24, 32)}
			})}, nil
		},
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	require.NoError(t, rc.RouteAdd(newTestRoute(nil)))
	rc.reconcile()

	require.Equal(t, 2, mock.addCount(), "a /24 kernel route must not satisfy a tracked /32")
}

func TestClient_Reconcile_UntracksOnDelete_ProtocolAgnostic(t *testing.T) {
	t.Parallel()

	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	require.NoError(t, rc.RouteAdd(newTestRoute(nil)))

	// BGP withdraw-driven deletes are constructed without Protocol
	// (bgp/plugin.go); untracking must still match.
	withdraw := newTestRoute(func(r *routing.Route) { r.Protocol = 0 })
	require.NoError(t, rc.RouteDelete(withdraw))

	rc.reconcile()
	require.Equal(t, 1, mock.addCount(), "a withdrawn route must not be reinstalled")
}

func TestClient_Reconcile_DeleteDuringReconcileNotResurrected(t *testing.T) {
	t.Parallel()

	// Simulate the resurrection race window: the kernel scan sees the route
	// missing, then a withdrawal lands before the reinstall re-check. The
	// tracked-set re-check under the lock must observe the withdrawal and skip.
	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	r := newTestRoute(nil)
	require.NoError(t, rc.RouteAdd(r))
	require.Equal(t, 1, mock.addCount())

	rc.mu.Lock()
	toCheckLen := len(rc.tracked)
	rc.mu.Unlock()
	require.Equal(t, 1, toCheckLen)

	// Withdraw between the kernel scan and the re-check: reconcile() below
	// re-scans (kernel empty) but must find the tracked entry gone.
	require.NoError(t, rc.RouteDelete(r))

	rc.reconcile()
	require.Equal(t, 1, mock.addCount(), "route withdrawn before the re-check must not be resurrected")
}

func TestClient_Reconcile_NonBGPNotTracked(t *testing.T) {
	t.Parallel()

	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	mroute := newTestRoute(func(r *routing.Route) { r.Protocol = unix.RTPROT_STATIC })
	require.NoError(t, rc.RouteAdd(mroute))
	require.Equal(t, 1, mock.addCount())

	rc.reconcile()
	require.Equal(t, 1, mock.addCount(), "non-BGP routes must not be tracked or reinstalled")
}

func TestClient_Reconcile_ReinstallFailureIncrementsFailureMetric(t *testing.T) {
	t.Parallel()

	failing := false
	mock := &mockNetlinker{
		RouteAddFunc: func(*routing.Route) error {
			if failing {
				return errors.New("boom")
			}
			return nil
		},
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	reg := prometheus.NewRegistry()
	rc := New(testLogger(), mock, 0, reg)

	require.NoError(t, rc.RouteAdd(newTestRoute(nil)))

	mock.mu.Lock()
	failing = true
	mock.mu.Unlock()

	rc.reconcile()

	failures := getCounterValue(t, reg, "doublezero_route_reconcile_failures_total",
		prometheus.Labels{"local_ip": "10.4.0.1"})
	require.Equal(t, float64(1), failures)
	reinstalls := getCounterValue(t, reg, "doublezero_route_reconcile_reinstalls_total",
		prometheus.Labels{"local_ip": "10.4.0.1"})
	require.Equal(t, float64(0), reinstalls, "a failed reinstall must not count as a reinstall")

	// The route stays tracked, so the next tick retries.
	rc.reconcile()
	failures = getCounterValue(t, reg, "doublezero_route_reconcile_failures_total",
		prometheus.Labels{"local_ip": "10.4.0.1"})
	require.Equal(t, float64(2), failures, "a failed reinstall must stay tracked and retry")
}

func TestClient_Reconcile_TunnelDeletePurgesRoutes(t *testing.T) {
	t.Parallel()

	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	rc := New(testLogger(), mock, 0, prometheus.NewRegistry())

	// Two routes via nexthop 10.5.0.1 (the tunnel being deleted), one via
	// another tunnel's nexthop.
	require.NoError(t, rc.RouteAdd(newTestRoute(nil)))
	require.NoError(t, rc.RouteAdd(newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: net.IP{10, 4, 0, 12}, Mask: net.CIDRMask(32, 32)}
	})))
	survivor := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: net.IP{10, 4, 0, 13}, Mask: net.CIDRMask(32, 32)}
		r.NextHop = net.IP{10, 6, 0, 1}
	})
	require.NoError(t, rc.RouteAdd(survivor))
	require.Equal(t, 3, mock.addCount())

	// NoUninstall teardown: no RouteDelete is issued; the tunnel is deleted and
	// the kernel drops its routes implicitly.
	require.NoError(t, rc.TunnelDelete(&routing.Tunnel{RemoteOverlay: net.IP{10, 5, 0, 1}}))

	rc.reconcile()

	// Only the survivor (still tracked, missing from kernel) is reinstalled;
	// the two purged routes must not be resurrected onto the dead tunnel.
	require.Equal(t, 4, mock.addCount(), "only the surviving route must be reinstalled")
	mock.mu.Lock()
	last := mock.addCalls[len(mock.addCalls)-1]
	mock.mu.Unlock()
	require.Equal(t, survivor.Dst.String(), last.Dst.String())
}

func TestClient_Reconcile_ExcludedRoutesNeverTracked(t *testing.T) {
	t.Parallel()

	// Production layering: ConfiguredRouteReaderWriter sits above the
	// Reconciler and no-ops RouteAdd for excluded destinations, so they never
	// reach this layer and are never tracked or churned.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"exclude":["10.4.0.11"]}`), 0o600))
	cr, err := routing.NewConfiguredRoutes(cfgPath)
	require.NoError(t, err)

	mock := &mockNetlinker{
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	reg := prometheus.NewRegistry()
	rc := New(testLogger(), mock, 0, reg)
	crw, err := routing.NewConfiguredRouteReaderWriter(testLogger(), rc, cr)
	require.NoError(t, err)

	require.NoError(t, crw.RouteAdd(newTestRoute(nil)))

	rc.reconcile()

	require.Equal(t, 0, mock.addCount(), "excluded routes must never reach the kernel or the tracked set")
	reinstalls := getCounterValue(t, reg, "doublezero_route_reconcile_reinstalls_total",
		prometheus.Labels{"local_ip": "10.4.0.1"})
	require.Equal(t, float64(0), reinstalls)
}

func TestClient_Reconcile_StartDisabledWithZeroInterval(t *testing.T) {
	t.Parallel()

	rc := New(testLogger(), &mockNetlinker{}, 0, prometheus.NewRegistry())
	rc.Start(t.Context())
	rc.Stop() // must not hang or panic with no ticker goroutine
}
