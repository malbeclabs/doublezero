package liveness

import (
	"log/slog"
	"net"
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

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Logf("%s", p); return len(p), nil }
func newTestLogger(t *testing.T) *slog.Logger {
	h := slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelInfo})
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

	m1, err := NewManager(t.Context(), log, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("NewManager m1: %v", err)
	}
	t.Cleanup(func() { _ = m1.Close() })

	m2, err := NewManager(t.Context(), log, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("NewManager m2: %v", err)
	}
	t.Cleanup(func() { _ = m2.Close() })

	up1, up2 := make(chan *Session, 1), make(chan *Session, 1)
	down1, down2 := make(chan *Session, 1), make(chan *Session, 1)
	m1.onUp, m2.onUp = func(s *Session) { up1 <- s }, func(s *Session) { up2 <- s }
	m1.onDown, m2.onDown = func(s *Session) { down1 <- s }, func(s *Session) { down2 <- s }

	const txMin = 100 * time.Millisecond
	const rxMin = 100 * time.Millisecond
	const det = 3

	// Distinct prefixes so hashes don’t collide; nextHop can be anything for this test.
	r1 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m2.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m1.LocalAddr().IP
	})
	if _, err := m1.RegisterRoute(r1, m2.LocalAddr(), "lo", txMin, rxMin, det); err != nil {
		t.Fatalf("m1 RegisterRoute: %v", err)
	}
	r2 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m1.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m2.LocalAddr().IP
	})
	if _, err := m2.RegisterRoute(r2, m1.LocalAddr(), "lo", txMin, rxMin, det); err != nil {
		t.Fatalf("m2 RegisterRoute: %v", err)
	}

	// Both sides should reach Up.
	select {
	case <-up1:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for m1 up")
	}
	select {
	case <-up2:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for m2 up")
	}

	// Give a few TX intervals to cycle and ensure neither side drops.
	time.Sleep(3 * txMin)

	select {
	case k := <-down1:
		t.Fatalf("m1 unexpectedly went down: %+v", k)
	default:
	}
	select {
	case k := <-down2:
		t.Fatalf("m2 unexpectedly went down: %+v", k)
	default:
	}
}

func TestManagers_E2E_UpAndExpire(t *testing.T) {
	log1 := newTestLogger(t)
	log2 := newTestLogger(t)

	ctx := t.Context()
	m1, err := NewManager(ctx, log1, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("m1: %v", err)
	}
	t.Cleanup(func() { _ = m1.Close() })

	m2, err := NewManager(ctx, log2, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("m2: %v", err)
	}
	t.Cleanup(func() { _ = m2.Close() })

	// Wire the peers: each manager sends to the other’s bound UDP address.
	peer1 := m2.LocalAddr().String() // m1 will send to m2
	peer2 := m1.LocalAddr().String() // m2 will send to m1
	if _, _, err := net.SplitHostPort(peer1); err != nil {
		t.Fatalf("peer1 addr: %v", err)
	}
	if _, _, err := net.SplitHostPort(peer2); err != nil {
		t.Fatalf("peer2 addr: %v", err)
	}

	up1 := make(chan *Session, 1)
	down1 := make(chan *Session, 1)
	m1.onUp = func(s *Session) { up1 <- s }
	m1.onDown = func(s *Session) { down1 <- s }

	up2 := make(chan *Session, 1)
	down2 := make(chan *Session, 1)
	m2.onUp = func(s *Session) { up2 <- s }
	m2.onDown = func(s *Session) { down2 <- s }

	// Register symmetrical sessions
	r1 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m2.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m1.LocalAddr().IP
	})
	if _, err := m1.RegisterRoute(r1, m2.LocalAddr(), "lo", testTxMin, testRxMin, testDetect); err != nil {
		t.Fatalf("m1.RegisterRoute: %v", err)
	}
	r2 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m1.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m2.LocalAddr().IP
	})
	if _, err := m2.RegisterRoute(r2, m1.LocalAddr(), "lo", testTxMin, testRxMin, testDetect); err != nil {
		t.Fatalf("m2.RegisterRoute: %v", err)
	}

	// Both should go Up
	gotUp1 := wait(t, up1, 3*time.Second, "m1 up")
	gotUp2 := wait(t, up2, 3*time.Second, "m2 up")

	require.Equal(t, "127.0.0.1", gotUp1.route.Src.String())
	require.Equal(t, r1.Dst.String(), gotUp1.route.Dst.String())
	require.Equal(t, r1.NextHop.String(), gotUp1.route.NextHop.String())

	require.Equal(t, "127.0.0.1", gotUp2.route.Src.String())
	require.Equal(t, r2.Dst.String(), gotUp2.route.Dst.String())
	require.Equal(t, r2.NextHop.String(), gotUp2.route.NextHop.String())

	// Now force expiry by silencing m2 entirely
	_ = m2.Close()

	// m1 should detect loss after detectMult * rxRef. Give generous headroom.
	_ = wait(t, down1, 2*time.Second, "m1 down")
}

func TestManager_WithdrawRoute_RemovesSession(t *testing.T) {
	log := newTestLogger(t)
	m, err := NewManager(t.Context(), log, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	// Peer doesn't matter here; we won't exchange traffic.
	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	if _, err := m.RegisterRoute(r, m.LocalAddr(), "lo", testTxMin, testRxMin, testDetect); err != nil {
		t.Fatalf("RegisterRoute: %v", err)
	}
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
	m, err := NewManager(t.Context(), log, "lo", "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	if _, err := m.RegisterRoute(r, m.LocalAddr(), "lo", testTxMin, testRxMin, testDetect); err != nil {
		t.Fatalf("RegisterRoute: %v", err)
	}

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
