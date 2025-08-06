package config

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	inetlatencyconfig "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/pkg/config"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
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
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_TESTNET)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(inetlatencyconfig.TestnetCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		return &NetworkConfig{
			LedgerRPCURL:               dzsdk.DZ_LEDGER_RPC_URL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
		}, nil
	case EnvDevnet:
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET)
		if err != nil {
			return nil, fmt.Errorf("failed to parse serviceability program ID: %w", err)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_DEVNET)
		if err != nil {
			return nil, fmt.Errorf("failed to parse telemetry program ID: %w", err)
		}
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(inetlatencyconfig.DevnetCollectorPK)
		if err != nil {
			return nil, fmt.Errorf("failed to parse internet latency collector oracle agent PK: %w", err)
		}
		return &NetworkConfig{
			LedgerRPCURL:               dzsdk.DZ_LEDGER_RPC_URL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
		}, nil
	default:
		return nil, ErrInvalidEnvironment
	}
}
