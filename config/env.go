package config

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

const (
	EnvTestnet = "testnet"
	EnvDevnet  = "devnet"
)

var (
	ErrInvalidEnvironment = fmt.Errorf("invalid environment")
)

type NetworkConfig struct {
	LedgerRPCURL               string
	ServiceabilityProgramID    solana.PublicKey
	TelemetryProgramID         solana.PublicKey
	InternetLatencyCollectorPK solana.PublicKey
}

func NetworkConfigForEnv(env string) (*NetworkConfig, error) {
	switch env {
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
		return &NetworkConfig{
			LedgerRPCURL:               TestnetLedgerRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
		}, nil
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
		return &NetworkConfig{
			LedgerRPCURL:               DevnetLedgerRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
		}, nil
	default:
		return nil, ErrInvalidEnvironment
	}
}
