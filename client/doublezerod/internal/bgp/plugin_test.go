package bgp

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type mockUpdateWriter struct {
	updates []*gobgp.BGPUpdate
}

func (m *mockUpdateWriter) WriteUpdate(u []byte) error {
	upd := &gobgp.BGPUpdate{}
	if err := upd.DecodeFromBytes(u); err != nil {
		return err
	}
	m.updates = append(m.updates, upd)
	return nil
}

func TestClient_BGPPlugin_GetCapabilitiesSendsPendingStatus(t *testing.T) {
	t.Parallel()

	peerStatus := make(chan SessionEvent, 1)
	p := &Plugin{
		PeerStatusChan: peerStatus,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.1"),
	}

	caps := p.GetCapabilities(peer)
	require.Len(t, caps, 1)

	select {
	case ev := <-peerStatus:
		require.True(t, ev.PeerAddr.Equal(net.ParseIP("192.0.2.1")), "unexpected peer addr: %v", ev.PeerAddr)
		require.Equal(t, SessionStatusPending, ev.Session.SessionStatus)
	default:
		require.Fail(t, "expected a SessionEvent on PeerStatusChan")
	}
}

func TestClient_BGPPlugin_OnOpenMessageSendsInitializingStatus(t *testing.T) {
	t.Parallel()

	peerStatus := make(chan SessionEvent, 1)
	p := &Plugin{
		PeerStatusChan: peerStatus,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("198.51.100.1"),
	}

	notif := p.OnOpenMessage(peer, netip.MustParseAddr("203.0.113.1"), nil)
	require.Nil(t, notif)

	select {
	case ev := <-peerStatus:
		require.True(t, ev.PeerAddr.Equal(net.ParseIP("198.51.100.1")), "unexpected peer addr: %v", ev.PeerAddr)
		require.Equal(t, SessionStatusInitializing, ev.Session.SessionStatus)
	default:
		require.Fail(t, "expected a SessionEvent on PeerStatusChan")
	}
}

func TestClient_BGPPlugin_OnEstablishedAdvertisesAllNLRIsAndSendsUpStatus(t *testing.T) {
	t.Parallel()

	peerStatus := make(chan SessionEvent, 1)
	writer := &mockUpdateWriter{}
	nlris := []NLRI{
		{Prefix: "10.0.0.0", PrefixLength: 24, NextHop: "192.0.2.254", AsPath: []uint32{64512}},
		{Prefix: "10.0.1.0", PrefixLength: 24, NextHop: "192.0.2.254", AsPath: []uint32{64512, 64513}},
	}

	p := &Plugin{
		AdvertisedNLRI: nlris,
		PeerStatusChan: peerStatus,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.2"),
	}

	handler := p.OnEstablished(peer, writer)
	require.NotNil(t, handler)

	require.Len(t, writer.updates, len(nlris))

	select {
	case ev := <-peerStatus:
		require.Equal(t, SessionStatusUp, ev.Session.SessionStatus)
	default:
		require.Fail(t, "expected a SessionEvent on PeerStatusChan")
	}
}

func TestClient_BGPPlugin_OnCloseDeletesRoutesAndSendsDownStatus(t *testing.T) {
	t.Parallel()

	peerStatus := make(chan SessionEvent, 1)

	var gotProtocol int
	var deletedRoutes []*routing.Route

	peerIP := net.ParseIP("203.0.113.1")
	otherPeerIP := net.ParseIP("203.0.113.2")

	mockRW := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			gotProtocol = p
			return []*routing.Route{
				{
					Dst:     &net.IPNet{IP: net.IP{1, 1, 1, 1}, Mask: net.CIDRMask(32, 32)},
					NextHop: peerIP,
				},
				{
					Dst:     &net.IPNet{IP: net.IP{2, 2, 2, 2}, Mask: net.CIDRMask(32, 32)},
					NextHop: otherPeerIP,
				},
			}, nil
		},
		RouteDeleteFunc: func(r *routing.Route) error {
			deletedRoutes = append(deletedRoutes, r)
			return nil
		},
	}

	p := &Plugin{
		PeerStatusChan:    peerStatus,
		RouteReaderWriter: mockRW,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("203.0.113.1"),
	}

	p.OnClose(peer)

	select {
	case ev := <-peerStatus:
		require.Equal(t, SessionStatusDown, ev.Session.SessionStatus)
	default:
		require.Fail(t, "expected a SessionEvent on PeerStatusChan")
	}

	require.Equal(t, unix.RTPROT_BGP, gotProtocol)
	require.Len(t, deletedRoutes, 1, "should only delete routes matching the closed peer's gateway")
	require.Equal(t, "1.1.1.1/32", deletedRoutes[0].Dst.String())
	require.True(t, deletedRoutes[0].NextHop.Equal(peerIP))
}

