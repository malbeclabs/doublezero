package devnet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/docker"
)

func (dn *Devnet) DeployGeolocationProgramIfNotDeployed(ctx context.Context) (bool, error) {
	log := dn.log.With("programID", dn.Manager.GeolocationProgramID)

	isDeployed, err := dn.IsGeolocationProgramDeployed(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if geolocation program is deployed: %w", err)
	}

	if isDeployed {
		log.Debug("--> Geolocation program is already deployed")
		return false, nil
	}

	log.Debug("--> Geolocation program is not deployed, deploying")
	err = dn.DeployGeolocationProgram(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to deploy geolocation program: %w", err)
	}

	return true, nil
}

func (dn *Devnet) DeployGeolocationProgram(ctx context.Context) error {
	dn.log.Debug("==> Deploying geolocation program", "programID", dn.Manager.GeolocationProgramID)

	start := time.Now()

	_, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		# Fund the manager account with some SOL if the balance is 0.
		if solana balance | grep -q "^0 SOL$"; then
			solana airdrop 100 $(solana-keygen pubkey)
		fi

		# Deploy the geolocation program.
		solana program deploy --program-id ${DZ_GEOLOCATION_PROGRAM_KEYPAIR_PATH} ${DZ_GEOLOCATION_PROGRAM_PATH}

		# Wait 1 slot to make sure the program is deployed and avoid race condition of follow-on instructions.
		slot_before=$(solana slot)
		until [ "$(solana slot)" -gt "$slot_before" ]; do
			sleep 0.2
		done
	`})
	if err != nil {
		return fmt.Errorf("failed to deploy geolocation program: %w", err)
	}

	dn.log.Debug("--> Geolocation program deployed", "duration", time.Since(start), "programID", dn.Manager.GeolocationProgramID)
	return nil
}

func (dn *Devnet) IsGeolocationProgramDeployed(ctx context.Context) (bool, error) {
	output, err := dn.Manager.Exec(ctx, []string{"solana", "program", "show", dn.Manager.GeolocationProgramID}, docker.NoPrintOnError())
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "unable to find") {
			return false, nil
		}
		fmt.Println("output", string(output))
		return false, fmt.Errorf("failed to check if geolocation program is deployed: %w", err)
	}
	return true, nil
}

func (dn *Devnet) InitGeolocationProgramConfigIfNotInitialized(ctx context.Context) (bool, error) {
	dn.log.Debug("==> Initializing geolocation program config")

	output, err := dn.Manager.Exec(ctx, []string{
		"doublezero-geolocation", "init-config", "--yes",
	}, docker.NoPrintOnError())
	if err != nil {
		outputStr := strings.ToLower(string(output))
		if strings.Contains(outputStr, "already") || strings.Contains(outputStr, "already in use") || strings.Contains(outputStr, "uninitialized account") {
			dn.log.Debug("--> Geolocation program config is already initialized")
			return false, nil
		}
		return false, fmt.Errorf("failed to initialize geolocation program config: %w", err)
	}

	dn.log.Debug("--> Geolocation program config initialized")
	return true, nil
}
