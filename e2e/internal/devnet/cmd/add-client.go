package devnetcmd

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/spf13/cobra"
)

type AddClientCmd struct{}

func NewAddClientCmd() *AddClientCmd {
	return &AddClientCmd{}
}

func (c *AddClientCmd) Command() *cobra.Command {
	var cyoaNetworkHostID uint32
	var keypairPath string

	cmd := &cobra.Command{
		Use:   "add-client",
		Short: "Create and start a client on the devnet",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			err := dn.Start(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to start devnet: %w", err)
			}

			_, err = dn.AddClient(ctx, devnet.ClientSpec{
				CYOANetworkIPHostID: cyoaNetworkHostID,
				KeypairPath:         keypairPath,
			})
			if err != nil {
				return fmt.Errorf("failed to add client: %w", err)
			}

			return nil
		}),
	}

	cmd.Flags().Uint32Var(&cyoaNetworkHostID, "cyoa-network-host-id", 0, "CYOA network host ID")
	_ = cmd.MarkFlagRequired("cyoa-network-host-id")

	cmd.Flags().StringVar(&keypairPath, "keypair-path", "", "Path to the keypair file (optional)")

	return cmd
}