func TestClient_BGPPlugin_OnCloseSkipsRouteDeletionWhenNoInstall(t *testing.T) {
	t.Parallel()

	peerStatus := make(chan SessionEvent, 1)
	routeByProtocolCalled := false

	mockRW := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			routeByProtocolCalled = true
			return nil, nil
		},
	}

	p := &Plugin{
		PeerStatusChan:    peerStatus,
		NoInstall:         true,
		RouteReaderWriter: mockRW,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("203.0.113.1"),
	}

	p.OnClose(peer)

	select {
	case ev := <-peerStatus:
		require.Equal(t, SessionStatusDown, ev.Session.SessionStatus)
	default:
		require.Fail(t, "expected a SessionEvent on PeerStatusChan")
	}

	require.False(t, routeByProtocolCalled, "should not query routes when NoInstall is set")
}

func TestClient_BGPPlugin_HandleUpdateWithdrawDeletesRouteWithPeerAsNextHop(t *testing.T) {
	t.Parallel()

	var deleted []*routing.Route

	mockRW := &MockRouteReaderWriter{
		RouteDeleteFunc: func(r *routing.Route) error {
			deleted = append(deleted, r)
			return nil
		},
	}

	p := &Plugin{
		RouteSrc:          net.IPv4(172, 16, 0, 1),
		RouteTable:        254,
		RouteReaderWriter: mockRW,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.10"),
	}

	withdraw := gobgp.NewIPAddrPrefix(32, "10.10.10.10")
	msg := gobgp.NewBGPUpdateMessage([]*gobgp.IPAddrPrefix{withdraw}, nil, nil)
	body, err := msg.Body.Serialize()
	require.NoError(t, err)

	notif := p.handleUpdate(peer, body)
	require.Nil(t, notif)

	require.Len(t, deleted, 1)

	r := deleted[0]
	require.True(t, r.Src.Equal(p.RouteSrc), "unexpected src, got %v want %v", r.Src, p.RouteSrc)
	require.Equal(t, p.RouteTable, r.Table)
	require.NotNil(t, r.Dst)
	require.Equal(t, "10.10.10.10/32", r.Dst.String())
	require.True(t, r.NextHop.Equal(net.IP(peer.RemoteAddress.AsSlice())),
		"unexpected nexthop, got %v want %v", r.NextHop, peer.RemoteAddress.AsSlice())
}

func TestClient_BGPPlugin_HandleUpdateInstallRouteFromNlriAndNextHop(t *testing.T) {
	t.Parallel()

	var added []*routing.Route

	mockRW := &MockRouteReaderWriter{
		RouteAddFunc: func(r *routing.Route) error {
			added = append(added, r)
			return nil
		},
	}

	p := &Plugin{
		RouteSrc:          net.IPv4(10, 0, 0, 1),
		RouteTable:        100,
		RouteReaderWriter: mockRW,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.20"),
	}

	nexthop := "192.0.2.254"
	attrNH := gobgp.NewPathAttributeNextHop(nexthop)
	nlri := gobgp.NewIPAddrPrefix(24, "203.0.113.0")

	msg := gobgp.NewBGPUpdateMessage(nil, []gobgp.PathAttributeInterface{attrNH}, []*gobgp.IPAddrPrefix{nlri})
	body, err := msg.Body.Serialize()
	require.NoError(t, err)

	notif := p.handleUpdate(peer, body)
	require.Nil(t, notif)

	require.Len(t, added, 1)

	r := added[0]
	require.True(t, r.Src.Equal(p.RouteSrc), "unexpected src, got %v want %v", r.Src, p.RouteSrc)
	require.Equal(t, p.RouteTable, r.Table)
	require.NotNil(t, r.Dst)
	require.Equal(t, "203.0.113.0/24", r.Dst.String())
	require.True(t, r.NextHop.Equal(net.ParseIP(nexthop)),
		"unexpected nexthop, got %v want %v", r.NextHop, nexthop)
	require.Equal(t, unix.RTPROT_BGP, r.Protocol)
}

