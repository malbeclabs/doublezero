package liveness

import (
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestClient_LivenessManager_ConfigValidate(t *testing.T) {
	t.Parallel()
	log := newTestLogger(t)

	err := (&ManagerConfig{Netlinker: &MockRouteReaderWriter{}, BindIP: "127.0.0.1"}).Validate()
	require.Error(t, err)

	err = (&ManagerConfig{Logger: log, BindIP: "127.0.0.1"}).Validate()
	require.Error(t, err)

	err = (&ManagerConfig{Logger: log, Netlinker: &MockRouteReaderWriter{}, BindIP: ""}).Validate()
	require.Error(t, err)

	err = (&ManagerConfig{Logger: log, Netlinker: &MockRouteReaderWriter{}, BindIP: "127.0.0.1", MinTxFloor: -1}).Validate()
	require.Error(t, err)
	err = (&ManagerConfig{Logger: log, Netlinker: &MockRouteReaderWriter{}, BindIP: "127.0.0.1", MaxTxCeil: -1}).Validate()
	require.Error(t, err)
	err = (&ManagerConfig{Logger: log, Netlinker: &MockRouteReaderWriter{}, BindIP: "127.0.0.1", BackoffMax: -1}).Validate()
	require.Error(t, err)

	err = (&ManagerConfig{
		Logger:     log,
		Netlinker:  &MockRouteReaderWriter{},
		BindIP:     "127.0.0.1",
		TxMin:      100 * time.Millisecond,
		RxMin:      100 * time.Millisecond,
		DetectMult: 3,
		MinTxFloor: 200 * time.Millisecond,
		MaxTxCeil:  100 * time.Millisecond,
		Port:       -1, // invalid port
	}).Validate()
	require.EqualError(t, err, "port must be greater than or equal to 0")

	cfg := &ManagerConfig{
		Logger:     log,
		Netlinker:  &MockRouteReaderWriter{},
		BindIP:     "127.0.0.1",
		TxMin:      100 * time.Millisecond,
		RxMin:      100 * time.Millisecond,
		DetectMult: 3,
		MinTxFloor: 50 * time.Millisecond,
		MaxTxCeil:  1 * time.Second,
	}
	err = cfg.Validate()
	require.NoError(t, err)
	require.NotZero(t, cfg.MinTxFloor)
	require.NotZero(t, cfg.MaxTxCeil)
	require.NotZero(t, cfg.BackoffMax)
	require.GreaterOrEqual(t, int64(cfg.MaxTxCeil), int64(cfg.MinTxFloor))
	require.GreaterOrEqual(t, int64(cfg.BackoffMax), int64(cfg.MinTxFloor))
}

func TestClient_LivenessManager_NewManager_BindsAndLocalAddr(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	la := m.LocalAddr()
	require.NotNil(t, la)
	require.Equal(t, "127.0.0.1", la.IP.String())
	require.NotZero(t, la.Port)
}

func TestClient_LivenessManager_RegisterRoute_Deduplicates(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})

	err = m.RegisterRoute(r, "lo")
	require.NoError(t, err)
	err = m.RegisterRoute(r, "lo")
	require.NoError(t, err)

	m.mu.Lock()
	require.Len(t, m.sessions, 1)
	require.Contains(t, m.sessions, Peer{Interface: "lo", LocalIP: r.Src.String(), RemoteIP: r.Dst.IP.String()})
	require.NotContains(t, m.sessions, Peer{Interface: "lo", LocalIP: r.Dst.IP.String(), RemoteIP: r.Src.String()})
	m.mu.Unlock()
}

func TestClient_LivenessManager_HandleRx_Transitions_AddAndDelete(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))

	var sess *Session
	var peer Peer
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for p, s := range m.sessions {
			peer = p
			sess = s
			break
		}
	}()
	require.NotNil(t, sess)

	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 1234, State: StateDown}, peer)
	func() {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		require.Equal(t, StateInit, sess.state)
		require.EqualValues(t, 1234, sess.yourDisc)
	}()

	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateInit}, peer)
	added := wait(t, addCh, 2*time.Second, "RouteAdd after Up")
	require.Equal(t, r.Table, added.Table)
	require.Equal(t, r.Src.String(), added.Src.String())
	require.Equal(t, r.Dst.String(), added.Dst.String())
	require.Equal(t, r.NextHop.String(), added.NextHop.String())

	m.mu.Lock()
	require.Len(t, m.sessions, 1)
	require.Contains(t, m.sessions, peer)
	require.NotContains(t, m.sessions, Peer{Interface: "lo", LocalIP: r.Dst.IP.String(), RemoteIP: r.Src.String()})
	require.Equal(t, StateUp, sess.state)
	m.mu.Unlock()

	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateDown}, peer)
	deleted := wait(t, delCh, 2*time.Second, "RouteDelete after Down")
	require.Equal(t, r.Table, deleted.Table)
	require.Equal(t, r.Src.String(), deleted.Src.String())
	require.Equal(t, r.Dst.String(), deleted.Dst.String())

	m.mu.Lock()
	require.Len(t, m.sessions, 1)
	require.Contains(t, m.sessions, peer)
	require.NotContains(t, m.sessions, Peer{Interface: "lo", LocalIP: r.Dst.IP.String(), RemoteIP: r.Src.String()})
	require.Equal(t, StateDown, sess.state)
	m.mu.Unlock()
}

