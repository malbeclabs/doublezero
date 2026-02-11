package geoprobe

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeAddress_Validate(t *testing.T) {
	tests := []struct {
		name    string
		addr    ProbeAddress
		wantErr string
	}{
		{
			name: "Valid address",
			addr: ProbeAddress{
				Host: "probe1.example.com",
				Port: 10000,
			},
			wantErr: "",
		},
		{
			name: "Empty host",
			addr: ProbeAddress{
				Host: "",
				Port: 10000,
			},
			wantErr: "host cannot be empty",
		},
		{
			name: "Zero port",
			addr: ProbeAddress{
				Host: "probe1.example.com",
				Port: 0,
			},
			wantErr: "port cannot be zero",
		},
		{
			name: "Valid IP address",
			addr: ProbeAddress{
				Host: "192.0.2.1",
				Port: 12345,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.addr.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"10.0.0.1", "10.0.0.1", true},
		{"10.255.255.255", "10.255.255.255", true},
		{"172.16.0.1", "172.16.0.1", true},
		{"172.31.255.255", "172.31.255.255", true},
		{"192.168.0.1", "192.168.0.1", true},
		{"192.168.255.255", "192.168.255.255", true},
		{"8.8.8.8", "8.8.8.8", false},
		{"1.1.1.1", "1.1.1.1", false},
		{"fc00::1", "fc00::1", true},
		{"fd00::1", "fd00::1", true},
		{"2001:4860:4860::8888", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %s", tt.ip)
			result := isPrivateIP(ip)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsLoopback(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"127.0.0.1", "127.0.0.1", true},
		{"127.255.255.255", "127.255.255.255", true},
		{"::1", "::1", true},
		{"8.8.8.8", "8.8.8.8", false},
		{"10.0.0.1", "10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %s", tt.ip)
			result := isLoopback(ip)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsLinkLocal(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"169.254.0.1", "169.254.0.1", true},
		{"169.254.255.255", "169.254.255.255", true},
		{"fe80::1", "fe80::1", true},
		{"8.8.8.8", "8.8.8.8", false},
		{"10.0.0.1", "10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %s", tt.ip)
			result := isLinkLocal(ip)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsReservedIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"0.0.0.0", "0.0.0.0", true},
		{"0.255.255.255", "0.255.255.255", true},
		{"240.0.0.1", "240.0.0.1", true},
		{"255.255.255.255", "255.255.255.255", true},
		{"224.0.0.1", "224.0.0.1", true},
		{"239.255.255.255", "239.255.255.255", true},
		{"8.8.8.8", "8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %s", tt.ip)
			result := isReservedIP(ip)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"8.8.8.8 public", "8.8.8.8", true},
		{"1.1.1.1 public", "1.1.1.1", true},
		{"2001:4860:4860::8888 public", "2001:4860:4860::8888", true},
		{"10.0.0.1 private", "10.0.0.1", false},
		{"192.168.1.1 private", "192.168.1.1", false},
		{"172.16.0.1 private", "172.16.0.1", false},
		{"127.0.0.1 loopback", "127.0.0.1", false},
		{"::1 loopback", "::1", false},
		{"169.254.1.1 link-local", "169.254.1.1", false},
		{"fe80::1 link-local", "fe80::1", false},
		{"0.0.0.0 unspecified", "0.0.0.0", false},
		{"240.0.0.1 reserved", "240.0.0.1", false},
		{"224.0.0.1 multicast", "224.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %s", tt.ip)
			result := isPublicIP(ip)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestProbeAddress_ValidatePublic(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		addr       ProbeAddress
		wantErrMsg string
	}{
		{
			name: "Public DNS - google.com",
			addr: ProbeAddress{
				Host: "google.com",
				Port: 443,
			},
			wantErrMsg: "",
		},
		{
			name: "Public DNS - cloudflare.com",
			addr: ProbeAddress{
				Host: "cloudflare.com",
				Port: 443,
			},
			wantErrMsg: "",
		},
		{
			name: "Public IP - 8.8.8.8",
			addr: ProbeAddress{
				Host: "8.8.8.8",
				Port: 53,
			},
			wantErrMsg: "",
		},
		{
			name: "Public IP - 1.1.1.1",
			addr: ProbeAddress{
				Host: "1.1.1.1",
				Port: 53,
			},
			wantErrMsg: "",
		},
		{
			name: "Private IP - 10.0.0.1",
			addr: ProbeAddress{
				Host: "10.0.0.1",
				Port: 8080,
			},
			wantErrMsg: "private IP address",
		},
		{
			name: "Private IP - 192.168.1.1",
			addr: ProbeAddress{
				Host: "192.168.1.1",
				Port: 8080,
			},
			wantErrMsg: "private IP address",
		},
		{
			name: "Private IP - 172.16.0.1",
			addr: ProbeAddress{
				Host: "172.16.0.1",
				Port: 8080,
			},
			wantErrMsg: "private IP address",
		},
		{
			name: "Loopback - 127.0.0.1",
			addr: ProbeAddress{
				Host: "127.0.0.1",
				Port: 8080,
			},
			wantErrMsg: "loopback IP address",
		},
		{
			name: "Loopback - localhost",
			addr: ProbeAddress{
				Host: "localhost",
				Port: 8080,
			},
			wantErrMsg: "loopback IP address",
		},
		{
			name: "Link-local - 169.254.1.1",
			addr: ProbeAddress{
				Host: "169.254.1.1",
				Port: 8080,
			},
			wantErrMsg: "link-local IP address",
		},
		{
			name: "Reserved - 0.0.0.0",
			addr: ProbeAddress{
				Host: "0.0.0.0",
				Port: 8080,
			},
			wantErrMsg: "reserved IP address",
		},
		{
			name: "Multicast - 224.0.0.1",
			addr: ProbeAddress{
				Host: "224.0.0.1",
				Port: 8080,
			},
			wantErrMsg: "reserved IP address",
		},
		{
			name: "Empty host",
			addr: ProbeAddress{
				Host: "",
				Port: 8080,
			},
			wantErrMsg: "host cannot be empty",
		},
		{
			name: "Zero port",
			addr: ProbeAddress{
				Host: "8.8.8.8",
				Port: 0,
			},
			wantErrMsg: "port cannot be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.addr.ValidatePublic(ctx)
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestParseProbeAddressesWithContext(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantErrMsg string
	}{
		{
			name:      "Empty string",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "Single public IP",
			input:     "8.8.8.8:53",
			wantCount: 1,
		},
		{
			name:      "Multiple public IPs",
			input:     "8.8.8.8:53,1.1.1.1:53",
			wantCount: 2,
		},
		{
			name:       "Private IP rejected",
			input:      "10.0.0.1:8080",
			wantErrMsg: "private IP address",
		},
		{
			name:       "Loopback rejected",
			input:      "127.0.0.1:8080",
			wantErrMsg: "loopback IP address",
		},
		{
			name:       "Link-local rejected",
			input:      "169.254.1.1:8080",
			wantErrMsg: "link-local IP address",
		},
		{
			name:       "Mixed public and private - rejected",
			input:      "8.8.8.8:53,10.0.0.1:8080",
			wantErrMsg: "private IP address",
		},
		{
			name:       "Invalid format",
			input:      "invalid",
			wantErrMsg: "invalid probe address",
		},
		{
			name:       "Invalid port",
			input:      "8.8.8.8:invalid",
			wantErrMsg: "invalid port",
		},
		{
			name:      "Deduplication",
			input:     "8.8.8.8:53,8.8.8.8:53",
			wantCount: 1,
		},
		{
			name:      "Whitespace handling",
			input:     " 8.8.8.8:53 , 1.1.1.1:53 ",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs, err := ParseProbeAddressesWithContext(ctx, tt.input)
			if tt.wantErrMsg == "" {
				require.NoError(t, err)
				require.Len(t, addrs, tt.wantCount)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrMsg)
			}
		})
	}
}
