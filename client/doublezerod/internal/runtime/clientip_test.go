package runtime

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
		{"public 44.0.0.1", "44.0.0.1", true},
		{"public 203.0.114.1", "203.0.114.1", true},

		// "This" network (0.0.0.0/8).
		{"this network", "0.0.0.1", false},

		// RFC 1918.
		{"rfc1918 10.x", "10.0.0.1", false},
		{"rfc1918 10.255.x", "10.255.255.255", false},
		{"rfc1918 172.16.x", "172.16.0.1", false},
		{"rfc1918 172.31.x", "172.31.255.255", false},
		{"rfc1918 192.168.x", "192.168.1.1", false},

		// Not RFC 1918 172.x.
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
		{"link-local low", "169.254.0.1", false},
		{"link-local high", "169.254.255.255", false},

		// IETF protocol assignments (192.0.0.0/24).
		{"ietf protocol assignments", "192.0.0.1", false},

		// Documentation / TEST-NET.
		{"test-net-1", "192.0.2.1", false},
		{"test-net-2", "198.51.100.1", false},
		{"test-net-3", "203.0.113.1", false},

		// Multicast.
		{"multicast low", "224.0.0.1", false},
		{"multicast high", "239.255.255.255", false},

		// Reserved.
		{"reserved", "240.0.0.1", false},
		{"broadcast", "255.255.255.255", false},

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

func withTestExternalURL(t *testing.T, url string) {
	t.Helper()
	orig := externalDiscoveryURL
	externalDiscoveryURL = url
	t.Cleanup(func() { externalDiscoveryURL = orig })
}

func TestDiscoverFromExternal_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "93.184.216.34")
	}))
	defer srv.Close()
	withTestExternalURL(t, srv.URL)

	ip, err := discoverFromExternal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.String() != "93.184.216.34" {
		t.Errorf("ip = %s, want 93.184.216.34", ip)
	}
}

func TestDiscoverFromExternal_RetriesOnTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "93.184.216.34")
	}))
	defer srv.Close()
	withTestExternalURL(t, srv.URL)

	// Override backoff to avoid slow tests.
	origBackoff := externalDiscoveryBackoff
	externalDiscoveryBackoff = 0
	t.Cleanup(func() { externalDiscoveryBackoff = origBackoff })

	ip, err := discoverFromExternal()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.String() != "93.184.216.34" {
		t.Errorf("ip = %s, want 93.184.216.34", ip)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDiscoverFromExternal_AllRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	withTestExternalURL(t, srv.URL)

	origBackoff := externalDiscoveryBackoff
	externalDiscoveryBackoff = 0
	t.Cleanup(func() { externalDiscoveryBackoff = origBackoff })

	_, err := discoverFromExternal()
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}
