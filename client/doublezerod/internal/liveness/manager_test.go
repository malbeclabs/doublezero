package liveness

import (
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// test tunables: keep them modest so state converges quickly but not flaky
const (
	testTxMin  = 100 * time.Millisecond
	testRxMin  = 100 * time.Millisecond
	testDetect = 3
)

type testWriter struct {
	t  *testing.T
	mu sync.Mutex
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Logf("%s", p)
	return len(p), nil
}

func newTestLogger(t *testing.T) *slog.Logger {
	w := &testWriter{t: t}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h)
}

// small helper: wait for a signal or fail
func wait[T any](t *testing.T, ch <-chan T, d time.Duration, name string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timeout waiting for %s", name)
		var z T
		return z
	}
}

func TestManager_E2E_TwoManagers_Up(t *testing.T) {
	log := newTestLogger(t)

	// Observe real effects on the RouteReaderWriter
	addCh := make(chan *routing.Route, 4)
	delCh := make(chan *routing.Route, 4)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m1, err := NewManager(t.Context(), log, nlr, "127.0.0.1", 0)
	require.NoError(t, err, "NewManager m1")
	t.Cleanup(func() { _ = m1.Close() })

	m2, err := NewManager(t.Context(), log, nlr, "127.0.0.1", 0)
	require.NoError(t, err, "NewManager m2")
	t.Cleanup(func() { _ = m2.Close() })

	const txMin = 100 * time.Millisecond
	const rxMin = 100 * time.Millisecond
	const det = 3

	// Distinct prefixes so hashes don’t collide; nextHop can be anything for this test.
	r1 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m2.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m1.LocalAddr().IP
	})
	require.NoError(t, m1.RegisterRoute(r1, m2.LocalAddr(), "lo", txMin, rxMin, det))

	r2 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m1.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m2.LocalAddr().IP
	})
	require.NoError(t, m2.RegisterRoute(r2, m1.LocalAddr(), "lo", txMin, rxMin, det))

	// Expect both sides to install their route (RouteAdd called twice total).
	gotA := wait(t, addCh, 2*time.Second, "first route add")
	gotB := wait(t, addCh, 2*time.Second, "second route add")

	// Order is nondeterministic; validate set membership
	adds := []*routing.Route{gotA, gotB}
	assertHas := func(want *routing.Route) {
		for _, r := range adds {
			if r != nil &&
				r.Src.String() == want.Src.String() &&
				r.Dst.String() == want.Dst.String() &&
				r.NextHop.String() == want.NextHop.String() &&
				r.Table == want.Table {
				return
			}
		}
		t.Fatalf("expected RouteAdd for %s not observed", want.String())
	}
	assertHas(r1)
	assertHas(r2)

	// Let a few TX intervals pass and ensure no RouteDelete is observed.
	time.Sleep(3 * txMin)
	select {
	case r := <-delCh:
		t.Fatalf("unexpected RouteDelete: %s", r.String())
	default:
	}
}

func TestManagers_E2E_UpAndExpire(t *testing.T) {
	log1 := newTestLogger(t)
	log2 := newTestLogger(t)

	addCh := make(chan *routing.Route, 4)
	delCh := make(chan *routing.Route, 4)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	ctx := t.Context()
	m1, err := NewManager(ctx, log1, nlr, "127.0.0.1", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m1.Close() })

	m2, err := NewManager(ctx, log2, nlr, "127.0.0.1", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m2.Close() })

	// Register symmetrical sessions
	r1 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m2.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m1.LocalAddr().IP
	})
	require.NoError(t, m1.RegisterRoute(r1, m2.LocalAddr(), "lo", testTxMin, testRxMin, testDetect))

	r2 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m1.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m2.LocalAddr().IP
	})
	require.NoError(t, m2.RegisterRoute(r2, m1.LocalAddr(), "lo", testTxMin, testRxMin, testDetect))

	// Both should go Up → two RouteAdd calls.
	gotA := wait(t, addCh, 3*time.Second, "first route add")
	gotB := wait(t, addCh, 3*time.Second, "second route add")
	// Sanity: make sure both r1 and r2 appeared in any order.
	adds := []*routing.Route{gotA, gotB}
	has := func(want *routing.Route) bool {
		for _, r := range adds {
			if r != nil &&
				r.Src.String() == want.Src.String() &&
				r.Dst.String() == want.Dst.String() &&
				r.NextHop.String() == want.NextHop.String() &&
				r.Table == want.Table {
				return true
			}
		}
		return false
	}
	require.True(t, has(r1), "RouteAdd for r1 not observed")
	require.True(t, has(r2), "RouteAdd for r2 not observed")

	// Now force expiry by silencing m2 entirely
	_ = m2.Close()

	// m1 should detect loss and remove its installed route → expect one RouteDelete (for r1).
	deleted := wait(t, delCh, 2*time.Second, "route delete after peer silence")
	require.Equal(t, r1.Src.String(), deleted.Src.String())
	require.Equal(t, r1.Dst.String(), deleted.Dst.String())
	require.Equal(t, r1.NextHop.String(), deleted.NextHop.String())
	require.Equal(t, r1.Table, deleted.Table)

	// And no immediate additional deletes.
	select {
	case extra := <-delCh:
		t.Fatalf("unexpected extra RouteDelete: %s", extra.String())
	default:
	}
}

func TestManager_WithdrawRoute_RemovesSession(t *testing.T) {
	log := newTestLogger(t)

	// We don’t expect kernel adds/deletes in this test, but mock must be non-nil.
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return nil },
		RouteDeleteFunc:     func(*routing.Route) error { return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := NewManager(t.Context(), log, nlr, "127.0.0.1", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	// Peer doesn't matter here; we won't exchange traffic.
	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, m.LocalAddr(), "lo", testTxMin, testRxMin, testDetect))
	m.WithdrawRoute(r, "lo")
	time.Sleep(50 * time.Millisecond)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) != 0 {
		t.Fatalf("expected no sessions, have %d", len(m.sessions))
	}
}

func TestManager_AdminDownAll_SetsState(t *testing.T) {
	log := newTestLogger(t)

	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return nil },
		RouteDeleteFunc:     func(*routing.Route) error { return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := NewManager(t.Context(), log, nlr, "127.0.0.1", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, m.LocalAddr(), "lo", testTxMin, testRxMin, testDetect))

	m.AdminDownAll()

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		if s.state != AdminDown {
			st := s.state
			s.mu.Unlock()
			t.Fatalf("session not AdminDown, got %v", st)
		}
		s.mu.Unlock()
	}
}

// --- helpers unchanged ---

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

type MockRouteReaderWriter struct {
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteGetFunc        func(net.IP) ([]*routing.Route, error)
	RouteByProtocolFunc func(int) ([]*routing.Route, error)

	mu sync.Mutex
}

func (m *MockRouteReaderWriter) RouteAdd(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteAddFunc(r)
}
func (m *MockRouteReaderWriter) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteDeleteFunc(r)
}
func (m *MockRouteReaderWriter) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteGetFunc(ip)
}
func (m *MockRouteReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RouteByProtocolFunc(protocol)
}
