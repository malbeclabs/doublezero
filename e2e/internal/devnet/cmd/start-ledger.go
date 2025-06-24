package devnetcmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type StartLedgerCmd struct{}

func NewStartLedgerCmd() *StartLedgerCmd {
	return &StartLedgerCmd{}
}

func (c *StartLedgerCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start-ledger",
		Short: "Start the ledger if it's not already running. This command won't start the devnet if it's not already running",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			_, err := dn.DefaultNetwork.CreateIfNotExists(ctx)
			if err != nil {
				return fmt.Errorf("failed to create default network: %w", err)
			}

			_, err = dn.Ledger.StartIfNotRunning(ctx)
			if err != nil {
				return fmt.Errorf("failed to start ledger: %w", err)
			}

			return nil
		}),
	}

	return cmd
}