func TestClient_BGPPlugin_HandleUpdateNoInstallSkipsRouteChanges(t *testing.T) {
	t.Parallel()

	var calledAdd, calledDel bool

	mockRW := &MockRouteReaderWriter{
		RouteAddFunc: func(r *routing.Route) error {
			calledAdd = true
			return nil
		},
		RouteDeleteFunc: func(r *routing.Route) error {
			calledDel = true
			return nil
		},
	}

	p := &Plugin{
		RouteSrc:          net.IPv4(10, 0, 0, 1),
		RouteTable:        100,
		RouteReaderWriter: mockRW,
		NoInstall:         true,
	}

	peer := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.30"),
	}

	nexthop := "192.0.2.254"
	attrNH := gobgp.NewPathAttributeNextHop(nexthop)
	nlri := gobgp.NewIPAddrPrefix(24, "198.51.100.0")

	msg := gobgp.NewBGPUpdateMessage(nil, []gobgp.PathAttributeInterface{attrNH}, []*gobgp.IPAddrPrefix{nlri})
	body, err := msg.Body.Serialize()
	require.NoError(t, err)

	_ = p.handleUpdate(peer, body)

	require.False(t, calledAdd, "expected no RouteAdd calls when NoInstall is true")
	require.False(t, calledDel, "expected no RouteDelete calls when NoInstall is true")
}

func TestClient_BGPPlugin_BuildUpdateRoundTrip(t *testing.T) {
	t.Parallel()

	p := &Plugin{}

	nlri := NLRI{
		Prefix:       "198.51.100.0",
		PrefixLength: 24,
		NextHop:      "192.0.2.254",
		AsPath:       []uint32{64512, 64513},
	}

	body, err := p.buildUpdate(nlri)
	require.NoError(t, err)

	var upd gobgp.BGPUpdate
	require.NoError(t, upd.DecodeFromBytes(body))

	require.Len(t, upd.NLRI, 1)
	require.Equal(t, nlri.Prefix, upd.NLRI[0].Prefix.String())
	require.Equal(t, int(nlri.PrefixLength), int(upd.NLRI[0].Length))

	var gotNextHop net.IP
	var gotAsPath []uint32

	for _, attr := range upd.PathAttributes {
		switch attr.GetType() {
		case gobgp.BGP_ATTR_TYPE_NEXT_HOP:
			gotNextHop = attr.(*gobgp.PathAttributeNextHop).Value
		case gobgp.BGP_ATTR_TYPE_AS4_PATH, gobgp.BGP_ATTR_TYPE_AS_PATH:
			asAttr := attr.(*gobgp.PathAttributeAsPath)
			for _, pth := range asAttr.Value {
				gotAsPath = append(gotAsPath, pth.GetAS()...)
			}
		}
	}

	require.True(t, gotNextHop.Equal(net.ParseIP(nlri.NextHop)),
		"unexpected nexthop, got %v want %v", gotNextHop, nlri.NextHop)

	require.Len(t, gotAsPath, len(nlri.AsPath))
	for i := range nlri.AsPath {
		require.Equal(t, nlri.AsPath[i], gotAsPath[i], "unexpected AS at index %d", i)
	}
}

func TestEmitTimeoutStatus_Unreachable(t *testing.T) {
	// When tcpConnected is false and session is not established,
	// emitTimeoutStatus should emit SessionStatusUnreachable
	statusChan := make(chan SessionEvent, 1)
	plugin := &Plugin{
		PeerStatusChan: statusChan,
		peerAddr:       net.ParseIP("10.0.0.1"),
	}

	// Neither tcpConnected nor currentlyEstablished are set (both default to false)
	emitted := plugin.emitTimeoutStatus()

	require.True(t, emitted, "expected status to be emitted")
	event := <-statusChan
	require.Equal(t, SessionStatusUnreachable, event.Session.SessionStatus)
	require.True(t, event.PeerAddr.Equal(net.ParseIP("10.0.0.1")))
}

func TestEmitTimeoutStatus_Failed(t *testing.T) {
	// When tcpConnected is true but session is not established,
	// emitTimeoutStatus should emit SessionStatusFailed
	statusChan := make(chan SessionEvent, 1)
	plugin := &Plugin{
		PeerStatusChan: statusChan,
		peerAddr:       net.ParseIP("10.0.0.1"),
	}

	// TCP connected but BGP handshake didn't complete
	plugin.tcpConnected.Store(true)

	emitted := plugin.emitTimeoutStatus()

	require.True(t, emitted, "expected status to be emitted")
	event := <-statusChan
	require.Equal(t, SessionStatusFailed, event.Session.SessionStatus)
	require.True(t, event.PeerAddr.Equal(net.ParseIP("10.0.0.1")))
}

