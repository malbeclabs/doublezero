package devnetcmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type DeployProgramsCmd struct{}

func NewDeployProgramsCmd() *DeployProgramsCmd {
	return &DeployProgramsCmd{}
}

func (c *DeployProgramsCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-programs",
		Short: "Deploy the Serviceability and Telemetry programs to the ledger",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			_, err := dn.DefaultNetwork.CreateIfNotExists(ctx)
			if err != nil {
				return fmt.Errorf("failed to create default network: %w", err)
			}

			_, err = dn.Ledger.StartIfNotRunning(ctx)
			if err != nil {
				return fmt.Errorf("failed to start ledger: %w", err)
			}

			_, err = dn.Manager.StartIfNotRunning(ctx)
			if err != nil {
				return fmt.Errorf("failed to start manager: %w", err)
			}

			err = dn.DeployServiceabilityProgram(ctx)
			if err != nil {
				return fmt.Errorf("failed to deploy serviceability program: %w", err)
			}

			return err
		}),
	}

	return cmd
}
