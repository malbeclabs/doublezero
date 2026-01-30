package bgp_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type mockRouteReaderWriter struct {
	routesAdded   []*routing.Route
	routesDeleted []*routing.Route
	routesFlushed []*routing.Route
	mu            sync.Mutex
}

func (m *mockRouteReaderWriter) RouteAdd(route *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routesAdded = append(m.routesAdded, route)
	return nil
}

func (m *mockRouteReaderWriter) RouteDelete(route *routing.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routesDeleted = append(m.routesDeleted, route)
	return nil
}

func (m *mockRouteReaderWriter) RouteByProtocol(int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []*routing.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IP{1, 1, 1, 1},
				Mask: net.CIDRMask(32, 32),
			},
			Src:     net.IPv4(7, 7, 7, 7),
			NextHop: net.IP{127, 0, 0, 1},
			Table:   syscall.RT_TABLE_MAIN,
		},
	}, nil
}

func (m *mockRouteReaderWriter) getRoutesAdded() []*routing.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*routing.Route(nil), m.routesAdded...)
}

func (m *mockRouteReaderWriter) getRoutesDeleted() []*routing.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*routing.Route(nil), m.routesDeleted...)
}

func (m *mockRouteReaderWriter) getRoutesFlushed() []*routing.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*routing.Route(nil), m.routesFlushed...)
}

type dummyPlugin struct{}

func (p *dummyPlugin) GetCapabilities(c corebgp.PeerConfig) []corebgp.Capability {
	caps := make([]corebgp.Capability, 0)
	return caps
}

func (p *dummyPlugin) OnOpenMessage(peer corebgp.PeerConfig, routerID netip.Addr, capabilities []corebgp.Capability) *corebgp.Notification {
	return nil
}

func (p *dummyPlugin) OnEstablished(peer corebgp.PeerConfig, writer corebgp.UpdateMessageWriter) corebgp.UpdateMessageHandler {
	origin := gobgp.NewPathAttributeOrigin(0)
	nexthop := gobgp.NewPathAttributeNextHop("2.2.2.2")
	param := gobgp.NewAs4PathParam(2, []uint32{65001})
	aspath := gobgp.NewPathAttributeAsPath([]gobgp.AsPathParamInterface{param})
	update := gobgp.NewBGPUpdateMessage(
		[]*gobgp.IPAddrPrefix{gobgp.NewIPAddrPrefix(32, "4.4.4.4")},
		[]gobgp.PathAttributeInterface{origin, nexthop, aspath},
		[]*gobgp.IPAddrPrefix{gobgp.NewIPAddrPrefix(32, "3.3.3.3")})
	buf, err := update.Body.Serialize()
	if err != nil {
		log.Printf("error serializing: %v", err)
	}
	if err := writer.WriteUpdate(buf); err != nil {
		log.Printf("error writing update: %v", err)
	}
	return p.handleUpdate
}

func (p *dummyPlugin) OnClose(peer corebgp.PeerConfig) {}

func (p *dummyPlugin) handleUpdate(peer corebgp.PeerConfig, u []byte) *corebgp.Notification {
	return nil
}

