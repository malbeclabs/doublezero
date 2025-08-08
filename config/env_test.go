package config_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/config"
	"github.com/stretchr/testify/require"
)

func TestConfig_NetworkConfigForEnv(t *testing.T) {
	tests := []struct {
		env     string
		want    *config.NetworkConfig
		wantErr error
	}{
		{
			env: config.EnvTestnet,
			want: &config.NetworkConfig{
				LedgerRPCURL:               config.TestnetLedgerRPCURL,
				ServiceabilityProgramID:    solana.MustPublicKeyFromBase58(config.TestnetServiceabilityProgramID),
				TelemetryProgramID:         solana.MustPublicKeyFromBase58(config.TestnetTelemetryProgramID),
				InternetLatencyCollectorPK: solana.MustPublicKeyFromBase58(config.TestnetInternetLatencyCollectorPK),
			},
		},
		{
			env: config.EnvDevnet,
			want: &config.NetworkConfig{
				LedgerRPCURL:               config.DevnetLedgerRPCURL,
				ServiceabilityProgramID:    solana.MustPublicKeyFromBase58(config.DevnetServiceabilityProgramID),
				TelemetryProgramID:         solana.MustPublicKeyFromBase58(config.DevnetTelemetryProgramID),
				InternetLatencyCollectorPK: solana.MustPublicKeyFromBase58(config.DevnetInternetLatencyCollectorPK),
			},
		},
		{
			env:     "invalid",
			want:    nil,
			wantErr: config.ErrInvalidEnvironment,
		},
	}

	for _, test := range tests {
		t.Run(test.env, func(t *testing.T) {
			got, err := config.NetworkConfigForEnv(test.env)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.Equal(t, test.want, got)
		})
	}
}
