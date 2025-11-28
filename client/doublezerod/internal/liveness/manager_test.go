package liveness

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/prometheus/client_golang/prometheus"
	prom "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_Manager_ConfigValidate(t *testing.T) {
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

func TestClient_Liveness_Manager_NewManager_BindsAndLocalAddr(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	la := m.LocalAddr()
	require.NotNil(t, la)
	require.Equal(t, "127.0.0.1", la.IP.String())
	require.NotZero(t, la.Port)
}

func TestClient_Liveness_Manager_RegisterRoute_Deduplicates(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})

	err = m.RegisterRoute(r, "lo", m.LocalAddr().Port)
	require.NoError(t, err)
	err = m.RegisterRoute(r, "lo", m.LocalAddr().Port)
	require.NoError(t, err)

	require.Equal(t, 1, m.GetSessionsLen())
	require.True(t, m.HasSession(Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}))
	require.False(t, m.HasSession(Peer{Interface: "lo", LocalIP: r.Dst.IP.String(), PeerIP: r.Src.String()}))
}

func TestClient_Liveness_Manager_HandleRx_Transitions_AddAndDelete(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1234, State: StateInit}, peer)
	require.Equal(t, StateInit, sess.GetState())
	require.EqualValues(t, 1234, sess.peerDiscr)

	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateInit}, peer)
	added := wait(t, addCh, 2*time.Second, "RouteAdd after Up")
	require.Equal(t, r.Table, added.Table)
	require.Equal(t, r.Src.String(), added.Src.String())
	require.Equal(t, r.Dst.String(), added.Dst.String())
	require.Equal(t, r.NextHop.String(), added.NextHop.String())

	sess, ok = m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)
	require.Equal(t, 1, m.GetSessionsLen())
	require.Equal(t, StateUp, sess.GetState())

	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateAdminDown}, peer)
	deleted := wait(t, delCh, 2*time.Second, "RouteDelete after Down")
	require.Equal(t, r.Table, deleted.Table)
	require.Equal(t, r.Src.String(), deleted.Src.String())
	require.Equal(t, r.Dst.String(), deleted.Dst.String())

	sess, ok = m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)
	require.Equal(t, 1, m.GetSessionsLen())
	require.Equal(t, StateDown, sess.GetState())
}

func TestClient_Liveness_Manager_WithdrawRoute_RemovesSessionAndDeletesIfInstalled(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
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
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Down -> Init (learn peerDiscr)
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, peer)
	// Init -> Up requires explicit echo (PeerDiscr == localDiscr)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateInit}, peer)
	wait(t, addCh, 2*time.Second, "RouteAdd before withdraw")

	require.NoError(t, m.WithdrawRoute(r, "lo"))
	wait(t, delCh, 2*time.Second, "RouteDelete on withdraw")

	require.Equal(t, 0, m.GetSessionsLen())
	require.False(t, sess.alive)
}

func TestClient_Liveness_Manager_Close_Idempotent(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{}
	})
	require.NoError(t, err)
	require.NoError(t, m.Close())
	require.NoError(t, m.Close())
}

func TestClient_Liveness_Manager_HandleRx_UnknownPeer_NoEffect(t *testing.T) {
	t.Parallel()

	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return nil },
		RouteDeleteFunc:     func(*routing.Route) error { return nil },
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
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	prevSessions := m.GetSessionsLen()
	prevInstalled := m.GetInstalledLen()

	// Construct a peer key that doesn't exist.
	unknown := Peer{Interface: "lo", LocalIP: "127.0.0.2", PeerIP: "127.0.0.3"}
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, unknown)

	// Assert no changes.
	require.Equal(t, prevSessions, m.GetSessionsLen())
	require.Equal(t, prevInstalled, m.GetInstalledLen())
}

