package config_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/config"
	inetlatencyconfig "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/pkg/config"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
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
				LedgerRPCURL:               dzsdk.DZ_LEDGER_RPC_URL,
				ServiceabilityProgramID:    solana.MustPublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET),
				TelemetryProgramID:         solana.MustPublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_TESTNET),
				InternetLatencyCollectorPK: solana.MustPublicKeyFromBase58(inetlatencyconfig.TestnetCollectorPK),
			},
		},
		{
			env: config.EnvDevnet,
			want: &config.NetworkConfig{
				LedgerRPCURL:               dzsdk.DZ_LEDGER_RPC_URL,
				ServiceabilityProgramID:    solana.MustPublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET),
				TelemetryProgramID:         solana.MustPublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_DEVNET),
				InternetLatencyCollectorPK: solana.MustPublicKeyFromBase58(inetlatencyconfig.DevnetCollectorPK),
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
