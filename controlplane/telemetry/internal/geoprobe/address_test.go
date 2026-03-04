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
				Host: "192.0.2.1",
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
				Host: "192.0.2.1",
				Port: 0,
			},
			wantErr: "port cannot be zero",
		},
		{
			name: "Another valid IP",
			addr: ProbeAddress{
				Host: "8.8.8.8",
				Port: 12345,
			},
			wantErr: "",
		},
		{
			name: "Hostname rejected",
			addr: ProbeAddress{
				Host: "probe1.example.com",
				Port: 10000,
			},
			wantErr: "host must be a valid IP address",
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