func TestClient_Liveness_Manager_NetlinkerErrors_NoCrash(t *testing.T) {
	t.Parallel()

	addErr := errors.New("add boom")
	delErr := errors.New("del boom")
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(*routing.Route) error { return addErr },
		RouteDeleteFunc:     func(*routing.Route) error { return delErr },
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
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	// Grab session+peer key to inspect installed flags.
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Drive to Up (RouteAdd returns error but should not crash; installed set true).
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 99, State: StateInit}, peer)                         // Down -> Init
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}, peer) // Init -> Up

	rk := routeKeyFor(peer.Interface, sess.route)
	time.Sleep(50 * time.Millisecond) // allow onSessionUp goroutine to run

	require.True(t, m.IsInstalled(rk), "installed should be true after Up even if RouteAdd errored")

	// Drive to Down via remote AdminDown (RouteDelete returns error; should not crash; installed set false).
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateAdminDown}, peer)
	time.Sleep(50 * time.Millisecond)

	require.False(t, m.IsInstalled(rk), "installed should be false after Down even if RouteDelete errored")
}

func TestClient_Liveness_Manager_PassiveMode_ImmediateInstall_NoAutoWithdraw(t *testing.T) {
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
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))
	_ = wait(t, addCh, time.Second, "immediate RouteAdd in PassiveMode")

	// drive Up then Down; expect no RouteDelete (caller owns dataplane)
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateAdminDown}, peer)

	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete in PassiveMode")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestClient_Liveness_Manager_LocalAddrNilAfterClose(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	require.NoError(t, m.Close())
	require.Nil(t, m.LocalAddr())
}

func TestClient_Liveness_Manager_PeerKey_IPv4Canonicalization(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	defer m.Close()

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))
	peer := Peer{Interface: "lo", LocalIP: r.Src.To4().String(), PeerIP: r.Dst.IP.To4().String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)
	require.True(t, ok, "peer key should use IPv4 string forms")
}

func TestClient_Liveness_Manager_ReceiverFailure_PropagatesOnErr(t *testing.T) {
	t.Parallel()
	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	// Close the UDP socket directly to force Receiver.Run to error out.
	var udp UDPService
	m.mu.Lock()
	udp = m.udp
	m.mu.Unlock()
	require.NotNil(t, udp)
	_ = udp.Close()

	// Expect an error to surface on Err().
	select {
	case e := <-m.Err():
		require.Error(t, e)
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for error from manager.Err after UDP close")
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

func TestClient_Liveness_Manager_AdminDownRoute_WithdrawsAndMarksAdminDown(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)
	nlr := &MockRouteReaderWriter{
		RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
		RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
		RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
	}

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = nlr
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 42, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}, peer)
	added := wait(t, addCh, 2*time.Second, "RouteAdd before AdminDownRoute")
	require.Equal(t, r.Table, added.Table)
	require.Equal(t, r.Src.String(), added.Src.String())
	require.Equal(t, r.Dst.String(), added.Dst.String())

	rk := routeKeyFor(peer.Interface, sess.route)
	time.Sleep(50 * time.Millisecond)
	require.True(t, m.IsInstalled(rk), "route should be marked installed after Up")

	m.AdminDownRoute(r, "lo")

	deleted := wait(t, delCh, 2*time.Second, "RouteDelete on AdminDownRoute")
	require.Equal(t, r.Table, deleted.Table)
	require.Equal(t, r.Src.String(), deleted.Src.String())
	require.Equal(t, r.Dst.String(), deleted.Dst.String())

	require.False(t, m.IsInstalled(rk), "route should be marked not installed after AdminDownRoute")

	snap := sess.Snapshot()

	require.Equal(t, StateAdminDown, snap.State)
	require.Equal(t, DownReasonLocalAdmin, snap.LastDownReason)
	require.False(t, snap.DownSince.IsZero(), "downSince should be set")
	require.True(t, snap.UpSince.IsZero(), "upSince should be cleared")
	require.True(t, snap.DetectDeadline.IsZero(), "detectDeadline should be cleared")
	require.True(t, snap.NextDetectScheduled.IsZero(), "nextDetectScheduled should be cleared")
}

