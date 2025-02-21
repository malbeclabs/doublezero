package bgp_test

import (
	"context"
	"log"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
)

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
	b, err := bgp.NewBgpServer(net.IP{1, 1, 1, 1})
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

	go func() {
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			errChan <- err
		}
	}()

	// route withdraws are written to a blocking channel prior route adds
	// so we need to check for withdraws first
	t.Run("validate_route_withdraw", func(t *testing.T) {
		select {
		case err := <-errChan:
			log.Fatalf("received error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for route withdraw")
		case got := <-b.WithdrawRoute():
			// bgp withdrawals have no nexthop attached so verify we set nexthop to the peer address
			want := bgp.NLRI{NextHop: "127.0.0.1", Prefix: "4.4.4.4", PrefixLength: 32}
			if diff := cmp.Diff(got, want); diff != "" {
				log.Fatalf("bgp withdraw mismatch: -(got); +(want): %s", diff)
			}
		}
	})

	t.Run("validate_route_add", func(t *testing.T) {
		select {
		case err := <-errChan:
			log.Fatalf("received error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for route update")
		case got := <-b.AddRoute():
			want := bgp.NLRI{NextHop: "2.2.2.2", Prefix: "3.3.3.3", PrefixLength: 32}
			if diff := cmp.Diff(got, want); diff != "" {
				log.Fatalf("bgp update mismatch: -(got); +(want): %s", diff)
			}
		}
	})
}
