package devnetcmd

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/spf13/cobra"
)

type AddDeviceCmd struct{}

func NewAddDeviceCmd() *AddDeviceCmd {
	return &AddDeviceCmd{}
}

func (c *AddDeviceCmd) Command() *cobra.Command {
	var code string
	var cyoaNetworkHostID uint32

	cmd := &cobra.Command{
		Use:   "add-device",
		Short: "Create and start a device on the devnet",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			err := dn.Start(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to start devnet: %w", err)
			}

			_, err = dn.AddDevice(ctx, devnet.DeviceSpec{
				Code:                code,
				CYOANetworkIPHostID: cyoaNetworkHostID,
			})
			if err != nil {
				return fmt.Errorf("failed to add device: %w", err)
			}

			return nil
		}),
	}

	cmd.Flags().StringVar(&code, "code", "", "Device code")
	cmd.Flags().Uint32Var(&cyoaNetworkHostID, "cyoa-network-host-id", 0, "CYOA network host ID")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("cyoa-network-host-id")

	return cmd
}