func TestClient_Liveness_Manager_AdminDownRoute_PassiveMode_NoDelete_Idempotent(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	_ = wait(t, addCh, time.Second, "immediate RouteAdd in PassiveMode")

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 7, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}, peer)

	m.AdminDownRoute(r, "lo")

	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete in PassiveMode via AdminDownRoute")
	case <-time.After(200 * time.Millisecond):
	}

	snap := sess.Snapshot()
	require.Equal(t, StateAdminDown, snap.State)
	require.Equal(t, DownReasonLocalAdmin, snap.LastDownReason)

	// Idempotent second call.
	m.AdminDownRoute(r, "lo")
	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete on second AdminDownRoute in PassiveMode")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestClient_Liveness_Manager_WithdrawRoute_PassiveMode_DeletesAndRemovesSession(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: m.LocalAddr().IP, Mask: net.CIDRMask(32, 32)}
		r.Src = m.LocalAddr().IP
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	_ = wait(t, addCh, time.Second, "immediate RouteAdd in PassiveMode")

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}, peer)

	require.NoError(t, m.WithdrawRoute(r, "lo"))
	_ = wait(t, delCh, 2*time.Second, "RouteDelete in PassiveMode WithdrawRoute")

	select {
	case <-delCh:
		t.Fatalf("unexpected second RouteDelete in PassiveMode WithdrawRoute")
	case <-time.After(200 * time.Millisecond):
	}

	require.Equal(t, 0, m.GetInstalledLen(), "installed should be empty after withdraw in PassiveMode")
	require.Equal(t, 0, m.GetSessionsLen(), "session should be removed after withdraw in PassiveMode")
	require.False(t, m.HasSession(peer), "session should be removed after withdraw in PassiveMode")
	require.False(t, sess.alive, "session should be marked not alive after withdraw in PassiveMode")
}

func TestClient_Liveness_Manager_AdminDownRoute_NoSession_NoDelete(t *testing.T) {
	t.Parallel()

	delCh := make(chan *routing.Route, 1)
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteAddFunc:        func(*routing.Route) error { return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})

	m.AdminDownRoute(r, "lo")

	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete when no session exists")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestClient_Liveness_Manager_RegisterRoute_InvalidIPv4Validation(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	rNilSrc := newTestRoute(func(r *routing.Route) {
		r.Src = nil
	})
	err = m.RegisterRoute(rNilSrc, "lo", m.LocalAddr().Port)
	require.Error(t, err)
	require.ErrorContains(t, err, "nil source (<nil>) or destination IP (10.4.0.11)")

	rNonIPv4 := newTestRoute(func(r *routing.Route) {
		r.Src = net.ParseIP("::1")
	})
	err = m.RegisterRoute(rNonIPv4, "lo", m.LocalAddr().Port)
	require.Error(t, err)
	require.ErrorContains(t, err, "non-IPv4 source")
}

func TestClient_Liveness_Manager_WithdrawRoute_InvalidIPv4Validation(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	rNilDst := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: nil, Mask: net.CIDRMask(32, 32)}
	})
	err = m.WithdrawRoute(rNilDst, "lo")
	require.Error(t, err)
	require.ErrorContains(t, err, "nil source or destination IP")

	rNonIPv4 := newTestRoute(func(r *routing.Route) {
		r.Dst = &net.IPNet{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)}
	})
	err = m.WithdrawRoute(rNonIPv4, "lo")
	require.Error(t, err)
	require.ErrorContains(t, err, "non-IPv4 source")
}

