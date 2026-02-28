//go:build container_tests

package pim_test

import (
	"log"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/gopacket"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"golang.org/x/net/ipv4"
)

// implement packetconn interface
type mockRawConn struct {
	writeChan chan []byte
}

func (m *mockRawConn) WriteTo(h *ipv4.Header, b []byte, cm *ipv4.ControlMessage) error {
	m.writeChan <- b
	return nil
}

func (m *mockRawConn) Close() error {
	return nil
}

func (m *mockRawConn) SetMulticastInterface(iface *net.Interface) error {
	return nil
}

func (m *mockRawConn) SetControlMessage(cm ipv4.ControlFlags, on bool) error {
	return nil
}

func TestPIMServer(t *testing.T) {
	c := &mockRawConn{writeChan: make(chan []byte, 10)}
	svr := pim.NewPIMServer()
	err := svr.Start(c, "eth0", net.IPv4(169, 254, 0, 0), []net.IP{net.IPv4(239, 0, 0, 1)})
	if err != nil {
		log.Fatalf("failed to start PIM server: %v", err)
	}

	t.Run("verify_pim_hello_sent", func(t *testing.T) {
		var helloMsgBuf []byte

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for hello message")
		case helloMsgBuf = <-c.writeChan:
		}

		checkHelloMesssage(t, helloMsgBuf)
	})

	t.Run("verify_pim_join_sent", func(t *testing.T) {
		var joinMsgBuf []byte

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for join message")
		case joinMsgBuf = <-c.writeChan:
		}
		checkJoinMesssage(t, joinMsgBuf)
	})

	// close server to prunes are sent
	svr.Close()

	t.Run("verify_pim_prune_sent", func(t *testing.T) {
		var pruneMsgBuf []byte

		select {
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for prune message")
		case pruneMsgBuf = <-c.writeChan:
		}
		checkPruneMesssage(t, pruneMsgBuf)
	})
}

func TestPIMServer_UpdateGroups(t *testing.T) {
	c := &mockRawConn{writeChan: make(chan []byte, 10)}
	svr := pim.NewPIMServer()
	err := svr.Start(c, "eth0", net.IPv4(169, 254, 0, 0), []net.IP{net.IPv4(239, 0, 0, 1)})
	if err != nil {
		t.Fatalf("failed to start PIM server: %v", err)
	}

	// Drain initial hello + join.
	for range 2 {
		select {
		case <-c.writeChan:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for initial PIM messages")
		}
	}

	// Add a second group — should send a join for the added group on the same conn.
	err = svr.UpdateGroups([]net.IP{net.IPv4(239, 0, 0, 1), net.IPv4(239, 0, 0, 2)})
	if err != nil {
		t.Fatalf("UpdateGroups (add) failed: %v", err)
	}

	// Expect a join message for the added group on the same connection.
	select {
	case msg := <-c.writeChan:
		// Verify it's a join/prune message (for the added group 239.0.0.2).
		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("error decoding join packet: %v", p.ErrorLayer().Error())
		}
		jp, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage)
		if !ok {
			t.Fatal("expected JoinPruneMessage layer")
		}
		if jp.NumGroups != 1 {
			t.Fatalf("expected 1 group in join, got %d", jp.NumGroups)
		}
		if !jp.Groups[0].MulticastGroupAddress.Equal(net.IPv4(239, 0, 0, 2)) {
			t.Fatalf("expected join for 239.0.0.2, got %v", jp.Groups[0].MulticastGroupAddress)
		}
		if len(jp.Groups[0].Joins) != 1 {
			t.Fatalf("expected 1 join source, got %d", len(jp.Groups[0].Joins))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for join message after UpdateGroups (add)")
	}

	// Remove the first group — should send a prune for the removed group.
	err = svr.UpdateGroups([]net.IP{net.IPv4(239, 0, 0, 2)})
	if err != nil {
		t.Fatalf("UpdateGroups (remove) failed: %v", err)
	}

	// Expect a prune message for the removed group on the same connection.
	select {
	case msg := <-c.writeChan:
		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("error decoding prune packet: %v", p.ErrorLayer().Error())
		}
		jp, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage)
		if !ok {
			t.Fatal("expected JoinPruneMessage layer")
		}
		if jp.NumGroups != 1 {
			t.Fatalf("expected 1 group in prune, got %d", jp.NumGroups)
		}
		if !jp.Groups[0].MulticastGroupAddress.Equal(net.IPv4(239, 0, 0, 1)) {
			t.Fatalf("expected prune for 239.0.0.1, got %v", jp.Groups[0].MulticastGroupAddress)
		}
		if len(jp.Groups[0].Prunes) != 1 {
			t.Fatalf("expected 1 prune source, got %d", len(jp.Groups[0].Prunes))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for prune message after UpdateGroups (remove)")
	}

	svr.Close()
}

func checkHelloMesssage(t *testing.T, b []byte) {
	p := gopacket.NewPacket(b, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.Hello,
				Checksum: 0x4317,
			},
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("PIMMessage mismatch (-got +want):\n%s", diff)
		}
	}

	if got, ok := p.Layer(pim.HelloMessageType).(*pim.HelloMessage); ok {
		want := &pim.HelloMessage{
			Holdtime:     105,
			DRPriority:   1,
			GenerationID: 3614426332,
		}
		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.HelloMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("HelloMessage mismatch (-got +want):\n%s", diff)
		}
	}
}

func checkJoinMesssage(t *testing.T, b []byte) {
	p := gopacket.NewPacket(b, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.JoinPrune,
				Checksum: 0x2e45,
			},
		}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("PIMMessage mismatch (-got +want):\n%s", diff)
		}
	}
	if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
		want := &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.IP([]byte{169, 254, 0, 0}),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                120,
			Groups: []pim.Group{
				{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      1,
					NumPrunedSources:      0,
					MaskLength:            32,
					MulticastGroupAddress: net.IP([]byte{239, 0, 0, 1}),
					Joins: []pim.SourceAddress{
						{
							AddressFamily: 1,
							Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
							MaskLength:    32,
							EncodingType:  0,
							Address:       pim.RpAddress,
						},
					},
					Prunes: []pim.SourceAddress{},
				},
			}}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
		}
	}
}

func checkPruneMesssage(t *testing.T, b []byte) {
	p := gopacket.NewPacket(b, pim.PIMMessageType, gopacket.Default)
	if p.ErrorLayer() != nil {
		t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
	}
	if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
		want := &pim.PIMMessage{
			Header: pim.PIMHeader{
				Version:  2,
				Type:     pim.JoinPrune,
				Checksum: 0x2eb8,
			},
		}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("PIMMessage mismatch (-got +want):\n%s", diff)
		}
	}
	if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
		want := &pim.JoinPruneMessage{
			UpstreamNeighborAddress: net.IP([]byte{169, 254, 0, 0}),
			NumGroups:               1,
			Reserved:                0,
			Holdtime:                5,
			Groups: []pim.Group{
				{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      0,
					NumPrunedSources:      1,
					MaskLength:            32,
					MulticastGroupAddress: net.IP([]byte{239, 0, 0, 1}),
					Joins:                 []pim.SourceAddress{},
					Prunes: []pim.SourceAddress{
						{
							AddressFamily: 1,
							Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
							MaskLength:    32,
							EncodingType:  0,
							Address:       pim.RpAddress,
						},
					},
				},
			}}

		if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
			t.Fatalf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
		}
	}
}
