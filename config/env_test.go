package config_test

import (
	"fmt"
	"os"
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
			env: config.EnvMainnet,
			want: &config.NetworkConfig{
				Moniker:                       config.EnvMainnetBeta,
				LedgerPublicRPCURL:            config.MainnetLedgerPublicRPCURL,
				ServiceabilityProgramID:       solana.MustPublicKeyFromBase58(config.MainnetServiceabilityProgramID),
				TelemetryProgramID:            solana.MustPublicKeyFromBase58(config.MainnetTelemetryProgramID),
				InternetLatencyCollectorPK:    solana.MustPublicKeyFromBase58(config.MainnetInternetLatencyCollectorPK),
				DeviceLocalASN:                config.MainnetDeviceLocalASN,
				TwoZOracleURL:                 config.MainnetTwoZOracleURL,
				SolanaRPCURL:                  config.MainnetSolanaRPC,
				TelemetryFlowIngestURL:        config.MainnetTelemetryFlowIngestURL,
				TelemetryStateIngestURL:       config.MainnetTelemetryStateIngestURL,
				TelemetryGNMITunnelServerAddr: config.MainnetTelemetryGNMITunnelServerAddr,
			},
		},
		{
			env: config.EnvMainnetBeta,
			want: &config.NetworkConfig{
				Moniker:                       config.EnvMainnetBeta,
				LedgerPublicRPCURL:            config.MainnetLedgerPublicRPCURL,
				ServiceabilityProgramID:       solana.MustPublicKeyFromBase58(config.MainnetServiceabilityProgramID),
				TelemetryProgramID:            solana.MustPublicKeyFromBase58(config.MainnetTelemetryProgramID),
				InternetLatencyCollectorPK:    solana.MustPublicKeyFromBase58(config.MainnetInternetLatencyCollectorPK),
				DeviceLocalASN:                config.MainnetDeviceLocalASN,
				TwoZOracleURL:                 config.MainnetTwoZOracleURL,
				SolanaRPCURL:                  config.MainnetSolanaRPC,
				TelemetryFlowIngestURL:        config.MainnetTelemetryFlowIngestURL,
				TelemetryStateIngestURL:       config.MainnetTelemetryStateIngestURL,
				TelemetryGNMITunnelServerAddr: config.MainnetTelemetryGNMITunnelServerAddr,
			},
		},
		{
			env: config.EnvTestnet,
			want: &config.NetworkConfig{
				Moniker:                       config.EnvTestnet,
				LedgerPublicRPCURL:            config.TestnetLedgerPublicRPCURL,
				ServiceabilityProgramID:       solana.MustPublicKeyFromBase58(config.TestnetServiceabilityProgramID),
				TelemetryProgramID:            solana.MustPublicKeyFromBase58(config.TestnetTelemetryProgramID),
				InternetLatencyCollectorPK:    solana.MustPublicKeyFromBase58(config.TestnetInternetLatencyCollectorPK),
				DeviceLocalASN:                config.TestnetDeviceLocalASN,
				TwoZOracleURL:                 config.TestnetTwoZOracleURL,
				SolanaRPCURL:                  config.TestnetSolanaRPC,
				TelemetryFlowIngestURL:        config.TestnetTelemetryFlowIngestURL,
				TelemetryStateIngestURL:       config.TestnetTelemetryStateIngestURL,
				TelemetryGNMITunnelServerAddr: config.TestnetTelemetryGNMITunnelServerAddr,
			},
		},
		{
			env: config.EnvDevnet,
			want: &config.NetworkConfig{
				Moniker:                       config.EnvDevnet,
				LedgerPublicRPCURL:            config.DevnetLedgerPublicRPCURL,
				ServiceabilityProgramID:       solana.MustPublicKeyFromBase58(config.DevnetServiceabilityProgramID),
				TelemetryProgramID:            solana.MustPublicKeyFromBase58(config.DevnetTelemetryProgramID),
				InternetLatencyCollectorPK:    solana.MustPublicKeyFromBase58(config.DevnetInternetLatencyCollectorPK),
				DeviceLocalASN:                config.DevnetDeviceLocalASN,
				TwoZOracleURL:                 config.DevnetTwoZOracleURL,
				SolanaRPCURL:                  config.TestnetSolanaRPC,
				TelemetryFlowIngestURL:        config.DevnetTelemetryFlowIngestURL,
				TelemetryStateIngestURL:       config.DevnetTelemetryStateIngestURL,
				TelemetryGNMITunnelServerAddr: config.DevnetTelemetryGNMITunnelServerAddr,
			},
		},
		{
			env:     "invalid",
			want:    nil,
			wantErr: fmt.Errorf("invalid environment %q, must be one of: %s, %s, %s", "invalid", config.EnvMainnetBeta, config.EnvTestnet, config.EnvDevnet),
		},
	}

	for _, test := range tests {
		t.Run(test.env, func(t *testing.T) {
			got, err := config.NetworkConfigForEnv(test.env)
			if test.wantErr != nil {
				require.Equal(t, test.wantErr.Error(), err.Error())
				return
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestConfig_NetworkConfigForEnv_RPCURLOverrideFromEnvVars(t *testing.T) {
	os.Setenv("DZ_LEDGER_RPC_URL", "https://other-rpc-url.com")
	os.Setenv("DZ_LEDGER_WS_RPC_URL", "wss://other-ws-rpc-url.com")
	got, err := config.NetworkConfigForEnv(config.EnvMainnet)
	require.NoError(t, err)
	require.Equal(t, "https://other-rpc-url.com", got.LedgerPublicRPCURL)
}
