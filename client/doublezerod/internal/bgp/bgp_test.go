package bgp_test

import (
	"context"
	"log"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
	"golang.org/x/sys/unix"
)

type mockRouteReaderWriter struct {
	routesAdded   []*routing.Route
	routesDeleted []*routing.Route
	routesFlushed []*routing.Route
}

func (m *mockRouteReaderWriter) RouteAdd(route *routing.Route) error {
	m.routesAdded = append(m.routesAdded, route)
	return nil
}

func (m *mockRouteReaderWriter) RouteDelete(route *routing.Route) error {
	m.routesDeleted = append(m.routesDeleted, route)
	return nil
}

func (m *mockRouteReaderWriter) RouteByProtocol(int) ([]*routing.Route, error) {
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
	b, err := bgp.NewBgpServer(net.IP{1, 1, 1, 1}, nlr)
	if err != nil {
		t.Fatalf("error creating bgp server: %v", err)
	}

	lc := &net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "tcp", ":6667")
	if err != nil {
		log.Fatalf("error constructing listener: %v", err)
	}

	err = b.AddPeer(
		&bgp.PeerConfig{
			LocalAddress:  net.IP{127, 0, 0, 1},
			RemoteAddress: net.IP{127, 0, 0, 1},
			LocalAs:       65000,
			RemoteAs:      65001,
			Port:          6666,
			FlushRoutes:   true,
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

	errChan := make(chan error)
	go func() {
		if err := b.Serve([]net.Listener{lis}); err != nil {
			errChan <- err
		}
	}()

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
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			status := b.GetPeerStatus(net.IP{127, 0, 0, 1})
			if status.SessionStatus == s {
				return true
			}
			time.Sleep(200 * time.Millisecond)
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
		if diff := checkRoutes(nlr.routesDeleted, want); diff != "" {
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
		if diff := checkRoutes(nlr.routesAdded, want); diff != "" {
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
		if diff := checkRoutes(nlr.routesFlushed, want); diff != "" {
			t.Fatalf("bgp flush mismatch: -(got); +(want): %s", diff)
		}
	})
}
