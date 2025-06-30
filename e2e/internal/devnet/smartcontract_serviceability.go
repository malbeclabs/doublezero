package devnet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

func (dn *Devnet) DeployServiceabilityProgramIfNotDeployed(ctx context.Context) (bool, error) {
	log := dn.log.With("programID", dn.Manager.ServiceabilityProgramID)

	isDeployed, err := dn.IsServiceabilityProgramDeployed(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if serviceability program is deployed: %w", err)
	}

	if isDeployed {
		log.Info("--> Serviceability program is already deployed")
		return false, nil
	}

	log.Info("--> Serviceability program is not deployed, deploying")
	err = dn.DeployServiceabilityProgram(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to deploy serviceability program: %w", err)
	}

	return true, nil
}

func (dn *Devnet) DeployServiceabilityProgram(ctx context.Context) error {
	dn.log.Info("==> Deploying serviceability program", "programID", dn.Manager.ServiceabilityProgramID)

	start := time.Now()

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

		# Deploy the serviceability program.
		echo "==> Deploying serviceability program"
		solana program deploy --program-id ${DZ_SERVICEABILITY_PROGRAM_KEYPAIR_PATH} ${DZ_SERVICEABILITY_PROGRAM_PATH}

		# Wait 1 slot to make sure the program is deployed and avoid race condition of follow-on instructions.
		echo "==> Waiting for serviceability program to be ready"
		slot_before=$(solana slot)
		echo "==> Slot before: $slot_before"
		until [ "$(solana slot)" -gt "$slot_before" ]; do
			echo "==> Waiting for serviceability program to be ready (slot: $(solana slot))"
			sleep 0.2
		done
		echo "==> Slot after: $(solana slot)"
	`})
	if err != nil {
		return fmt.Errorf("failed to deploy serviceability program: %w", err)
	}

	dn.log.Info("--> Serviceability program deployed", "duration", time.Since(start), "programID", dn.Manager.ServiceabilityProgramID)
	return nil
}

func (dn *Devnet) IsServiceabilityProgramDeployed(ctx context.Context) (bool, error) {
	output, err := dn.Manager.Exec(ctx, []string{"solana", "program", "show", dn.Manager.ServiceabilityProgramID}, docker.NoPrintOnError())
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "unable to find") {
			return false, nil
		}
		fmt.Println("output", string(output))
		return false, fmt.Errorf("failed to check if serviceability program is deployed: %w", err)
	}
	return true, nil
}