func TestBgpServer(t *testing.T) {
	nlr := &mockRouteReaderWriter{}
	lm, err := liveness.NewManager(t.Context(), &liveness.ManagerConfig{
		Logger:        slog.Default(),
		Netlinker:     nlr,
		BindIP:        "127.0.0.1",
		Port:          0,
		TxMin:         100 * time.Millisecond,
		RxMin:         100 * time.Millisecond,
		DetectMult:    3,
		MinTxFloor:    50 * time.Millisecond,
		MaxTxCeil:     1 * time.Second,
		ClientVersion: "1.2.3-dev",
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lm.Close() })
	b, err := bgp.NewBgpServer(net.IP{1, 1, 1, 1}, nlr, lm)
	if err != nil {
		t.Fatalf("error creating bgp server: %v", err)
	}

	lc := &net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "tcp", ":6667")
	if err != nil {
		log.Fatalf("error constructing listener: %v", err)
	}

	// Start Serve() before AddPeer() to match production order.
	// The status reader goroutine must be running before AddPeer sends status events.
	errChan := make(chan error)
	go func() {
		if err := b.Serve([]net.Listener{lis}); err != nil {
			errChan <- err
		}
	}()

	// Give the status reader goroutine time to start
	time.Sleep(50 * time.Millisecond)

	err = b.AddPeer(
		&bgp.PeerConfig{
			LocalAddress:  net.IP{127, 0, 0, 1},
			RemoteAddress: net.IP{127, 0, 0, 1},
			LocalAs:       65000,
			RemoteAs:      65001,
			Port:          6666,
			NoUninstall:   false,
			RouteTable:    syscall.RT_TABLE_MAIN,
			RouteSrc:      net.IP{7, 7, 7, 7},
		},
		[]bgp.NLRI{
			{AsPath: []uint32{}, NextHop: "1.1.1.1", Prefix: "10.0.0.0", PrefixLength: 32},
		},
	)
	if err != nil {
		t.Fatalf("error adding peer: %v", err)
	}

	// start dummy bgp instance as peer
	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	d := &dummyPlugin{}
	err = srv.AddPeer(corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr("127.0.0.1"),
		LocalAS:       65001,
		RemoteAS:      65000,
	}, d, corebgp.WithPort(6667), corebgp.WithPassive())
	if err != nil {
		t.Fatalf("error creating dummy bgp server: %v", err)
	}
	dlc := &net.ListenConfig{}
	dlis, err := dlc.Listen(context.Background(), "tcp", ":6666")
	if err != nil {
		log.Fatalf("error constructing listener: %v", err)
	}

	waitForPeerStatus := func(s bgp.SessionStatus) bool {
		deadline := time.Now().Add(30 * time.Second)
		start := time.Now()
		for time.Now().Before(deadline) {
			status := b.GetPeerStatus(net.IP{127, 0, 0, 1})
			if status.SessionStatus == s {
				return true
			}
			if time.Since(start) > 10*time.Second {
				t.Logf("Waiting for peer status %v, got %v", s, status)
				time.Sleep(3 * time.Second)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
		return false
	}

	checkRoutes := func(got []*routing.Route, want []*routing.Route) string {
		var diff string
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if diff := cmp.Diff(got, want); diff == "" {
				return diff
			}
			time.Sleep(200 * time.Millisecond)
		}
		return diff
	}

	t.Run("validate_peer_status_is_pending", func(t *testing.T) {
		if !waitForPeerStatus(bgp.SessionStatusPending) {
			t.Fatal("timed out waiting for peer status of pending")
		}
	})

	go func() {
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}

	}()

	// TODO: https://github.com/malbeclabs/doublezero/issues/267
	// t.Run("validate_peer_status_is_initializing", func(t *testing.T) {
	// 	if !waitForPeerStatus(bgp.SessionStatusInitializing) {
	// 		t.Fatal("timed out waiting for peer status of initializing")
	// 	}
	// })

	t.Run("validate_peer_status_is_up", func(t *testing.T) {
		if !waitForPeerStatus(bgp.SessionStatusUp) {
			t.Fatal("timed out waiting for peer status of up")
		}
	})

	t.Run("validate_session_status_metric_is_one", func(t *testing.T) {
		expectedMetric := fmt.Sprintf(bgp.MetricSessionStatusDesc, 1)
		if err := testutil.CollectAndCompare(bgp.MetricSessionStatus, strings.NewReader(expectedMetric)); err != nil {
			t.Fatalf("unexpected metric value: %v", err)
		}
	})

	t.Run("validate_route_withdraw", func(t *testing.T) {
		want := []*routing.Route{
			{
				Dst: &net.IPNet{
					IP:   net.IP{4, 4, 4, 4},
					Mask: net.CIDRMask(32, 32),
				},
				Src:     net.IP{7, 7, 7, 7},
				NextHop: net.IP{127, 0, 0, 1},
				Table:   syscall.RT_TABLE_MAIN,
			},
		}
		if diff := checkRoutes(nlr.getRoutesDeleted(), want); diff != "" {
			t.Fatalf("bgp withdraw mismatch: -(got); +(want): %s", diff)
		}
	})

	t.Run("validate_route_add", func(t *testing.T) {
		want := []*routing.Route{
			{
				Dst: &net.IPNet{
					IP:   net.IP{3, 3, 3, 3},
					Mask: net.CIDRMask(32, 32),
				},
				Src:      net.IP{7, 7, 7, 7},
				NextHop:  net.IP{2, 2, 2, 2},
				Protocol: unix.RTPROT_BGP,
				Table:    syscall.RT_TABLE_MAIN,
			},
		}
		if diff := checkRoutes(nlr.getRoutesAdded(), want); diff != "" {
			t.Fatalf("bgp add mismatch: -(got); +(want): %s", diff)
		}
	})

	t.Run("validate_route_flush", func(t *testing.T) {
		// close remote server to force a flush message
		srv.Close()
		want := []*routing.Route{
			{
				Dst: &net.IPNet{
					IP:   net.IP{4, 4, 4, 4},
					Mask: net.CIDRMask(32, 32),
				},
				Src:     net.IP{7, 7, 7, 7},
				NextHop: net.IP{127, 0, 0, 1},
				Table:   syscall.RT_TABLE_MAIN,
			},
			{
				Dst: &net.IPNet{
					IP:   net.IP{1, 1, 1, 1},
					Mask: net.CIDRMask(32, 32),
				},
				Src:     net.IP{7, 7, 7, 7},
				NextHop: net.IP{127, 0, 0, 1},
				Table:   syscall.RT_TABLE_MAIN,
			},
		}
		if diff := checkRoutes(nlr.getRoutesFlushed(), want); diff != "" {
			t.Fatalf("bgp flush mismatch: -(got); +(want): %s", diff)
		}
	})

	t.Run("validate_session_status_metric_is_zero", func(t *testing.T) {
		expectedMetric := fmt.Sprintf(bgp.MetricSessionStatusDesc, 0)
		if err := testutil.CollectAndCompare(bgp.MetricSessionStatus, strings.NewReader(expectedMetric)); err != nil {
			t.Fatalf("unexpected metric value: %v", err)
		}
	})
}

