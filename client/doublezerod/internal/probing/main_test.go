package probing

import (
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
		Level:      logLevel,
		TimeFormat: time.RFC3339,
	}))

	os.Exit(m.Run())
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
