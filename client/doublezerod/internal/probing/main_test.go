//go:build linux

package probing

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime/pprof"
	"strings"
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
	w := io.Discard
	if verbose {
		w = os.Stdout
		logLevel = slog.LevelDebug
	}
	logger = slog.New(tint.NewHandler(w, &tint.Options{
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
	liveness, err := NewHysteresisLivenessPolicy(2, 2)
	if err != nil {
		require.NoError(t, err)
	}
	limiter, err := NewSemaphoreLimiter(10)
	if err != nil {
		require.NoError(t, err)
	}
	scheduler, err := NewIntervalScheduler(500*time.Millisecond, 0.1, false)
	if err != nil {
		require.NoError(t, err)
	}
	cfg := Config{
		Logger:     logger.With("test", t.Name()),
		Context:    t.Context(),
		Netlink:    newMemoryNetlinker(),
		Liveness:   liveness,
		Limiter:    limiter,
		Scheduler:  scheduler,
		ListenFunc: func(ctx context.Context) error { <-ctx.Done(); return nil },
		ProbeFunc: func(context.Context, *routing.Route) (ProbeResult, error) {
			// Avoid starving CPU with a short sleep.
			time.Sleep(1 * time.Millisecond)
			return ProbeResult{OK: true, Sent: 1, Received: 1}, nil
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

type fakeScheduler struct {
	mu       sync.Mutex
	keys     map[RouteKey]struct{}
	inflight map[RouteKey]bool

	// wave coordination
	wavePending bool          // set by Trigger(); consumed by PopDue() once
	waveOut     int           // number of items handed out this wave and not yet completed
	waveDone    chan struct{} // closed when waveOut -> 0

	// wake/broadcast
	wake chan struct{} // closed to signal; recreated after each signal
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{
		keys:     make(map[RouteKey]struct{}),
		inflight: make(map[RouteKey]bool),
		wake:     make(chan struct{}),
	}
}

func (s *fakeScheduler) String() string { return "fakeScheduler" }

func (s *fakeScheduler) Wake() <-chan struct{} {
	s.mu.Lock()
	ch := s.wake
	s.mu.Unlock()
	return ch
}

func (s *fakeScheduler) signalLocked() {
	old := s.wake
	s.wake = make(chan struct{})
	close(old)
}

func (s *fakeScheduler) Add(k RouteKey, _ time.Time) {
	s.mu.Lock()
	s.keys[k] = struct{}{}
	// No need to wake here; a wave is only created via Trigger().
	s.mu.Unlock()
}

func (s *fakeScheduler) Del(k RouteKey) bool {
	s.mu.Lock()
	_, ok := s.keys[k]
	delete(s.keys, k)
	// If it was inflight this wave, treat it as completed for wave accounting.
	if s.inflight[k] {
		delete(s.inflight, k)
		if s.waveOut > 0 {
			s.waveOut--
			if s.waveOut == 0 && s.waveDone != nil {
				close(s.waveDone)
				s.waveDone = nil
			}
		}
	}
	s.mu.Unlock()
	return ok
}

func (s *fakeScheduler) Clear() {
	s.mu.Lock()
	s.keys = make(map[RouteKey]struct{})
	s.inflight = make(map[RouteKey]bool)
	// close any pending wave
	if s.waveDone != nil {
		close(s.waveDone)
		s.waveDone = nil
	}
	s.waveOut = 0
	s.wavePending = false
	// wake worker to re-check Peek() (now empty)
	s.signalLocked()
	s.mu.Unlock()
}

func (s *fakeScheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys)
}

// Trigger starts a new wave. Next Peek() will return "now" once.
func (s *fakeScheduler) Trigger() {
	s.mu.Lock()
	// If a wave is already pending, don't create a new waveDone; just wake.
	if s.wavePending {
		s.signalLocked()
		s.mu.Unlock()
		return
	}
	s.wavePending = true
	s.waveOut = 0
	// Close any previous waveDone and create a fresh one for this wave.
	if s.waveDone != nil {
		close(s.waveDone)
	}
	s.waveDone = make(chan struct{})
	// Wake any waiters (worker’s Wake() select).
	s.signalLocked()
	s.mu.Unlock()
}

func (s *fakeScheduler) waitDrained(t *testing.T, timeout time.Duration) {
	t.Helper()
	s.mu.Lock()
	ch := s.waveDone
	s.mu.Unlock()

	if ch == nil {
		// no wave in progress
		return
	}

	select {
	case <-ch:
		// drained normally
	case <-time.After(timeout):
		dumpGoroutines(t)
		dumpGoroutinesFiltered(t, "/internal/probing", "probingWorker", "fakeScheduler")
		t.Fatalf("scheduler did not drain in time (timeout=%v)", timeout)
	}
}

func (s *fakeScheduler) Peek() (time.Time, bool) {
	s.mu.Lock()
	p := s.wavePending
	s.mu.Unlock()
	if p {
		return time.Now(), true
	}
	return time.Time{}, false
}

func (s *fakeScheduler) PopDue(_ time.Time) []RouteKey {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.wavePending {
		return nil
	}
	// Consume the pending flag exactly once per wave.
	s.wavePending = false

	// Hand out every non-inflight key for this wave and mark it inflight.
	var out []RouteKey
	for k := range s.keys {
		if s.inflight[k] {
			continue
		}
		out = append(out, k)
		s.inflight[k] = true
	}
	s.waveOut = len(out)

	// If nothing to do this wave, signal drain immediately so waiters don’t hang.
	if s.waveOut == 0 && s.waveDone != nil {
		close(s.waveDone)
		s.waveDone = nil
	}
	return out
}

func (s *fakeScheduler) Complete(k RouteKey, _ ProbeOutcome) {
	s.mu.Lock()
	if s.inflight[k] {
		delete(s.inflight, k)
		if s.waveOut > 0 {
			s.waveOut--
			if s.waveOut == 0 && s.waveDone != nil {
				close(s.waveDone)
				s.waveDone = nil
			}
		}
	}
	s.mu.Unlock()
}

func waitEdge(t *testing.T, ch <-chan struct{}, d time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(d):
		dumpGoroutines(t)
		dumpGoroutinesFiltered(t, "/internal/probing", "probingWorker", "fakeScheduler")
		t.Fatal(msg)
	}
}

func dumpGoroutines(t *testing.T) {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 2)
	t.Logf("\n==== BEGIN GOROUTINE DUMP ====\n%s\n==== END GOROUTINE DUMP ====\n", b.String())
}

func dumpGoroutinesFiltered(t *testing.T, subs ...string) {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 2)
	lines := strings.Split(b.String(), "\n")
	var out bytes.Buffer
want:
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		for _, s := range subs {
			if strings.Contains(l, s) {
				// include the header line and following few lines (stack frames)
				for j := 0; j < 12 && i+j < len(lines); j++ {
					out.WriteString(lines[i+j])
					out.WriteByte('\n')
				}
				i += 11
				continue want
			}
		}
	}
	t.Logf("\n==== BEGIN FILTERED GOROUTINES ====\n%s==== END FILTERED GOROUTINES ====\n", out.String())
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

func (m *mockLivenessPolicy) String() string {
	return "mockLivenessPolicy"
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
