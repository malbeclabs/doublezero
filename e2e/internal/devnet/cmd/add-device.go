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
	var location string
	var exchange string
	var cyoaNetworkHostID uint32
	var cyoaNetworkAllocatablePrefix uint32

	cmd := &cobra.Command{
		Use:   "add-device",
		Short: "Create and start a device on the devnet",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			err := dn.Start(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to start devnet: %w", err)
			}

			_, err = dn.AddDevice(ctx, devnet.DeviceSpec{
				Code:                         code,
				Location:                     location,
				Exchange:                     exchange,
				CYOANetworkIPHostID:          cyoaNetworkHostID,
				CYOANetworkAllocatablePrefix: cyoaNetworkAllocatablePrefix,
			})
			if err != nil {
				return fmt.Errorf("failed to add device: %w", err)
			}

			return nil
		}),
	}

	cmd.Flags().StringVar(&code, "code", "", "Device code")
	cmd.Flags().StringVar(&location, "location", "", "Device location")
	cmd.Flags().StringVar(&exchange, "exchange", "", "Device exchange")
	cmd.Flags().Uint32Var(&cyoaNetworkHostID, "cyoa-network-host-id", 0, "CYOA network host ID; if the subnet CIDR prefix is 24 (default), this represents the last octet of the IP address")
	cmd.Flags().Uint32Var(&cyoaNetworkAllocatablePrefix, "cyoa-network-allocatable-prefix", 0, "CYOA network allocatable prefix; the prefix length of the block of IPs that are available for allocation to clients for this device in the CYOA subnet (default 29)")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("location")
	_ = cmd.MarkFlagRequired("exchange")
	_ = cmd.MarkFlagRequired("cyoa-network-host-id")

	return cmd
}
