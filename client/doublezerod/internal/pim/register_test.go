package pim

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/ipv4"
)

func TestConstructRegisterMessage(t *testing.T) {
	innerSrc := net.IPv4(148, 51, 122, 203)
	group := net.IPv4(233, 84, 178, 5)
	payload := []byte{0x44, 0x5A, 0x00, 0x01}

	buf, err := constructRegisterMessage(innerSrc, group, 5765, payload)
	if err != nil {
		t.Fatalf("constructRegisterMessage: %v", err)
	}
	b := buf.Bytes()

	// PIM common header: version 2, type Register (0x01) => first byte 0x21.
	if b[0] != 0x21 {
		t.Fatalf("pim header byte0 = 0x%02x, want 0x21", b[0])
	}
	// Register flags word (bytes 4..7): Border=0, Null=0.
	if b[4] != 0x00 {
		t.Fatalf("register flags high byte = 0x%02x, want 0x00", b[4])
	}
	// Bytes 8.. are the encapsulated IPv4/UDP datagram.
	pkt := gopacket.NewPacket(b[8:], layers.LayerTypeIPv4, gopacket.Default)
	ip, ok := pkt.Layer(layers.LayerTypeIPv4).(*layers.IPv4)
	if !ok {
		t.Fatal("no encapsulated IPv4 layer")
	}
	if !ip.SrcIP.Equal(innerSrc) || !ip.DstIP.Equal(group) {
		t.Fatalf("inner IP src/dst = %s/%s, want %s/%s", ip.SrcIP, ip.DstIP, innerSrc, group)
	}
	if ip.Protocol != layers.IPProtocolUDP {
		t.Fatalf("inner proto = %v, want UDP", ip.Protocol)
	}
	udp, ok := pkt.Layer(layers.LayerTypeUDP).(*layers.UDP)
	if !ok {
		t.Fatal("no encapsulated UDP layer")
	}
	if udp.DstPort != 5765 {
		t.Fatalf("inner UDP dport = %d, want 5765", udp.DstPort)
	}
}

type mockRawConn struct {
	mu    sync.Mutex
	calls []writeCall
}

type writeCall struct {
	h  *ipv4.Header
	b  []byte
	cm *ipv4.ControlMessage
}

func (m *mockRawConn) WriteTo(h *ipv4.Header, b []byte, cm *ipv4.ControlMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	m.calls = append(m.calls, writeCall{h: h, b: cp, cm: cm})
	return nil
}
func (m *mockRawConn) Close() error                                    { return nil }
func (m *mockRawConn) SetMulticastInterface(*net.Interface) error      { return nil }
func (m *mockRawConn) SetControlMessage(ipv4.ControlFlags, bool) error { return nil }

func TestRegisterSenderSendsRegisterToRP(t *testing.T) {
	mock := &mockRawConn{}
	s := NewRegisterSender()
	s.conn = mock
	s.innerSrc = net.IPv4(148, 51, 122, 203)
	s.srcOverlay = net.IPv4(169, 254, 4, 58)
	s.rp = RpAddress
	s.dport = 5765
	s.payload = []byte{0x44, 0x5A, 0x00, 0x01}

	intf := &net.Interface{Index: 7, Name: "doublezero1"}
	group := net.IPv4(233, 84, 178, 5)

	if err := s.sendRegister(intf, group); err != nil {
		t.Fatalf("sendRegister: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("got %d writes, want 1", len(mock.calls))
	}
	c := mock.calls[0]
	if !c.h.Dst.Equal(RpAddress) {
		t.Fatalf("dst = %s, want RP %s", c.h.Dst, RpAddress)
	}
	if c.h.Protocol != 103 {
		t.Fatalf("proto = %d, want 103", c.h.Protocol)
	}
	if c.cm.IfIndex != 7 {
		t.Fatalf("ifindex = %d, want 7 (egress pinned to tunnel)", c.cm.IfIndex)
	}
	if c.b[0] != 0x21 {
		t.Fatalf("pim byte0 = 0x%02x, want 0x21", c.b[0])
	}
	// Checksum is computed over the first 8 bytes only (RFC 7761 4.9.1),
	// with the checksum field itself zeroed during computation.
	hdr := make([]byte, 8)
	copy(hdr, c.b[:8])
	hdr[2], hdr[3] = 0, 0
	want := Checksum(hdr)
	if got := binary.BigEndian.Uint16(c.b[2:4]); got != want {
		t.Fatalf("pim checksum = 0x%04x, want 0x%04x", got, want)
	}
}
