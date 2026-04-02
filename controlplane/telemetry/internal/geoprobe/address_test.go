package geoprobe

import (
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
			name: "Valid IP address",
			addr: ProbeAddress{
				Host:      "192.0.2.1",
				Port:      10000,
				TWAMPPort: 8925,
			},
			wantErr: "",
		},
		{
			name: "Empty host",
			addr: ProbeAddress{
				Host:      "",
				Port:      10000,
				TWAMPPort: 8925,
			},
			wantErr: "host cannot be empty",
		},
		{
			name: "Zero port",
			addr: ProbeAddress{
				Host:      "192.0.2.1",
				Port:      0,
				TWAMPPort: 8925,
			},
			wantErr: "port cannot be zero",
		},
		{
			name: "Another valid IP",
			addr: ProbeAddress{
				Host:      "8.8.8.8",
				Port:      12345,
				TWAMPPort: 8925,
			},
			wantErr: "",
		},
		{
			name: "Hostname rejected",
			addr: ProbeAddress{
				Host:      "probe1.example.com",
				Port:      10000,
				TWAMPPort: 8925,
			},
			wantErr: "host must be a valid IP address",
		},
		{
			name: "Zero twamp port",
			addr: ProbeAddress{
				Host:      "192.0.2.1",
				Port:      10000,
				TWAMPPort: 0,
			},
			wantErr: "twamp port cannot be zero",
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

func TestProbeAddress_ValidateScope(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"public IP", "8.8.8.8", false},
		{"another public IP", "44.0.0.1", false},
		{"loopback", "127.0.0.1", true},
		{"private 10/8", "10.0.0.1", true},
		{"private 172.16/12", "172.16.0.1", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"multicast", "224.0.0.1", true},
		{"unspecified", "0.0.0.0", true},
		{"CGN 100.64/10", "100.64.0.1", true},
		{"CGN upper bound", "100.127.255.254", true},
		{"benchmarking 198.18/15", "198.18.0.1", true},
		{"TEST-NET-1", "192.0.2.1", true},
		{"TEST-NET-2", "198.51.100.1", true},
		{"TEST-NET-3", "203.0.113.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := ProbeAddress{Host: tt.host, Port: 9000, TWAMPPort: 8925}
			err := addr.ValidateScope()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "not a public unicast address")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParseProbeAddresses(t *testing.T) {
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
			name:      "Single host only",
			input:     "8.8.8.8",
			wantCount: 1,
		},
		{
			name:      "Multiple hosts only",
			input:     "8.8.8.8,1.1.1.1",
			wantCount: 2,
		},
		{
			name:       "Invalid host",
			input:      "invalid",
			wantErrMsg: "invalid probe address",
		},
		{
			name:       "Two-field format rejected",
			input:      "8.8.8.8:53",
			wantErrMsg: "invalid probe address",
		},
		{
			name:      "Deduplication",
			input:     "8.8.8.8,8.8.8.8",
			wantCount: 1,
		},
		{
			name:      "Whitespace handling",
			input:     " 8.8.8.8 , 1.1.1.1 ",
			wantCount: 2,
		},
		{
			name:      "Three-field format",
			input:     "8.8.8.8:53:8925",
			wantCount: 1,
		},
		{
			name:      "Mixed one and three field formats",
			input:     "8.8.8.8,1.1.1.1:53:9000",
			wantCount: 2,
		},
		{
			name:       "Invalid twamp port",
			input:      "8.8.8.8:53:invalid",
			wantErrMsg: "invalid twamp port",
		},
		{
			name:       "Too many fields",
			input:      "8.8.8.8:53:8925:extra",
			wantErrMsg: "invalid probe address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs, err := ParseProbeAddresses(tt.input)
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

func TestProbeAddress_ValidateICMP(t *testing.T) {
	tests := []struct {
		name    string
		addr    ProbeAddress
		wantErr string
	}{
		{
			name:    "Valid ICMP address",
			addr:    ProbeAddress{Host: "8.8.8.8", Port: 9000, TWAMPPort: 0},
			wantErr: "",
		},
		{
			name:    "Empty host",
			addr:    ProbeAddress{Host: "", Port: 9000, TWAMPPort: 0},
			wantErr: "host cannot be empty",
		},
		{
			name:    "Invalid host",
			addr:    ProbeAddress{Host: "notanip", Port: 9000, TWAMPPort: 0},
			wantErr: "host must be a valid IP address",
		},
		{
			name:    "Zero port",
			addr:    ProbeAddress{Host: "8.8.8.8", Port: 0, TWAMPPort: 0},
			wantErr: "port cannot be zero",
		},
		{
			name:    "TWAMPPort ignored",
			addr:    ProbeAddress{Host: "8.8.8.8", Port: 9000, TWAMPPort: 8925},
			wantErr: "",
		},
		{
			name:    "IPv6 rejected",
			addr:    ProbeAddress{Host: "::1", Port: 9000, TWAMPPort: 0},
			wantErr: "must be an IPv4 address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.addr.ValidateICMP()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestParseICMPProbeAddresses(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []ProbeAddress
		wantErr string
	}{
		{
			name:  "Empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "Single host:port",
			input: "8.8.8.8:9000",
			want:  []ProbeAddress{{Host: "8.8.8.8", Port: 9000, TWAMPPort: 0}},
		},
		{
			name:  "Multiple entries",
			input: "8.8.8.8:9000,1.1.1.1:9001",
			want: []ProbeAddress{
				{Host: "8.8.8.8", Port: 9000, TWAMPPort: 0},
				{Host: "1.1.1.1", Port: 9001, TWAMPPort: 0},
			},
		},
		{
			name:  "Deduplicate",
			input: "8.8.8.8:9000,8.8.8.8:9000",
			want:  []ProbeAddress{{Host: "8.8.8.8", Port: 9000, TWAMPPort: 0}},
		},
		{
			name:    "Missing port",
			input:   "8.8.8.8",
			wantErr: "expected host:offset_port",
		},
		{
			name:    "Three fields rejected",
			input:   "8.8.8.8:9000:8925",
			wantErr: "expected host:offset_port",
		},
		{
			name:    "Invalid IP",
			input:   "notanip:9000",
			wantErr: "expected host:offset_port",
		},
		{
			name:    "Zero port",
			input:   "8.8.8.8:0",
			wantErr: "invalid port 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseICMPProbeAddresses(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseProbeAddresses_Values(t *testing.T) {
	t.Run("host-only format uses default ports", func(t *testing.T) {
		addrs, err := ParseProbeAddresses("10.0.0.1")
		require.NoError(t, err)
		require.Len(t, addrs, 1)
		require.Equal(t, "10.0.0.1", addrs[0].Host)
		require.Equal(t, uint16(8923), addrs[0].Port)
		require.Equal(t, uint16(8925), addrs[0].TWAMPPort)
	})

	t.Run("three-field format uses explicit ports", func(t *testing.T) {
		addrs, err := ParseProbeAddresses("10.0.0.1:8923:9000")
		require.NoError(t, err)
		require.Len(t, addrs, 1)
		require.Equal(t, "10.0.0.1", addrs[0].Host)
		require.Equal(t, uint16(8923), addrs[0].Port)
		require.Equal(t, uint16(9000), addrs[0].TWAMPPort)
	})
}
