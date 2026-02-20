package dz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Status
		wantErr string
	}{
		{
			name:    "empty array",
			input:   `[]`,
			wantErr: "no status response",
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: "failed to unmarshal",
		},
		{
			name: "single entry",
			input: `[{
				"current_device": "frankry",
				"metro": "Frankfurt",
				"network": "mainnet-beta",
				"response": {"user_type": "IBRLWithAllocatedIP"}
			}]`,
			want: &Status{
				CurrentDeviceCode: "frankry",
				MetroName:         "Frankfurt",
				NetworkSlug:       "mainnet-beta",
			},
		},
		{
			name: "multiple entries selects non-multicast",
			input: `[
				{
					"current_device": "frankry",
					"metro": "Frankfurt",
					"network": "mainnet-beta",
					"response": {"user_type": "IBRLWithAllocatedIP"}
				},
				{
					"current_device": "fr2-dzx-001",
					"metro": "Frankfurt",
					"network": "mainnet-beta",
					"response": {"user_type": "Multicast"}
				}
			]`,
			want: &Status{
				CurrentDeviceCode: "frankry",
				MetroName:         "Frankfurt",
				NetworkSlug:       "mainnet-beta",
			},
		},
		{
			name: "multicast first then non-multicast",
			input: `[
				{
					"current_device": "fr2-dzx-001",
					"metro": "Frankfurt",
					"network": "mainnet-beta",
					"response": {"user_type": "Multicast"}
				},
				{
					"current_device": "frankry",
					"metro": "Frankfurt",
					"network": "mainnet-beta",
					"response": {"user_type": "IBRLWithAllocatedIP"}
				}
			]`,
			want: &Status{
				CurrentDeviceCode: "frankry",
				MetroName:         "Frankfurt",
				NetworkSlug:       "mainnet-beta",
			},
		},
		{
			name: "all multicast falls back to first",
			input: `[
				{
					"current_device": "fr2-dzx-001",
					"metro": "Frankfurt",
					"network": "mainnet-beta",
					"response": {"user_type": "Multicast"}
				},
				{
					"current_device": "fr3-dzx-002",
					"metro": "Paris",
					"network": "mainnet-beta",
					"response": {"user_type": "Multicast"}
				}
			]`,
			want: &Status{
				CurrentDeviceCode: "fr2-dzx-001",
				MetroName:         "Frankfurt",
				NetworkSlug:       "mainnet-beta",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus([]byte(tt.input))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