func TestClient_LivenessManager_WithdrawRoute_RemovesSessionAndDeletesIfInstalled(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = nlr
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))

	var peer Peer
	var sess *Session
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for p, s := range m.sessions {
			peer, sess = p, s
			break
		}
	}()
	// Down -> Init (learn yourDisc)
	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 1, State: StateInit}, peer)
	// Init -> Up requires explicit echo (YourDiscr == myDisc)
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateInit}, peer)
	wait(t, addCh, 2*time.Second, "RouteAdd before withdraw")

	require.NoError(t, m.WithdrawRoute(r, "lo"))
	wait(t, delCh, 2*time.Second, "RouteDelete on withdraw")

	m.mu.Lock()
	_, still := m.sessions[peer]
	m.mu.Unlock()
	require.False(t, still, "session should be removed after withdraw")

	sess.mu.Lock()
	require.False(t, sess.alive)
	sess.mu.Unlock()
}

func TestClient_LivenessManager_AdminDownAll(t *testing.T) {
	t.Parallel()
	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = nlr
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))

	// Drive session to Up so a route is installed
	var peer Peer
	var sess *Session
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for p, s := range m.sessions {
			peer, sess = p, s
			break
		}
	}()
	require.NotNil(t, sess)
	// Down->Init
	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 1234, State: StateDown}, peer)
	// Init->Up (RouteAdd enqueued)
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateUp}, peer)
	_ = wait(t, addCh, 2*time.Second, "RouteAdd after Up")

	// Enter AdminDownAll -> should withdraw route once
	m.AdminDownAll()
	_ = wait(t, delCh, 2*time.Second, "RouteDelete on AdminDownAll")

	m.mu.Lock()
	for _, s := range m.sessions {
		s.mu.Lock()
		require.Equal(t, StateAdminDown, s.state)
		s.mu.Unlock()
	}
	m.mu.Unlock()
}

func TestClient_LivenessManager_Close_Idempotent(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{}
	})
	require.NoError(t, err)
	require.NoError(t, m.Close())
	require.NoError(t, m.Close())
}

func TestClient_LivenessManager_HandleRx_UnknownPeer_NoEffect(t *testing.T) {
	t.Parallel()

	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return nil },
		RouteDeleteFunc:     func(*routing.Route) error { return nil },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = nlr
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	// Register a real session to ensure maps are non-empty.
	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))

	m.mu.Lock()
	prevSessions := len(m.sessions)
	prevInstalled := len(m.installed)
	m.mu.Unlock()

	// Construct a peer key that doesn't exist.
	unknown := Peer{Interface: "lo", LocalIP: "127.0.0.2", RemoteIP: "127.0.0.3"}
	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 1, State: StateInit}, unknown)

	// Assert no changes.
	m.mu.Lock()
	defer m.mu.Unlock()
	require.Equal(t, prevSessions, len(m.sessions))
	require.Equal(t, prevInstalled, len(m.installed))
}

