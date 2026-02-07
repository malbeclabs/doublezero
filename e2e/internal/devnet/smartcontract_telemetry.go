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
		log.Debug("--> Telemetry program is already deployed")
		return false, nil
	}

	log.Debug("--> Telemetry program is not deployed, deploying")
	err = dn.DeployTelemetryProgram(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to deploy telemetry program: %w", err)
	}

	return true, nil
}

func (dn *Devnet) DeployTelemetryProgram(ctx context.Context) error {
	dn.log.Debug("==> Deploying telemetry program", "programID", dn.Manager.TelemetryProgramID)

	start := time.Now()

	_, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		# Fund the manager account with some SOL if the balance is 0.
		if solana balance | grep -q "^0 SOL$"; then
			solana airdrop 100 $(solana-keygen pubkey)
		fi

		# Deploy the telemetry program.
		solana program deploy --program-id ${DZ_TELEMETRY_PROGRAM_KEYPAIR_PATH} ${DZ_TELEMETRY_PROGRAM_PATH}

		# Wait 1 slot to make sure the program is deployed and avoid race condition of follow-on instructions.
		slot_before=$(solana slot)
		until [ "$(solana slot)" -gt "$slot_before" ]; do
			sleep 0.2
		done
	`})
	if err != nil {
		return fmt.Errorf("failed to deploy telemetry program: %w", err)
	}

	dn.log.Debug("--> Telemetry program deployed", "duration", time.Since(start), "programID", dn.Manager.TelemetryProgramID)
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