func TestEmitTimeoutStatus_NoEmitWhenEstablished(t *testing.T) {
	// When session is already established, emitTimeoutStatus should not emit
	statusChan := make(chan SessionEvent, 1)
	plugin := &Plugin{
		PeerStatusChan: statusChan,
		peerAddr:       net.ParseIP("10.0.0.1"),
	}

	// Session is established
	plugin.currentlyEstablished.Store(true)

	emitted := plugin.emitTimeoutStatus()

	require.False(t, emitted, "expected no status to be emitted when established")
	select {
	case <-statusChan:
		t.Fatal("unexpected event in status channel")
	default:
		// Expected: channel should be empty
	}
}

// TestPlugin_MarkDeleted_PreventsTimeoutEmission tests that marking a plugin as deleted
// prevents timeout status emissions. This is critical for avoiding "BGP Session Failed"
// status when a peer is intentionally deleted during device rollover.
func TestPlugin_MarkDeleted_PreventsTimeoutEmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tcpConnected      bool
		expectedEmitted   bool
		markDeletedBefore bool
	}{
		{
			name:              "deleted before timeout prevents emission",
			tcpConnected:      false,
			expectedEmitted:   false,
			markDeletedBefore: true,
		},
		{
			name:              "not deleted allows timeout emission for unreachable",
			tcpConnected:      false,
			expectedEmitted:   true,
			markDeletedBefore: false,
		},
		{
			name:              "not deleted allows timeout emission for failed",
			tcpConnected:      true,
			expectedEmitted:   true,
			markDeletedBefore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusChan := make(chan SessionEvent, 10)
			mockRW := &MockRouteReaderWriter{
				RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
					return nil, nil
				},
			}
			plugin := NewBgpPlugin(
				[]NLRI{},
				net.ParseIP("10.0.0.1"),
				100,
				statusChan,
				true,
				mockRW,
			)
			plugin.peerAddr = net.ParseIP("192.0.2.1")

			if tt.tcpConnected {
				plugin.tcpConnected.Store(true)
			}

			if tt.markDeletedBefore {
				plugin.MarkDeleted()
			}

			// Directly call emitTimeoutStatus to test the logic
			emitted := plugin.emitTimeoutStatus()

			require.Equal(t, tt.expectedEmitted, emitted, "unexpected emission result")

			if tt.expectedEmitted {
				select {
				case event := <-statusChan:
					if tt.tcpConnected {
						require.Equal(t, SessionStatusFailed, event.Session.SessionStatus)
					} else {
						require.Equal(t, SessionStatusUnreachable, event.Session.SessionStatus)
					}
				default:
					t.Fatal("expected status event but got none")
				}
			} else {
				select {
				case <-statusChan:
					t.Fatal("unexpected status event received")
				default:
					// Expected: no event
				}
			}
		})
	}
}

// TestPlugin_OnClose_NoTimeoutAfterMarkDeleted tests that OnClose does not start a new
// timeout when the peer has been marked as deleted. This prevents spurious timeout events
// for peers that won't reconnect during device rollover.
func TestPlugin_OnClose_NoTimeoutAfterMarkDeleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		markDeletedBeforeClose bool
		expectTimeoutStarted   bool
	}{
		{
			name:                   "normal close starts timeout for reconnection",
			markDeletedBeforeClose: false,
			expectTimeoutStarted:   true,
		},
		{
			name:                   "close after deletion does not start timeout",
			markDeletedBeforeClose: true,
			expectTimeoutStarted:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusChan := make(chan SessionEvent, 10)
			mockRW := &MockRouteReaderWriter{
				RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
					return nil, nil
				},
			}
			plugin := NewBgpPlugin(
				[]NLRI{},
				net.ParseIP("10.0.0.1"),
				100,
				statusChan,
				true,
				mockRW,
			)
			plugin.peerAddr = net.ParseIP("192.0.2.1")
			plugin.currentlyEstablished.Store(true)

			if tt.markDeletedBeforeClose {
				plugin.MarkDeleted()
			}

			peerConfig := corebgp.PeerConfig{
				RemoteAddress: netip.MustParseAddr("192.0.2.1"),
			}
			plugin.OnClose(peerConfig)

			// Check if cancelTimeout was set (indicating timeout was started)
			timeoutWasStarted := plugin.cancelTimeout != nil

			require.Equal(t, tt.expectTimeoutStarted, timeoutWasStarted,
				"timeout start state mismatch")
		})
	}
}

