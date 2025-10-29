//go:build linux

package probing

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	logger *slog.Logger
)

// TestMain sets up the test environment with a global logger.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: logLevel,
	}))

	os.Exit(m.Run())
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

func newTestConfig(t *testing.T, mutate func(*Config)) *Config {
	cfg := Config{
		Logger:     logger.With("test", t.Name()),
		Context:    t.Context(),
		Netlink:    newMemoryNetlinker(),
		Liveness:   NewHysteresisLivenessPolicy(2, 2),
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			// Avoid starving CPU with a short sleep.
			time.Sleep(1 * time.Millisecond)
			return ProbeResult{OK: true, Sent: 1, Received: 1}, nil
		},
		Interval:       500 * time.Millisecond,
		ProbeTimeout:   time.Second,
		MaxConcurrency: 10,
		ListenBackoff: ListenBackoffConfig{
			Initial:    10 * time.Millisecond,
			Max:        100 * time.Millisecond,
			Multiplier: 2,
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return &cfg
}

func newTestRoute(mutate func(*routing.Route)) *routing.Route {
	r := &routing.Route{
		Table:    100,
		Src:      net.IPv4(10, 4, 0, 1),
		Dst:      &net.IPNet{IP: net.IPv4(10, 4, 0, 11), Mask: net.CIDRMask(32, 32)},
		NextHop:  net.IPv4(10, 5, 0, 1),
		Protocol: unix.RTPROT_BGP,
	}
	if mutate != nil {
		mutate(r)
	}
	return r
}

func newTestRouteWithDst(dst net.IP) *routing.Route {
	return newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: dst, Mask: net.CIDRMask(32, 32)}
	})
}

func hasRouteLiveness(w *probingWorker, r *routing.Route, status LivenessStatus, consecutiveOK uint, consecutiveFail uint) bool {
	mr, ok := w.store.Get(newRouteKey(r))
	if !ok || mr.liveness.Status() != status || mr.liveness.ConsecutiveOK() != consecutiveOK || mr.liveness.ConsecutiveFail() != consecutiveFail {
		w.log.Debug("probing: route liveness mismatch", "route", r.String(), "status", mr.liveness.Status(), "consecutiveOK", mr.liveness.ConsecutiveOK(), "consecutiveFail", mr.liveness.ConsecutiveFail())
		return false
	}
	return true
}

func requireNetlinkRoutes(t *testing.T, netlinker routing.Netlinker, routes []*routing.Route) {
	rts, err := netlinker.RouteByProtocol(unix.RTPROT_BGP)
	require.NoError(t, err)
	require.Equal(t, len(routes), len(rts))
	require.ElementsMatch(t, routes, rts)
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

type memoryNetlinker struct {
	routesByDst map[string][]*routing.Route

	mu sync.Mutex
}

func newMemoryNetlinker() *memoryNetlinker {
	return &memoryNetlinker{
		routesByDst: make(map[string][]*routing.Route),
	}
}

func (m *memoryNetlinker) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routesByDst[r.Dst.IP.String()] = append(m.routesByDst[r.Dst.IP.String()], r)
	return nil
}

func (m *memoryNetlinker) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routesByDst, r.Dst.IP.String())
	return nil
}

func (m *memoryNetlinker) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.routesByDst[ip.String()], nil
}

func (m *memoryNetlinker) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	routes := make([]*routing.Route, 0)
	for _, rs := range m.routesByDst {
		for _, r := range rs {
			if r.Protocol == protocol {
				routes = append(routes, r)
			}
		}
	}
	return routes, nil
}

func (m *memoryNetlinker) RuleAdd(r *routing.IPRule) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) RuleDel(r *routing.IPRule) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) RuleGet(r *routing.IPRule) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) TunnelAdd(t *routing.Tunnel) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) TunnelDelete(t *routing.Tunnel) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) TunnelAddrAdd(t *routing.Tunnel, ip string) error {
	return errors.New("not implemented")
}

func (m *memoryNetlinker) TunnelUp(t *routing.Tunnel) error {
	return errors.New("not implemented")
}

type MockNetlinker struct {
	TunnelAddFunc       func(*routing.Tunnel) error
	TunnelDeleteFunc    func(*routing.Tunnel) error
	TunnelAddrAddFunc   func(*routing.Tunnel, string) error
	TunnelUpFunc        func(*routing.Tunnel) error
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteGetFunc        func(net.IP) ([]*routing.Route, error)
	RuleAddFunc         func(*routing.IPRule) error
	RuleDelFunc         func(*routing.IPRule) error
	RouteByProtocolFunc func(int) ([]*routing.Route, error)

	mu sync.Mutex
}

func (m *MockNetlinker) Update(f func(nl *MockNetlinker)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m)
}

func (m *MockNetlinker) TunnelAdd(t *routing.Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelAddFunc(t)
}

func (m *MockNetlinker) TunnelDelete(t *routing.Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelDeleteFunc(t)
}

func (m *MockNetlinker) TunnelAddrAdd(t *routing.Tunnel, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelAddrAddFunc(t, ip)
}

func (m *MockNetlinker) TunnelUp(t *routing.Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TunnelUpFunc(t)
}

func (m *MockNetlinker) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteAddFunc(r)
}

func (m *MockNetlinker) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteDeleteFunc(r)
}

func (m *MockNetlinker) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteGetFunc(ip)
}

func (m *MockNetlinker) RuleAdd(r *routing.IPRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RuleAddFunc(r)
}

func (m *MockNetlinker) RuleDel(r *routing.IPRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RuleDelFunc(r)
}

func (m *MockNetlinker) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteByProtocolFunc(protocol)
}

type mockLivenessPolicy struct {
	NewTrackerFunc func() LivenessTracker
}

func (m *mockLivenessPolicy) NewTracker() LivenessTracker {
	return m.NewTrackerFunc()
}

type mockLivenessTracker struct {
	OnProbeFunc         func(bool) LivenessTransition
	StatusFunc          func() LivenessStatus
	ConsecutiveOKFunc   func() uint
	ConsecutiveFailFunc func() uint
}

func (m *mockLivenessTracker) OnProbe(ok bool) LivenessTransition {
	return m.OnProbeFunc(ok)
}
func (m *mockLivenessTracker) Status() LivenessStatus {
	return m.StatusFunc()
}
func (m *mockLivenessTracker) ConsecutiveOK() uint {
	return m.ConsecutiveOKFunc()
}
func (m *mockLivenessTracker) ConsecutiveFail() uint {
	return m.ConsecutiveFailFunc()
}
