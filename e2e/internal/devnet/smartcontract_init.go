package devnet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
)

// IsSmartContractInitialized checks if the smart contract is initialized by checking for the presence
// of the default global configuration of any value.
func (dn *Devnet) IsSmartContractInitialized(ctx context.Context) (bool, error) {
	output, err := docker.Exec(ctx, dn.dockerClient, dn.Manager.ContainerID, []string{"bash", "-c", `
		set -xe

		doublezero global-config get
	`}, docker.NoPrintOnError())
	if err != nil {
		if strings.Contains(string(output), "AccountNotFound") {
			return false, nil
		}
		fmt.Println(string(output))
		return false, fmt.Errorf("failed to check if smart contract is initialized: %w", err)
	}

	// This is a simple, naive check to see if the smart contract is initialized, by checking
	// for the presence of the default global configuration of any value.
	var count int
	for line := range strings.SplitSeq(string(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "asn") {
			continue
		}
		count++
	}

	return count > 0, nil
}

// InitSmartContractIfNotInitialized initializes the smart contract if it's not already initialized.
func (dn *Devnet) InitSmartContractIfNotInitialized(ctx context.Context) (bool, error) {
	initialized, err := dn.IsSmartContractInitialized(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if smart contract is initialized: %w", err)
	}

	if initialized {
		dn.log.Info("--> Smart contract already initialized")
		return true, nil
	}

	return false, dn.InitSmartContract(ctx)
}

// InitSmartContract initializes the smart contract using the manager container.
//
// Perform the global state initialization via `doublezero init`, and then populate global config,
// location, and exchange information onchain.
func (dn *Devnet) InitSmartContract(ctx context.Context) error {
	dn.log.Info("==> Initializing smart contract")

	_, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		# Fund the manager account with some SOL if the balance is 0.
		echo "==> Checking manager account balance"
		solana balance
		if solana balance | grep -q "^0 SOL$"; then
			echo "==> Manager account balance is 0 SOL, funding with 100 SOL"
			solana airdrop 100 $(solana-keygen pubkey)
		fi
		echo

		# Wait for the serviceability program to be ready.
		echo "==> Waiting for serviceability program to be ready"
		solana program show ` + dn.Manager.ServiceabilityProgramID + `
		solana account ` + dn.Manager.ServiceabilityProgramID + `

		echo "==> Initializing smart contract"
		doublezero --version
		doublezero init
		echo

		doublezero global-config authority set --activator-authority me --sentinel-authority me
		echo

		# Populate global configuration onchain.
		echo "==> Populating global configuration onchain"
		echo doublezero global-config set --local-asn 65000 --remote-asn 65342 --device-tunnel-block ` + dn.Spec.DeviceTunnelNet + ` --user-tunnel-block 169.254.0.0/16 --multicastgroup-block 233.84.178.0/24
		doublezero global-config set --local-asn 65000 --remote-asn 65342 --device-tunnel-block ` + dn.Spec.DeviceTunnelNet + ` --user-tunnel-block 169.254.0.0/16 --multicastgroup-block 233.84.178.0/24
		echo "--> Global configuration onchain:"
		doublezero global-config get
		echo

		doublezero global-config authority set --activator-authority me --sentinel-authority me

		# Populate location information onchain.
		echo "==> Populating location information onchain"
		doublezero location create --code lax --name "Los Angeles" --country US --lat 34.049641274076464 --lng -118.25939642499903
		doublezero location create --code ewr --name "New York" --country US --lat 40.780297071772125 --lng -74.07203003496925
		doublezero location create --code lhr --name "London" --country UK --lat 51.513999803939384 --lng -0.12014764843092213
		doublezero location create --code fra --name "Frankfurt" --country DE --lat 50.1215356432098 --lng 8.642047117175098
		doublezero location create --code sin --name "Singapore" --country SG --lat 1.2807150707390342 --lng 103.85507136144396
		doublezero location create --code tyo --name "Tokyo" --country JP --lat 35.66875144228767 --lng 139.76565267564501
		doublezero location create --code pit --name "Pittsburgh" --country US --lat 40.45119259881935 --lng -80.00498215509094
		doublezero location create --code ams --name "Amsterdam" --country US --lat 52.30085793004002 --lng 4.942241140085309
		echo "--> Location information onchain:"
		doublezero location list

		# Populate exchange information onchain.
		echo "==> Populating exchange information onchain"
		doublezero exchange create --code xlax --name "Los Angeles" --lat 34.049641274076464 --lng -118.25939642499903
		doublezero exchange create --code xewr --name "New York" --lat 40.780297071772125 --lng -74.07203003496925
		doublezero exchange create --code xlhr --name "London" --lat 51.513999803939384 --lng -0.12014764843092213
		doublezero exchange create --code xfra --name "Frankfurt" --lat 50.1215356432098 --lng 8.642047117175098
		doublezero exchange create --code xsin --name "Singapore" --lat 1.2807150707390342 --lng 103.85507136144396
		doublezero exchange create --code xtyo --name "Tokyo" --lat 35.66875144228767 --lng 139.76565267564501
		doublezero exchange create --code xpit --name "Pittsburgh" --lat 40.45119259881935 --lng -80.00498215509094
		doublezero exchange create --code xams --name "Amsterdam" --lat 52.30085793004002 --lng 4.942241140085309
		echo "--> Exchange information onchain:"
		doublezero exchange list

		echo "==> Populating contributor information onchain"
		doublezero contributor create --code co01 --owner me

		echo "--> Smart contract initialized"

	`})
	if err != nil {
		return fmt.Errorf("failed to execute script initializing smart contract: %w", err)
	}

	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return fmt.Errorf("failed to get serviceability program client: %w", err)
	}

	// Wait for the global config to be populated.
	err = poll.Until(ctx, func() (bool, error) {
		data, err := client.GetProgramData(ctx)
		if err != nil {
			// GetProgramData returns an error when the program has no accounts yet,
			// which is expected before initialization completes. Continue polling.
			if strings.Contains(err.Error(), "GetProgramAccounts returned empty result") {
				dn.log.Debug("--> Waiting for program accounts to be created")
				return false, nil
			}
			return false, fmt.Errorf("failed to load serviceability program client: %w", err)
		}
		config := data.Config

		if config.Local_asn != 0 {
			return true, nil
		}

		dn.log.Debug("--> Waiting for global config update", "config", config)
		return false, nil
	}, 30*time.Second, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to wait for global config to be populated: %w", err)
	}

	dn.log.Info("--> Smart contract initialized")

	return nil
}
