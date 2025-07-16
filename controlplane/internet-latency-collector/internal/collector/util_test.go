package collector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsInternetRoutable(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// Valid internet routable IPv4 addresses
		{"Google DNS", "8.8.8.8", true},
		{"Cloudflare DNS", "1.1.1.1", true},
		{"AWS public IP", "52.95.115.232", true},

		// Invalid/private IPv4 addresses
		{"RFC1918 10.x.x.x", "10.0.0.1", false},
		{"RFC1918 172.16.x.x", "172.16.0.1", false},
		{"RFC1918 192.168.x.x", "192.168.1.1", false},
		{"Link-local", "169.254.1.1", false},
		{"Loopback", "127.0.0.1", false},
		{"Multicast", "224.0.0.1", false},
		{"Broadcast", "255.255.255.255", true}, // Note: current implementation doesn't filter broadcast

		// IPv6 addresses
		{"IPv6 Google DNS", "2001:4860:4860::8888", true},
		{"IPv6 Cloudflare", "2606:4700:4700::1111", true},
		{"IPv6 loopback", "::1", false},
		{"IPv6 link-local", "fe80::1", false},
		{"IPv6 multicast", "ff00::1", false},
		{"IPv6 unique local", "fc00::1", false},
		{"IPv6 unique local 2", "fd00::1", false},

		// Invalid IP addresses
		{"Invalid IP", "not.an.ip", false},
		{"Empty string", "", false},
		{"Malformed IPv4", "256.256.256.256", false},
		{"Incomplete IPv4", "192.168.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInternetRoutable(tt.ip)
			require.Equal(t, tt.expected, result, "IsInternetRoutable(%s)", tt.ip)
		})
	}
}

func TestIsInternetRoutable_EdgeCases(t *testing.T) {
	// Test edge cases for IPv4 private ranges
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// Boundary cases for 10.0.0.0/8
		{"10.0.0.0 start", "10.0.0.0", false},
		{"10.255.255.255 end", "10.255.255.255", false},
		{"9.255.255.255 before", "9.255.255.255", true},
		{"11.0.0.0 after", "11.0.0.0", true},

		// Boundary cases for 172.16.0.0/12
		{"172.16.0.0 start", "172.16.0.0", false},
		{"172.31.255.255 end", "172.31.255.255", false},
		{"172.15.255.255 before", "172.15.255.255", true},
		{"172.32.0.0 after", "172.32.0.0", true},

		// Boundary cases for 192.168.0.0/16
		{"192.168.0.0 start", "192.168.0.0", false},
		{"192.168.255.255 end", "192.168.255.255", false},
		{"192.167.255.255 before", "192.167.255.255", true},
		{"192.169.0.0 after", "192.169.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInternetRoutable(tt.ip)
			require.Equal(t, tt.expected, result, "IsInternetRoutable(%s)", tt.ip)
		})
	}
}
