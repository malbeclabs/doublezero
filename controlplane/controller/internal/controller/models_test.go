package controller

import (
	"net"
	"testing"
)

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
		{"valid public 147", "147.51.126.1", false},
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
