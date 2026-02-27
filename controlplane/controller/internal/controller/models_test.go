package controller

import (
	"net"
	"net/netip"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func TestToInterface_StaleNodeSegmentIdx(t *testing.T) {
	// Simulates a loopback that was changed from vpnv4 to ipv4 but still has
	// a stale NodeSegmentIdx from the old vpnv4 config. toInterface should
	// zero it out instead of returning an error.
	iface := serviceability.Interface{
		Name:           "Loopback256",
		InterfaceType:  serviceability.InterfaceTypeLoopback,
		LoopbackType:   serviceability.LoopbackTypeIpv4,
		IpNet:          [5]uint8{172, 16, 1, 195, 32},
		NodeSegmentIdx: 90, // stale from when it was vpnv4
	}

	got, err := toInterface(iface)
	if err != nil {
		t.Fatalf("toInterface returned error for ipv4 loopback with stale NodeSegmentIdx: %v", err)
	}
	if got.NodeSegmentIdx != 0 {
		t.Errorf("NodeSegmentIdx = %d, want 0 (should be zeroed for non-vpnv4 loopback)", got.NodeSegmentIdx)
	}
	if got.LoopbackType != LoopbackTypeIpv4 {
		t.Errorf("LoopbackType = %v, want LoopbackTypeIpv4", got.LoopbackType)
	}
	if got.Ip != netip.MustParsePrefix("172.16.1.195/32") {
		t.Errorf("Ip = %v, want 172.16.1.195/32", got.Ip)
	}
}

func TestToInterface_Vpnv4KeepsNodeSegmentIdx(t *testing.T) {
	// Vpnv4 loopbacks should preserve their NodeSegmentIdx.
	iface := serviceability.Interface{
		Name:           "Loopback255",
		InterfaceType:  serviceability.InterfaceTypeLoopback,
		LoopbackType:   serviceability.LoopbackTypeVpnv4,
		IpNet:          [5]uint8{14, 14, 14, 14, 32},
		NodeSegmentIdx: 101,
	}

	got, err := toInterface(iface)
	if err != nil {
		t.Fatalf("toInterface returned error: %v", err)
	}
	if got.NodeSegmentIdx != 101 {
		t.Errorf("NodeSegmentIdx = %d, want 101", got.NodeSegmentIdx)
	}
	if got.LoopbackType != LoopbackTypeVpnv4 {
		t.Errorf("LoopbackType = %v, want LoopbackTypeVpnv4", got.LoopbackType)
	}
}

func TestIsBgpMartian(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isMartian bool
	}{
		// Martian addresses
		{"zero network", "0.0.0.1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 10.x deep", "10.255.255.255", true},
		{"shared address space", "100.64.0.1", true},
		{"loopback", "127.0.0.1", true},
		{"link-local", "169.254.1.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 172.31.x", "172.31.255.255", true},
		{"documentation TEST-NET-1", "192.0.2.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"documentation TEST-NET-2", "198.51.100.1", true},
		{"documentation TEST-NET-3", "203.0.113.1", true},
		{"multicast", "224.0.0.1", true},
		{"multicast high", "239.255.255.255", true},
		{"reserved class E", "240.0.0.1", true},
		{"broadcast", "255.255.255.255", true},

		// Non-martian addresses
		{"valid public 1", "1.1.1.1", false},
		{"valid public 8", "8.8.8.8", false},
		{"valid public 100", "100.128.0.1", false},
		{"valid public 148", "148.51.120.1", false},
		{"valid public 172.15", "172.15.255.255", false},
		{"valid public 172.32", "172.32.0.0", false},
		{"valid public 192.0.3", "192.0.3.1", false},
		{"valid public 223", "223.255.255.255", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip).To4()
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			got := isBgpMartian(ip)
			if got != tt.isMartian {
				t.Errorf("isBgpMartian(%s) = %v, want %v", tt.ip, got, tt.isMartian)
			}
		})
	}
}
