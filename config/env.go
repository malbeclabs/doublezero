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
)

var (
	ErrInvalidEnvironment = fmt.Errorf("invalid environment")
)

type NetworkConfig struct {
	LedgerPublicRPCURL         string
	ServiceabilityProgramID    solana.PublicKey
	TelemetryProgramID         solana.PublicKey
	InternetLatencyCollectorPK solana.PublicKey
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
			LedgerPublicRPCURL:         MainnetLedgerPublicRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
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
			LedgerPublicRPCURL:         TestnetLedgerPublicRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
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
			LedgerPublicRPCURL:         DevnetLedgerPublicRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
		}
	default:
		return nil, ErrInvalidEnvironment
	}

	ledgerRPCURL := os.Getenv("DZ_LEDGER_RPC_URL")
	if ledgerRPCURL != "" {
		config.LedgerPublicRPCURL = ledgerRPCURL
	}

	return config, nil
}
