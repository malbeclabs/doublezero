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
	var additionalNetworksShortNames []string

	cmd := &cobra.Command{
		Use:   "add-device",
		Short: "Create and start a device on the devnet",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			err := dn.Start(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to start devnet: %w", err)
			}

			additionalNetworks := make([]string, 0, len(additionalNetworksShortNames))
			for _, network := range additionalNetworksShortNames {
				network := devnet.NewMiscNetwork(dn.Devnet, dn.log, network)
				_, err = network.CreateIfNotExists(ctx)
				if err != nil {
					return fmt.Errorf("failed to create or get additional network %s: %w", network.Name, err)
				}
				additionalNetworks = append(additionalNetworks, network.Name)
			}

			_, err = dn.AddDevice(ctx, devnet.DeviceSpec{
				Code:                         code,
				Location:                     location,
				Exchange:                     exchange,
				CYOANetworkIPHostID:          cyoaNetworkHostID,
				CYOANetworkAllocatablePrefix: cyoaNetworkAllocatablePrefix,
				Telemetry: devnet.DeviceTelemetrySpec{
					Enabled:      true,
					ManagementNS: "ns-management",
					Verbose:      true,
				},
				Interfaces: map[string]string{
					"Ethernet2": "physical",
				},
				LoopbackInterfaces: map[string]string{
					"Loopback255": "vpnv4",
					"Loopback256": "ipv4",
				},
				AdditionalNetworks: additionalNetworks,
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
	cmd.Flags().StringSliceVar(&additionalNetworksShortNames, "additional-networks", []string{}, "Additional docker networks for this device")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("location")
	_ = cmd.MarkFlagRequired("exchange")
	_ = cmd.MarkFlagRequired("cyoa-network-host-id")

	return cmd
}
