package config

import (
	"fmt"
	"os"
)

const (
	SolanaEnvMainnetBeta = "mainnet-beta"
	SolanaEnvTestnet     = "testnet"
	SolanaEnvDevnet      = "devnet"
	SolanaEnvLocalnet    = "localnet"
)

type SolanaNetworkConfig struct {
	RPCURL string
}

func SolanaNetworkConfigForEnv(env string) (*SolanaNetworkConfig, error) {
	var config *SolanaNetworkConfig
	switch env {
	case SolanaEnvMainnetBeta:
		config = &SolanaNetworkConfig{
			RPCURL: MainnetSolanaRPC,
		}
	case SolanaEnvTestnet:
		config = &SolanaNetworkConfig{
			RPCURL: TestnetSolanaRPC,
		}
	case SolanaEnvDevnet:
		config = &SolanaNetworkConfig{
			RPCURL: TestnetSolanaRPC,
		}
	case SolanaEnvLocalnet:
		config = &SolanaNetworkConfig{
			RPCURL: TestnetSolanaRPC,
		}
	default:
		// We intentionally do not include localnet in the error message.
		return nil, fmt.Errorf("invalid environment %q, must be one of: %s, %s, %s", env, SolanaEnvMainnetBeta, SolanaEnvTestnet, SolanaEnvDevnet)
	}

	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL != "" {
		config.RPCURL = rpcURL
	}
	return config, nil
}
