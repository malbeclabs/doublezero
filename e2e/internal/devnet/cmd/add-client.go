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
	var routeLivenessEnablePassive bool
	var routeLivenessEnableActive bool

	cmd := &cobra.Command{
		Use:   "add-client",
		Short: "Create and start a client on the devnet",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			err := dn.Start(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to start devnet: %w", err)
			}

			_, err = dn.AddClient(ctx, devnet.ClientSpec{
				CYOANetworkIPHostID:        cyoaNetworkHostID,
				KeypairPath:                keypairPath,
				RouteLivenessEnablePassive: routeLivenessEnablePassive,
				RouteLivenessEnableActive:  routeLivenessEnableActive,
			})
			if err != nil {
				return fmt.Errorf("failed to add client: %w", err)
			}

			return nil
		}),
	}

	cmd.Flags().Uint32Var(&cyoaNetworkHostID, "cyoa-network-host-id", 0, "CYOA network host ID; if the subnet CIDR prefix is 24 (default), this represents the last octet of the IP address")
	_ = cmd.MarkFlagRequired("cyoa-network-host-id")

	cmd.Flags().StringVar(&keypairPath, "keypair-path", "", "Path to the keypair file (optional)")
	cmd.Flags().BoolVar(&routeLivenessEnablePassive, "route-liveness-enable-passive", false, "Enable route liveness in passive mode (experimental)")
	cmd.Flags().BoolVar(&routeLivenessEnableActive, "route-liveness-enable-active", false, "Enable route liveness in active mode (experimental)")

	return cmd
}
