//go:build e2e

package e2e_test

import (
	"context"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// waitForDeviceInterfaceActivated waits until the specified interface is activated on the device.
func waitForDeviceInterfaceActivated(ctx context.Context, dn *devnet.Devnet, deviceCode, interfaceName string, timeout time.Duration) error {
	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return err
	}

	condition := func() (bool, error) {
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false, err
		}
		for _, device := range data.Devices {
			if device.Code == deviceCode {
				for _, iface := range device.Interfaces {
					if iface.Name == interfaceName {
						return iface.Status == serviceability.InterfaceStatusActivated, nil
					}
				}
			}
		}
		return false, nil
	}

	return poll.Until(ctx, condition, timeout, 2*time.Second)
}

// waitForDeviceInterfaceRemoved waits until the specified interface is removed from the device.
func waitForDeviceInterfaceRemoved(ctx context.Context, dn *devnet.Devnet, deviceCode, interfaceName string, timeout time.Duration) error {
	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return err
	}

	condition := func() (bool, error) {
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false, err
		}
		for _, device := range data.Devices {
			if device.Code == deviceCode {
				for _, iface := range device.Interfaces {
					if iface.Name == interfaceName {
						return false, nil
					}
				}
				return true, nil
			}
		}
		return false, nil
	}

	return poll.Until(ctx, condition, timeout, 2*time.Second)
}