func TestClient_Liveness_Manager_HandleRx_RemoteDownHonoredOnlyAfterDetectInterval(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	// Grab the session + peer.
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Drive Down -> Init -> Up so the route is installed.
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, peer)
	m.HandleRx(&ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateInit}, peer)
	added := wait(t, addCh, 2*time.Second, "RouteAdd after Up")
	require.Equal(t, r.Dst.String(), added.Dst.String())

	rk := routeKeyFor(peer.Interface, sess.route)
	time.Sleep(50 * time.Millisecond)

	require.True(t, m.IsInstalled(rk), "route should be marked installed after Up")

	// 1) Remote Down while UpFor < detect interval → should be ignored (no delete).
	sess.mu.Lock()
	sess.upSince = time.Now() // "just went Up"
	sess.mu.Unlock()

	m.HandleRx(&ControlPacket{
		PeerDiscr:  sess.localDiscr,
		LocalDiscr: sess.peerDiscr,
		State:      StateDown,
	}, peer)

	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete for early remote Down (UpFor < detect interval)")
	case <-time.After(200 * time.Millisecond):
	}

	require.True(t, m.IsInstalled(rk), "route should remain installed after early remote Down")

	// 2) Remote Down after UpFor >= detect interval → should withdraw route.
	var detect time.Duration
	sess.mu.Lock()
	detect = sess.detectTime()
	sess.upSince = time.Now().Add(-2 * detect)
	sess.mu.Unlock()

	m.HandleRx(&ControlPacket{
		PeerDiscr:  sess.localDiscr,
		LocalDiscr: sess.peerDiscr,
		State:      StateDown,
	}, peer)

	deleted := wait(t, delCh, 2*time.Second, "RouteDelete after remote Down")
	require.Equal(t, r.Dst.String(), deleted.Dst.String())

	time.Sleep(50 * time.Millisecond)
	require.False(t, m.IsInstalled(rk), "route should be marked not installed after remote Down")
}

func TestClient_Liveness_Manager_HonorPeerAdvertisedPassive_LeavesRouteInstalledOnDown(t *testing.T) {
	t.Parallel()

	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.HonorPeerAdvertisedPassive = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	// Build a route and peer like the rest of the tests.
	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}

	// Synthetic session which is "effectively passive" due to peer flags:
	sess := &Session{
		peer:               &peer,
		route:              r,
		state:              StateDown,
		peerAdvertisedMode: PeerModePassive,
		downSince:          time.Now(),
		lastDownReason:     DownReasonRemoteAdmin,
		alive:              true,
	}

	// Seed manager bookkeeping so onSessionDown thinks the route is desired+installed.
	rk := routeKeyFor(peer.Interface, r)
	m.mu.Lock()
	m.desired[rk] = r
	m.installed[rk] = true
	m.mu.Unlock()

	// Sanity: effectively passive should be true for this snapshot.
	snap := sess.Snapshot()
	require.True(t, m.isPeerEffectivelyPassive(snap), "session should be effectively passive before onSessionDown")

	// Call onSessionDown directly: with HonorPeerAdvertisedPassive and peerAdvertisedPassive,
	// we expect no RouteDelete and the route to remain logically installed.
	m.onSessionDown(sess)

	// No RouteDelete should be called.
	select {
	case <-delCh:
		t.Fatalf("unexpected RouteDelete for peer advertising passive when HonorPeerAdvertisedPassive is enabled")
	case <-time.After(200 * time.Millisecond):
	}

	// installed bit should remain true because we are effectively passive and not in PassiveMode.
	require.True(t, m.IsInstalled(rk), "route should remain installed when peer is effectively passive")
}

