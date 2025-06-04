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
				Checksum: 0x2deb,
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
			Holdtime:                210,
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
				Checksum: 0x2deb,
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
			Holdtime:                210,
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
