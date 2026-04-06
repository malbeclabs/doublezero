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