// TestPlugin_MarkDeleted_CancelsInFlightTimeout tests that MarkDeleted cancels any
// in-flight timeout that may have been started from a prior network disconnect.
func TestPlugin_MarkDeleted_CancelsInFlightTimeout(t *testing.T) {
	t.Parallel()

	statusChan := make(chan SessionEvent, 10)
	mockRW := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	plugin := NewBgpPlugin(
		[]NLRI{},
		net.ParseIP("10.0.0.1"),
		100,
		statusChan,
		true,
		mockRW,
	)
	plugin.peerAddr = net.ParseIP("192.0.2.1")
	plugin.tcpConnected.Store(true)

	// Start a timeout
	plugin.startSessionTimeout()
	require.NotNil(t, plugin.cancelTimeout, "timeout should be started")

	// Mark as deleted which should cancel the timeout
	plugin.MarkDeleted()

	// Wait for the original timeout period
	// Note: Using a shorter wait for test speed
	select {
	case event := <-statusChan:
		t.Fatalf("unexpected status event received after MarkDeleted: %v", event.Session.SessionStatus)
	case <-time.After(50 * time.Millisecond):
		// Expected: no event within reasonable time
	}

	// Verify deleted flag is set
	require.True(t, plugin.deleted.Load(), "deleted flag should be set")
}

// TestPlugin_OnEstablished_TOCTOURace_MutexPreventsStatusOverwrite tests the fix for the
// TOCTOU race where the timeout goroutine could emit a "Failed" status just as the session
// establishes, overwriting the correct "Up" status. The mutex ensures the established flag
// check in emitTimeoutStatus is atomic with the flag set in OnEstablished.
func TestPlugin_OnEstablished_TOCTOURace_MutexPreventsStatusOverwrite(t *testing.T) {
	t.Parallel()

	// This test verifies that the mutex prevents the race condition.
	// We simulate concurrent calls to OnEstablished and emitTimeoutStatus
	// and verify that once established is set, timeout emission is blocked.

	statusChan := make(chan SessionEvent, 10)
	mockRW := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	plugin := NewBgpPlugin(
		[]NLRI{},
		net.ParseIP("10.0.0.1"),
		100,
		statusChan,
		true,
		mockRW,
	)
	plugin.peerAddr = net.ParseIP("192.0.2.1")
	plugin.tcpConnected.Store(true)

	// Spawn multiple goroutines that try to emit timeout concurrently with OnEstablished
	done := make(chan bool)
	var emitCount int

	// Start goroutines trying to emit timeout
	for i := 0; i < 5; i++ {
		go func() {
			time.Sleep(1 * time.Millisecond)
			if plugin.emitTimeoutStatus() {
				emitCount++
			}
			done <- true
		}()
	}

	// Call OnEstablished which should set the flag under lock
	peerConfig := corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("192.0.2.1"),
	}
	plugin.OnEstablished(peerConfig, &mockUpdateWriter{})

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	// Collect all events
	var events []SessionEvent
	for {
		select {
		case event := <-statusChan:
			events = append(events, event)
		default:
			goto eventsDone
		}
	}
eventsDone:

	// Analyze the events: we should see only "Up" status from OnEstablished
	// The mutex should prevent any emitTimeoutStatus calls from succeeding after established=true
	var hasUp, hasFailed bool
	for _, event := range events {
		if event.Session.SessionStatus == SessionStatusUp {
			hasUp = true
		}
		if event.Session.SessionStatus == SessionStatusFailed {
			hasFailed = true
		}
	}

	require.True(t, hasUp, "should have received SessionStatusUp from OnEstablished")
	require.False(t, hasFailed, "should NOT have received SessionStatusFailed from timeout after establish due to mutex protection")
}

// TestPlugin_emitTimeoutStatus_RespectsEstablishedFlag tests that emitTimeoutStatus
// returns false without emitting when the session is already established.
func TestPlugin_emitTimeoutStatus_RespectsEstablishedFlag(t *testing.T) {
	t.Parallel()

	statusChan := make(chan SessionEvent, 10)
	mockRW := &MockRouteReaderWriter{
		RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
			return nil, nil
		},
	}
	plugin := NewBgpPlugin(
		[]NLRI{},
		net.ParseIP("10.0.0.1"),
		100,
		statusChan,
		true,
		mockRW,
	)
	plugin.peerAddr = net.ParseIP("192.0.2.1")
	plugin.tcpConnected.Store(true)

	// Mark as established
	plugin.currentlyEstablished.Store(true)

	// Try to emit timeout status
	emitted := plugin.emitTimeoutStatus()

	require.False(t, emitted, "should not emit when session is established")

	// Verify no event was sent
	select {
	case <-statusChan:
		t.Fatal("unexpected status event when session is established")
	default:
		// Expected: no event
	}
}
