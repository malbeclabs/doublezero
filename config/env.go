package config

import (
	"fmt"
	"os"

	"github.com/gagliardetto/solana-go"
)

const (
	EnvMainnetBeta = "mainnet-beta"
	EnvMainnet     = "mainnet"
	EnvTestnet     = "testnet"
	EnvDevnet      = "devnet"
	EnvLocalnet    = "localnet"
)

type NetworkConfig struct {
	Moniker                       string
	LedgerPublicRPCURL            string
	ServiceabilityProgramID       solana.PublicKey
	TelemetryProgramID            solana.PublicKey
	InternetLatencyCollectorPK    solana.PublicKey
	DeviceLocalASN                uint32
	TwoZOracleURL                 string
	SolanaRPCURL                  string
	TelemetryFlowIngestURL        string
	TelemetryStateIngestURL       string
	TelemetryGNMITunnelServerAddr string
}

func NetworkConfigForEnv(env string) (*NetworkConfig, error) {
	var config *NetworkConfig
	switch env {
	case EnvMainnetBeta, EnvMainnet:
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(MainnetServiceabilityProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(MainnetTelemetryProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(MainnetInternetLatencyCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		config = &NetworkConfig{
			Moniker:                       EnvMainnetBeta,
			LedgerPublicRPCURL:            MainnetLedgerPublicRPCURL,
			ServiceabilityProgramID:       serviceabilityProgramID,
			TelemetryProgramID:            telemetryProgramID,
			InternetLatencyCollectorPK:    internetLatencyCollectorPK,
			DeviceLocalASN:                MainnetDeviceLocalASN,
			TwoZOracleURL:                 MainnetTwoZOracleURL,
			SolanaRPCURL:                  MainnetSolanaRPC,
			TelemetryFlowIngestURL:        MainnetTelemetryFlowIngestURL,
			TelemetryStateIngestURL:       MainnetTelemetryStateIngestURL,
			TelemetryGNMITunnelServerAddr: MainnetTelemetryGNMITunnelServerAddr,
		}
	case EnvTestnet:
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(TestnetServiceabilityProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(TestnetTelemetryProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(TestnetInternetLatencyCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		config = &NetworkConfig{
			Moniker:                       EnvTestnet,
			LedgerPublicRPCURL:            TestnetLedgerPublicRPCURL,
			ServiceabilityProgramID:       serviceabilityProgramID,
			TelemetryProgramID:            telemetryProgramID,
			InternetLatencyCollectorPK:    internetLatencyCollectorPK,
			DeviceLocalASN:                TestnetDeviceLocalASN,
			TwoZOracleURL:                 TestnetTwoZOracleURL,
			SolanaRPCURL:                  TestnetSolanaRPC,
			TelemetryFlowIngestURL:        TestnetTelemetryFlowIngestURL,
			TelemetryStateIngestURL:       TestnetTelemetryStateIngestURL,
			TelemetryGNMITunnelServerAddr: TestnetTelemetryGNMITunnelServerAddr,
		}
	case EnvDevnet:
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(DevnetServiceabilityProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(DevnetTelemetryProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(DevnetInternetLatencyCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		config = &NetworkConfig{
			Moniker:                       EnvDevnet,
			LedgerPublicRPCURL:            DevnetLedgerPublicRPCURL,
			ServiceabilityProgramID:       serviceabilityProgramID,
			TelemetryProgramID:            telemetryProgramID,
			InternetLatencyCollectorPK:    internetLatencyCollectorPK,
			DeviceLocalASN:                DevnetDeviceLocalASN,
			TwoZOracleURL:                 DevnetTwoZOracleURL,
			SolanaRPCURL:                  TestnetSolanaRPC,
			TelemetryFlowIngestURL:        DevnetTelemetryFlowIngestURL,
			TelemetryStateIngestURL:       DevnetTelemetryStateIngestURL,
			TelemetryGNMITunnelServerAddr: DevnetTelemetryGNMITunnelServerAddr,
		}
	case EnvLocalnet:
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(LocalnetServiceabilityProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(LocalnetTelemetryProgramID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(LocalnetInternetLatencyCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		config = &NetworkConfig{
			Moniker:                       EnvLocalnet,
			LedgerPublicRPCURL:            LocalnetLedgerPublicRPCURL,
			ServiceabilityProgramID:       serviceabilityProgramID,
			TelemetryProgramID:            telemetryProgramID,
			InternetLatencyCollectorPK:    internetLatencyCollectorPK,
			DeviceLocalASN:                LocalnetDeviceLocalASN,
			TwoZOracleURL:                 LocalnetTwoZOracleURL,
			SolanaRPCURL:                  LocalnetSolanaRPC,
			TelemetryFlowIngestURL:        LocalnetTelemetryFlowIngestURL,
			TelemetryStateIngestURL:       LocalnetTelemetryStateIngestURL,
			TelemetryGNMITunnelServerAddr: LocalnetTelemetryGNMITunnelServerAddr,
		}
	default:
		// We intentionally do not include localnet in the error message.
		return nil, fmt.Errorf("invalid environment %q, must be one of: %s, %s, %s", env, EnvMainnetBeta, EnvTestnet, EnvDevnet)
	}

	ledgerRPCURL := os.Getenv("DZ_LEDGER_RPC_URL")
	if ledgerRPCURL != "" {
		config.LedgerPublicRPCURL = ledgerRPCURL
	}

	solanaRPCURL := os.Getenv("SOLANA_RPC_URL")
	if solanaRPCURL != "" {
		config.SolanaRPCURL = solanaRPCURL
	}
	return config, nil
}