func TestClient_Liveness_Manager_HonorPeerAdvertisedPassive_TurnOffPassiveThenDeletesOnDown(t *testing.T) {
	t.Parallel()

	delCh := make(chan *routing.Route, 1)

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = false
		cfg.HonorPeerAdvertisedPassive = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { delCh <- r; return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Down -> Init with passive advertised.
	cpInit := &ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}
	cpInit.SetPassive()
	m.HandleRx(cpInit, peer)

	// Init -> Up with passive still advertised.
	cpUp := &ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}
	cpUp.SetPassive()
	m.HandleRx(cpUp, peer)

	snap := sess.Snapshot()
	require.Equal(t, PeerModePassive, snap.PeerAdvertisedMode, "peer should start as advertising passive")

	// RouteKey for this session.
	rk := routeKeyFor(peer.Interface, sess.route)

	// Wait for async onSessionUp goroutine to run and mark installed.
	require.Eventually(t, func() bool {
		return m.IsInstalled(rk)
	}, 2*time.Second, 50*time.Millisecond,
		"route should be installed after Up even when peer is advertising passive")

	// Peer stops advertising passive while staying Up.
	m.HandleRx(&ControlPacket{
		PeerDiscr:       sess.localDiscr,
		LocalDiscr:      sess.peerDiscr,
		State:           StateUp,
		DesiredMinTxUs:  20_000,
		RequiredMinRxUs: 20_000,
	}, peer)

	snap = sess.Snapshot()
	require.Equal(t, PeerModeActive, snap.PeerAdvertisedMode, "PeerAdvertisedMode should reflect the latest packet (passive off)")
	require.False(t, m.isPeerEffectivelyPassive(snap), "session should no longer be effectively passive after passive is cleared")

	// Now remote AdminDown; since passive is no longer advertised, we should
	// uninstall the route as normal.
	m.HandleRx(&ControlPacket{
		PeerDiscr:  sess.localDiscr,
		LocalDiscr: sess.peerDiscr,
		State:      StateAdminDown,
	}, peer)

	deleted := wait(t, delCh, 2*time.Second, "RouteDelete after peer stops advertising passive and goes AdminDown")
	require.Equal(t, r.Dst.String(), deleted.Dst.String())
}

func TestClient_Liveness_Manager_OnSessionUp_InstallsEvenWhenPeerPassive(t *testing.T) {
	t.Parallel()

	addCh := make(chan *routing.Route, 1)
	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = false
		cfg.HonorPeerAdvertisedPassive = true
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { addCh <- r; return nil },
			RouteDeleteFunc:     func(*routing.Route) error { return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Down -> Init with passive advertised
	cpInit := &ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}
	cpInit.SetPassive()
	m.HandleRx(cpInit, peer)

	// Init -> Up with passive still advertised
	cpUp := &ControlPacket{PeerDiscr: sess.localDiscr, LocalDiscr: sess.peerDiscr, State: StateUp}
	cpUp.SetPassive()
	m.HandleRx(cpUp, peer)

	// RouteAdd must have been called
	added := wait(t, addCh, 2*time.Second, "RouteAdd after Up with passive peer")
	require.Equal(t, r.Dst.String(), added.Dst.String())

	// And installed[] must be true
	rk := routeKeyFor(peer.Interface, sess.route)
	require.True(t, m.IsInstalled(rk), "route should be marked installed after Up even when peer is advertising passive")

	snap := sess.Snapshot()
	require.Equal(t, PeerModePassive, snap.PeerAdvertisedMode, "sanity: peer still advertising passive")
}