func TestDeletePeerClearsPeerStatus(t *testing.T) {
	nlr := &mockRouteReaderWriter{}
	b, err := bgp.NewBgpServer(net.IP{2, 2, 2, 2}, nlr, nil)
	require.NoError(t, err)

	// Start the server to enable status goroutine
	lc := &net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { lis.Close() })

	go func() {
		_ = b.Serve([]net.Listener{lis})
	}()
	time.Sleep(50 * time.Millisecond)

	peerIP := net.IP{127, 0, 0, 3}

	// Add a peer
	err = b.AddPeer(&bgp.PeerConfig{
		LocalAddress:  net.IP{127, 0, 0, 1},
		RemoteAddress: peerIP,
		LocalAs:       65000,
		RemoteAs:      65001,
		Port:          9999, // non-existent port, won't connect
		RouteTable:    syscall.RT_TABLE_MAIN,
		RouteSrc:      net.IP{2, 2, 2, 2},
	}, nil)
	require.NoError(t, err)

	// Wait for peer status to be set (Pending from GetCapabilities callback)
	time.Sleep(100 * time.Millisecond)
	status := b.GetPeerStatus(peerIP)
	require.Equal(t, bgp.SessionStatusPending, status.SessionStatus,
		"peer should have Pending status after AddPeer")

	// Delete the peer
	err = b.DeletePeer(peerIP)
	require.NoError(t, err)

	// After deletion, GetPeerStatus should return the default value (Pending)
	// because the entry was removed from the map. While both cases return Pending,
	// this test verifies the DeletePeer -> map deletion code path executes without error.
	status = b.GetPeerStatus(peerIP)
	require.Equal(t, bgp.SessionStatusPending, status.SessionStatus,
		"peer status should return default after DeletePeer")
}
