package runtime

import (
	"net"
	"testing"
)

func TestIsPublicIPv4(t *testing.T) {
	tests := []struct {
		name   string
		ip     string
		expect bool
	}{
		// Public IPs.
		{"public 8.8.8.8", "8.8.8.8", true},
		{"public 1.1.1.1", "1.1.1.1", true},
		{"public 203.0.113.1", "203.0.113.1", true},
		{"public 44.0.0.1", "44.0.0.1", true},

		// RFC1918.
		{"rfc1918 10.x", "10.0.0.1", false},
		{"rfc1918 10.255.x", "10.255.255.255", false},
		{"rfc1918 172.16.x", "172.16.0.1", false},
		{"rfc1918 172.31.x", "172.31.255.255", false},
		{"rfc1918 192.168.x", "192.168.1.1", false},

		// Not RFC1918 172.x.
		{"not rfc1918 172.15.x", "172.15.255.255", true},
		{"not rfc1918 172.32.x", "172.32.0.1", true},

		// CGNAT.
		{"cgnat 100.64.x", "100.64.0.1", false},
		{"cgnat 100.127.x", "100.127.255.255", false},
		{"not cgnat 100.128.x", "100.128.0.1", true},

		// Loopback.
		{"loopback", "127.0.0.1", false},
		{"loopback 127.x", "127.255.255.255", false},

		// Link-local.
		{"link-local", "169.254.0.1", false},
		{"link-local", "169.254.255.255", false},

		// Multicast.
		{"multicast", "224.0.0.1", false},
		{"multicast", "239.255.255.255", false},

		// IPv6 returns false.
		{"ipv6", "::1", false},
		{"ipv6 public", "2001:db8::1", false},

		// Nil/empty.
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			got := IsPublicIPv4(ip)
			if got != tt.expect {
				t.Errorf("IsPublicIPv4(%s) = %v, want %v", tt.ip, got, tt.expect)
			}
		})
	}
}

func TestDiscoverClientIP_Explicit(t *testing.T) {
	tests := []struct {
		name      string
		explicit  string
		expectIP  string
		expectErr bool
	}{
		{
			name:     "valid IPv4",
			explicit: "1.2.3.4",
			expectIP: "1.2.3.4",
		},
		{
			name:     "valid private IPv4 still accepted",
			explicit: "10.0.0.1",
			expectIP: "10.0.0.1",
		},
		{
			name:      "invalid IP",
			explicit:  "not-an-ip",
			expectErr: true,
		},
		{
			name:      "IPv6 rejected",
			explicit:  "2001:db8::1",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, method, err := DiscoverClientIP(tt.explicit)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got ip=%v method=%s", ip, method)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if method != "explicit flag" {
				t.Errorf("method = %q, want %q", method, "explicit flag")
			}
			if ip.String() != tt.expectIP {
				t.Errorf("ip = %s, want %s", ip.String(), tt.expectIP)
			}
		})
	}
}
