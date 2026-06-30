package pim

import (
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
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