func TestClient_Liveness_Manager_IsPeerEffectivelyPassive(t *testing.T) {
	tests := []struct {
		name string
		cfg  ManagerConfig
		snap SessionSnapshot
		want bool
	}{
		{
			name: "global passive mode has no effect",
			cfg: ManagerConfig{
				PassiveMode:                true,
				HonorPeerAdvertisedPassive: false,
			},
			snap: SessionSnapshot{
				PeerAdvertisedMode: PeerModeActive,
			},
			want: false,
		},
		{
			name: "active, no flags -> not passive",
			cfg: ManagerConfig{
				PassiveMode:                false,
				HonorPeerAdvertisedPassive: false,
			},
			snap: SessionSnapshot{
				PeerAdvertisedMode: PeerModeActive,
			},
			want: false,
		},
		{
			name: "active, peer advertised passive, HonorPeerAdvertisedPassive enabled -> passive",
			cfg: ManagerConfig{
				PassiveMode:                false,
				HonorPeerAdvertisedPassive: true,
			},
			snap: SessionSnapshot{
				PeerAdvertisedMode: PeerModePassive,
			},
			want: true,
		},
		{
			name: "active, peer advertised not passive -> not passive",
			cfg: ManagerConfig{
				PassiveMode:                false,
				HonorPeerAdvertisedPassive: true,
			},
			snap: SessionSnapshot{
				PeerAdvertisedMode: PeerModeActive,
			},
			want: false,
		},
		{
			name: "active, peer advertised passive, HonorPeerAdvertisedPassive disabled -> not passive",
			cfg: ManagerConfig{
				PassiveMode:                false,
				HonorPeerAdvertisedPassive: false,
			},
			snap: SessionSnapshot{
				PeerAdvertisedMode: PeerModePassive,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			m := &manager{cfg: &tt.cfg}
			got := m.isPeerEffectivelyPassive(tt.snap)
			if got != tt.want {
				t.Fatalf("isEffectivelyPassive() = %v, want %v (cfg=%+v, snap=%+v)", got, tt.want, tt.cfg, tt.snap)
			}
		})
	}
}

func TestClient_Liveness_Manager_PeerSessionsMetrics_StateTransitions(t *testing.T) {
	t.Parallel()

	m, reg, err := newTestManagerWithMetrics(t, func(cfg *ManagerConfig) {
		cfg.EnablePeerMetrics = true
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}

	// Helper to read the peer_sessions gauge for a specific state.
	peerGauge := func(state State) float64 {
		return getGaugeValue(t, reg, "doublezero_liveness_peer_sessions", prometheus.Labels{
			LabelIface:   peer.Interface,
			LabelLocalIP: peer.LocalIP,
			LabelPeerIP:  peer.PeerIP,
			LabelState:   state.String(),
		})
	}

	// After RegisterRoute: session starts Down.
	require.Equal(t, 1.0, peerGauge(StateDown))
	require.Equal(t, 0.0, peerGauge(StateInit))
	require.Equal(t, 0.0, peerGauge(StateUp))
	require.GreaterOrEqual(t, peerGauge(StateDown), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateInit), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateUp), 0.0)

	// Drive Down -> Init.
	m.HandleRx(&ControlPacket{PeerDiscr: 0, LocalDiscr: 1, State: StateInit}, peer)

	require.Equal(t, 0.0, peerGauge(StateDown))
	require.Equal(t, 1.0, peerGauge(StateInit))
	require.Equal(t, 0.0, peerGauge(StateUp))
	require.GreaterOrEqual(t, peerGauge(StateDown), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateInit), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateUp), 0.0)

	// Grab session so we can echo discriminators.
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	require.NotNil(t, sess)

	// Init -> Up.
	m.HandleRx(&ControlPacket{
		PeerDiscr:  sess.localDiscr,
		LocalDiscr: sess.peerDiscr,
		State:      StateInit,
	}, peer)

	require.Equal(t, 0.0, peerGauge(StateDown))
	require.Equal(t, 0.0, peerGauge(StateInit))
	require.Equal(t, 1.0, peerGauge(StateUp))
	require.GreaterOrEqual(t, peerGauge(StateDown), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateInit), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateUp), 0.0)

	// Up -> remote AdminDown (Session.HandleRx maps this to StateDown).
	m.HandleRx(&ControlPacket{
		PeerDiscr:  sess.localDiscr,
		LocalDiscr: sess.peerDiscr,
		State:      StateAdminDown,
	}, peer)

	require.Equal(t, 1.0, peerGauge(StateDown))
	require.Equal(t, 0.0, peerGauge(StateInit))
	require.Equal(t, 0.0, peerGauge(StateUp))
	require.GreaterOrEqual(t, peerGauge(StateDown), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateInit), 0.0)
	require.GreaterOrEqual(t, peerGauge(StateUp), 0.0)
}

