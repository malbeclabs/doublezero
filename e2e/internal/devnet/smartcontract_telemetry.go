package devnet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

func (dn *Devnet) DeployTelemetryProgramIfNotDeployed(ctx context.Context) (bool, error) {
	log := dn.log.With("programID", dn.Manager.TelemetryProgramID)

	isDeployed, err := dn.IsTelemetryProgramDeployed(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if telemetry program is deployed: %w", err)
	}

	if isDeployed {
		log.Info("--> Telemetry program is already deployed")
		return false, nil
	}

	log.Info("--> Telemetry program is not deployed, deploying")
	err = dn.DeployTelemetryProgram(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to deploy telemetry program: %w", err)
	}

	return true, nil
}

func (dn *Devnet) DeployTelemetryProgram(ctx context.Context) error {
	dn.log.Info("==> Deploying telemetry program", "programID", dn.Manager.TelemetryProgramID)

	start := time.Now()

	_, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		# Fund the manager account with some SOL if the balance is 0.
		echo "==> Checking manager account balance"
		solana balance
		if solana balance | grep -q "^0 SOL$"; then
			echo "==> Manager account balance is 0 SOL, funding with 1000 SOL"
			solana airdrop 100 $(solana-keygen pubkey)
		fi
		echo

		# Deploy the telemetry program.
		echo "==> Deploying telemetry program"
		solana program deploy --program-id ${DZ_TELEMETRY_PROGRAM_KEYPAIR_PATH} ${DZ_TELEMETRY_PROGRAM_PATH}

		# Wait 1 slot to make sure the program is deployed and avoid race condition of follow-on instructions.
		echo "==> Waiting for telemetry program to be ready"
		slot_before=$(solana slot)
		echo "==> Slot before: $slot_before"
		until [ "$(solana slot)" -gt "$slot_before" ]; do
			echo "==> Waiting for telemetry program to be ready (slot: $(solana slot))"
			sleep 0.2
		done
		echo "==> Slot after: $(solana slot)"
	`})
	if err != nil {
		return fmt.Errorf("failed to deploy telemetry program: %w", err)
	}

	dn.log.Info("--> Telemetry program deployed", "duration", time.Since(start), "programID", dn.Manager.TelemetryProgramID)
	return nil
}

func (dn *Devnet) IsTelemetryProgramDeployed(ctx context.Context) (bool, error) {
	output, err := dn.Manager.Exec(ctx, []string{"solana", "program", "show", dn.Manager.TelemetryProgramID}, docker.NoPrintOnError())
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "unable to find") {
			return false, nil
		}
		fmt.Println("output", string(output))
		return false, fmt.Errorf("failed to check if telemetry program is deployed: %w", err)
	}
	return true, nil
}
