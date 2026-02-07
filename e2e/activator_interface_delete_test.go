//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

// TestE2E_ActivatorInterfaceDeleteOutOfPoolIP tests that the activator does not panic
// when deleting a Loopback interface that has an IP address outside the tunnel pool.
//
// This reproduces a bug where ip_to_index() in IPBlockAllocator only checked the lower bound,
// causing a bitvec index out of range panic when deleting an interface with an out-of-pool IP.
//
// The scenario is:
// 1. Create a Loopback interface with --ip-net flag (IP outside tunnel pool)
// 2. Delete the interface
// 3. Activator should handle gracefully (not panic) and remove the interface
func TestE2E_ActivatorInterfaceDeleteOutOfPoolIP(t *testing.T) {
	t.Parallel()

	dn, device, _ := NewSingleDeviceSingleClientTestDevnet(t)

	// Test interface name and out-of-pool IP
	testInterfaceName := "Loopback100"
	testDeviceCode := device.Spec.Code
	outOfPoolIP := "195.219.121.96/32" // Outside 172.16.0.0/16 pool

	// Step 1: Create a Loopback interface with an IP outside the tunnel pool
	// This bypasses the activator's IP allocation - the IP is set directly at creation
	if !t.Run("create_loopback_with_out_of_pool_ip", func(t *testing.T) {
		dn.log.Debug("==> Creating Loopback100 interface with out-of-pool IP", "device", testDeviceCode, "ip", outOfPoolIP)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--ip-net", outOfPoolIP,
			"--user-tunnel-endpoint", "true",
		})
		require.NoError(t, err, "failed to create loopback interface with out-of-pool IP")

		dn.log.Debug("--> Interface created with out-of-pool IP")
	}) {
		t.Fail()
		return
	}

	// Step 2: Wait for the interface to be activated
	if !t.Run("wait_for_interface_activation", func(t *testing.T) {
		dn.log.Debug("==> Waiting for interface to be activated")

		err := waitForDeviceInterfaceActivated(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not activated within timeout")

		dn.log.Debug("--> Interface activated")
	}) {
		t.Fail()
		return
	}

	// Step 3: Delete the interface - this triggers the bug scenario
	if !t.Run("delete_interface_with_out_of_pool_ip", func(t *testing.T) {
		dn.log.Debug("==> Deleting interface with out-of-pool IP")

		err := dn.DeleteDeviceLoopbackInterface(t.Context(), testDeviceCode, testInterfaceName)
		require.NoError(t, err, "failed to delete loopback interface")

		// Give the activator time to process the deletion
		time.Sleep(5 * time.Second)
	}) {
		t.Fail()
		return
	}

	// Step 4: Verify that the activator is still running (didn't crash from the panic)
	if !t.Run("verify_activator_running", func(t *testing.T) {
		dn.log.Debug("==> Verifying activator container is still running")

		running, err := isActivatorRunning(t.Context(), dn.Devnet)
		require.NoError(t, err, "failed to check activator status")
		require.True(t, running, "activator container is not running - it likely crashed")

		dn.log.Debug("--> Activator is still running")
	}) {
		t.Fail()
		return
	}

	// Step 5: Verify the interface is eventually removed from chain
	if !t.Run("verify_interface_removed", func(t *testing.T) {
		dn.log.Debug("==> Verifying interface is removed from chain")

		err := waitForDeviceInterfaceRemoved(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not removed from chain within timeout")

		dn.log.Debug("--> Interface successfully removed from chain")
	}) {
		t.Fail()
	}
}

// waitForDeviceInterfaceActivated waits until the specified interface on a device is activated.
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
						// Interface still exists
						return false, nil
					}
				}
				// Interface not found - it has been removed
				return true, nil
			}
		}
		// Device not found (shouldn't happen)
		return false, nil
	}

	return poll.Until(ctx, condition, timeout, 2*time.Second)
}

// isActivatorRunning checks if the activator container is still running.
func isActivatorRunning(ctx context.Context, dn *devnet.Devnet) (bool, error) {
	container, err := dn.Activator.GetContainerState(ctx)
	if err != nil {
		return false, err
	}
	return container.Running, nil
}