func TestClient_Liveness_Manager_OnSessionDown_EmitsConvergenceToDownWhenInstalled(t *testing.T) {
	t.Parallel()

	m, reg, err := newTestManagerWithMetrics(t, func(cfg *ManagerConfig) {
		cfg.PassiveMode = false
		cfg.Netlinker = &MockRouteReaderWriter{
			RouteAddFunc:        func(*routing.Route) error { return nil },
			RouteDeleteFunc:     func(*routing.Route) error { return nil },
			RouteByProtocolFunc: func(int) ([]*routing.Route, error) { return nil, nil },
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(func(r *routing.Route) {
		r.Src = net.IPv4(127, 0, 0, 1)
		r.Dst = &net.IPNet{IP: net.IPv4(127, 0, 0, 2), Mask: net.CIDRMask(32, 32)}
	})
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}

	// Synthetic session that just went Down after being installed.
	sess := &Session{
		peer:           &peer,
		route:          r,
		state:          StateDown,
		downSince:      time.Now(),
		lastDownReason: DownReasonTimeout,
		alive:          false,
	}
	// Pretend convergence started 200ms ago.
	sess.mu.Lock()
	sess.convDownStart = time.Now().Add(-200 * time.Millisecond)
	sess.mu.Unlock()

	rk := routeKeyFor(peer.Interface, r)
	m.mu.Lock()
	m.desired[rk] = r
	m.installed[rk] = true
	m.mu.Unlock()

	// Call onSessionDown directly.
	m.onSessionDown(sess)

	labels := prometheus.Labels{
		LabelIface:   peer.Interface,
		LabelLocalIP: peer.LocalIP,
	}
	count := getHistogramCount(t, reg, "doublezero_liveness_convergence_to_down_seconds", labels)
	require.Equal(t, float64(1), count, "expected one convergence_to_down sample when route was installed")

	// convDownStart should be cleared after accounting.
	snap := sess.Snapshot()
	require.True(t, snap.ConvDownStart.IsZero(), "convDownStart should be cleared after onSessionDown")
}

func newTestManager(t *testing.T, mutate func(*ManagerConfig)) (*manager, error) {
	m, _, err := newTestManagerWithMetrics(t, mutate)
	return m, err
}

func newTestManagerWithMetrics(t *testing.T, mutate func(*ManagerConfig)) (*manager, *prometheus.Registry, error) {
	reg := prometheus.NewRegistry()
	cfg := &ManagerConfig{
		Logger:          newTestLogger(t),
		Netlinker:       &MockRouteReaderWriter{},
		MetricsRegistry: reg,
		BindIP:          "127.0.0.1",
		Port:            0,
		TxMin:           100 * time.Millisecond,
		RxMin:           100 * time.Millisecond,
		DetectMult:      3,
		MinTxFloor:      50 * time.Millisecond,
		MaxTxCeil:       1 * time.Second,
		BackoffMax:      1 * time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}
	m, err := NewManager(t.Context(), cfg)
	return m, reg, err
}

func getGaugeValue(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()

	mfs, err := reg.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			if metricHasLabels(m, labels) {
				if g := m.GetGauge(); g != nil {
					return g.GetValue()
				}
			}
		}
	}
	// Treat “no sample” as 0 for gauges.
	return 0
}

func metricHasLabels(m *prom.Metric, labels prometheus.Labels) bool {
	if len(labels) == 0 {
		return true
	}
	for k, v := range labels {
		found := false
		for _, lp := range m.Label {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func getHistogramCount(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()

	mfs, err := reg.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			if metricHasLabels(m, labels) {
				if h := m.GetHistogram(); h != nil {
					return float64(h.GetSampleCount())
				}
			}
		}
	}
	// Treat “no sample” as 0 for histograms too.
	return 0
}