func TestClient_LivenessManager_NetlinkerErrors_NoCrash(t *testing.T) {
	t.Parallel()

	addErr := errors.New("add boom")
	delErr := errors.New("del boom")
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return addErr },
		RouteDeleteFunc:     func(*routing.Route) error { return delErr },
		RouteGetFunc:        func(net.IP) ([]*routing.Route, error) { return nil, nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = nlr
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))

	// Grab session+peer key to inspect installed flags.
	var peer Peer
	var sess *Session
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for p, s := range m.sessions {
			peer, sess = p, s
			break
		}
	}()
	require.NotNil(t, sess)

	// Drive to Up (RouteAdd returns error but should not crash; installed set true).
	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 99, State: StateDown}, peer)                    // Down -> Init
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateUp}, peer) // Init -> Up

	rk := routeKeyFor(peer.Interface, sess.route)
	time.Sleep(50 * time.Millisecond) // allow onSessionUp goroutine to run

	m.mu.Lock()
	require.True(t, m.installed[rk], "installed should be true after Up even if RouteAdd errored")
	m.mu.Unlock()

	// Drive to Down (RouteDelete returns error; should not crash; installed set false).
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateDown}, peer)
	time.Sleep(50 * time.Millisecond)

	m.mu.Lock()
	require.False(t, m.installed[rk], "installed should be false after Down even if RouteDelete errored")
	m.mu.Unlock()
}

func TestClient_LivenessManager_PassiveMode_ImmediateInstall_NoAutoWithdraw(t *testing.T) {
	t.Parallel()
	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:    func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc: func(r *routing.Route) error { delCh <- r; return nil },
		}
	})
	require.NoError(t, err)
	defer m.Close()

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))
	_ = wait(t, addCh, time.Second, "immediate RouteAdd in PassiveMode")

	// drive Up then Down; expect no RouteDelete (caller owns dataplane)
	var peer Peer
	var sess *Session
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for p, s := range m.sessions {
			peer, sess = p, s
			break
		}
	}()
	m.HandleRx(&ControlPacket{YourDiscr: 0, MyDiscr: 1, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateUp}, peer)
	m.HandleRx(&ControlPacket{YourDiscr: sess.myDisc, MyDiscr: sess.yourDisc, State: StateDown}, peer)

	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete in PassiveMode")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestClient_LivenessManager_LocalAddrNilAfterClose(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	require.NoError(t, m.Close())
	require.Nil(t, m.LocalAddr())
}

func TestClient_LivenessManager_PeerKey_IPv4Canonicalization(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	defer m.Close()

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo"))
	m.mu.Lock()
	_, ok := m.sessions[Peer{Interface: "lo", LocalIP: r.Src.To4().String(), RemoteIP: r.Dst.IP.To4().String()}]
	m.mu.Unlock()
	require.True(t, ok, "peer key should use IPv4 string forms")
}

func TestClient_Liveness_Manager_ReceiverFailure_PropagatesOnErr(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	errCh := m.Err()

	// Close the UDP socket directly to force Receiver.Run to error out.
	var udp *UDPService
	m.mu.Lock()
	udp = m.udp
	m.mu.Unlock()
	require.NotNil(t, udp)
	_ = udp.Close()

	// Expect an error to surface on Err().
	select {
	case e := <-errCh:
		require.Error(t, e)
	default:
		select {
		case e := <-errCh:
			require.Error(t, e)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for error from manager.Err after UDP close")
		}
	}

	// Close should complete cleanly after the receiver failure.
	require.NoError(t, m.Close())
}

func TestClient_Liveness_Manager_Close_NoErrOnErrCh(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)

	// No spurious errors before close.
	func() {
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return
		case <-m.Err():
			t.Fatalf("unexpected error before Close")
		}
	}()

	require.NoError(t, m.Close())

	// No spurious errors after close either.
	func() {
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return
		case <-m.Err():
			t.Fatalf("unexpected error after Close")
		}
	}()
}

func newTestManager(t *testing.T, mutate func(*ManagerConfig)) (*Manager, error) {
	cfg := &ManagerConfig{
		Logger:     newTestLogger(t),
		Netlinker:  &MockRouteReaderWriter{},
		BindIP:     "127.0.0.1",
		Port:       0,
		TxMin:      100 * time.Millisecond,
		RxMin:      100 * time.Millisecond,
		DetectMult: 3,
		MinTxFloor: 50 * time.Millisecond,
		MaxTxCeil:  1 * time.Second,
		BackoffMax: 1 * time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}
	return NewManager(t.Context(), cfg)
}

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
	if m.RouteAddFunc == nil {
		return nil
	}
	return m.RouteAddFunc(r)
}

func (m *MockRouteReaderWriter) RouteDelete(r *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteDeleteFunc == nil {
		return nil
	}
	return m.RouteDeleteFunc(r)
}

func (m *MockRouteReaderWriter) RouteGet(ip net.IP) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteGetFunc == nil {
		return nil, nil
	}
	return m.RouteGetFunc(ip)
}

func (m *MockRouteReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RouteByProtocolFunc == nil {
		return nil, nil
	}
	return m.RouteByProtocolFunc(protocol)
}
