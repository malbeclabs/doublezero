//go:build e2e

package e2e_test

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

// TestE2E_InterfaceValidation tests the interface validation rules for CYOA and public IPs.
// This test consolidates multiple validation scenarios into a single test function to reduce
// the number of Docker networks created, avoiding network address pool exhaustion.
//
// The validation rules being tested are:
// 1. CYOA can only be set on physical interfaces (not loopbacks)
// 2. Public IPs are allowed on loopback interfaces when user_tunnel_endpoint=true
// 3. Public IPs are rejected on loopback interfaces when user_tunnel_endpoint=false
func TestE2E_InterfaceValidation(t *testing.T) {
	t.Parallel()

	dn, device, _ := NewSingleDeviceSingleClientTestDevnet(t)
	testDeviceCode := device.Spec.Code

	// Test 1: CYOA on loopback should be rejected
	if !t.Run("cyoa_on_loopback_rejected", func(t *testing.T) {
		testInterfaceName := "Loopback101"
		dn.log.Debug("==> Attempting to create Loopback interface with CYOA (should fail)", "device", testDeviceCode)

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--interface-cyoa", "gre-over-dia",
			"--bandwidth", "10G",
		})

		require.Error(t, err, "expected error when creating loopback with CYOA")
		require.True(t,
			strings.Contains(string(output), "CYOA can only be set on physical interfaces") ||
				strings.Contains(string(output), "CyoaRequiresPhysical") ||
				strings.Contains(string(output), "0x45"), // error code 69 in hex
			"expected CyoaRequiresPhysical error, got: %s", string(output))

		dn.log.Debug("--> Correctly rejected loopback with CYOA")
	}) {
		t.FailNow()
	}

	// Test 2: CYOA on physical interface should be allowed
	if !t.Run("cyoa_on_physical_allowed", func(t *testing.T) {
		testInterfaceName := "Ethernet10"
		dn.log.Debug("==> Creating physical interface with CYOA", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--interface-cyoa", "gre-over-dia",
			"--ip-net", "45.33.100.50/31",
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create physical interface with CYOA")

		// Verify the interface was created with correct CYOA value
		iface, err := waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not found")
		require.Equal(t, serviceability.InterfaceCYOAGREOverDIA, iface.InterfaceCYOA, "interface CYOA mismatch")

		dn.log.Debug("--> Physical interface with CYOA created and verified")
	}) {
		t.FailNow()
	}

	// Test 3: Public IP on loopback with user_tunnel_endpoint=true should be allowed
	if !t.Run("public_ip_on_loopback_with_ute_allowed", func(t *testing.T) {
		testInterfaceName := "Loopback102"
		publicIP := "203.0.113.10/32" // TEST-NET-3 public IP

		dn.log.Debug("==> Creating Loopback interface with public IP and user_tunnel_endpoint=true", "device", testDeviceCode, "ip", publicIP)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--ip-net", publicIP,
			"--user-tunnel-endpoint", "true",
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create loopback interface with public IP and user_tunnel_endpoint")

		// Wait for interface to be activated
		err = waitForDeviceInterfaceActivated(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not activated within timeout")

		// Verify the interface has the correct properties
		iface, err := waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 30*time.Second)
		require.NoError(t, err, "failed to get interface")
		require.True(t, iface.UserTunnelEndpoint, "user_tunnel_endpoint should be true")
		require.NotEqual(t, [5]uint8{}, iface.IpNet, "interface IP should be set")

		dn.log.Debug("--> Loopback with public IP and user_tunnel_endpoint created and verified")
	}) {
		t.FailNow()
	}

	// Test 4: Public IP on loopback without user_tunnel_endpoint should be rejected
	if !t.Run("public_ip_on_loopback_without_ute_rejected", func(t *testing.T) {
		testInterfaceName := "Loopback103"
		publicIP := "203.0.113.20/32" // TEST-NET-3 public IP

		dn.log.Debug("==> Attempting to create Loopback with public IP but without user_tunnel_endpoint (should fail)", "device", testDeviceCode, "ip", publicIP)

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--ip-net", publicIP,
			"--user-tunnel-endpoint", "false",
			"--bandwidth", "10G",
		})

		require.Error(t, err, "expected error when creating loopback with public IP but without user_tunnel_endpoint")
		require.True(t,
			strings.Contains(string(output), "Invalid interface IP") ||
				strings.Contains(string(output), "InvalidInterfaceIp") ||
				strings.Contains(string(output), "0x2f"), // error code 47 in hex
			"expected InvalidInterfaceIp error, got: %s", string(output))

		dn.log.Debug("--> Correctly rejected loopback with public IP without user_tunnel_endpoint")
	}) {
		t.FailNow()
	}

	// Test 5: Update loopback to add CYOA should be rejected
	if !t.Run("update_loopback_add_cyoa_rejected", func(t *testing.T) {
		testInterfaceName := "Loopback104"

		// First create a valid loopback interface
		dn.log.Debug("==> Creating Loopback interface for update test", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create loopback interface")

		// Wait for interface to exist
		_, err = waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not found")

		// Attempt to update the loopback interface to add CYOA - this should fail
		dn.log.Debug("==> Attempting to update Loopback interface to add CYOA (should fail)", "device", testDeviceCode)

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "update",
			testDeviceCode, testInterfaceName,
			"--interface-cyoa", "gre-over-dia",
		})

		require.Error(t, err, "expected error when updating loopback to add CYOA")
		require.True(t,
			strings.Contains(string(output), "CYOA can only be set on physical interfaces") ||
				strings.Contains(string(output), "CyoaRequiresPhysical") ||
				strings.Contains(string(output), "0x45"), // error code 69 in hex
			"expected CyoaRequiresPhysical error, got: %s", string(output))

		dn.log.Debug("--> Correctly rejected update to add CYOA on loopback")
	}) {
		t.FailNow()
	}

	// Test 6: Update loopback to add public IP with user_tunnel_endpoint should be allowed
	if !t.Run("update_loopback_add_public_ip_with_ute_allowed", func(t *testing.T) {
		testInterfaceName := "Loopback105"
		publicIP := "203.0.113.30/32" // TEST-NET-3 public IP

		// First create a loopback interface with user_tunnel_endpoint but no IP
		dn.log.Debug("==> Creating Loopback interface with user_tunnel_endpoint for update test", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--user-tunnel-endpoint", "true",
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create loopback interface")

		// Wait for interface to exist
		_, err = waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not found")

		// Update the loopback interface to add a public IP - should succeed
		dn.log.Debug("==> Updating Loopback interface to add public IP", "device", testDeviceCode, "ip", publicIP)

		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "update",
			testDeviceCode, testInterfaceName,
			"--ip-net", publicIP,
		})
		require.NoError(t, err, "failed to update loopback interface with public IP")

		// Give time for the update to propagate
		time.Sleep(2 * time.Second)

		// Verify the interface has the correct IP
		iface, err := waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 30*time.Second)
		require.NoError(t, err, "failed to get interface")
		require.NotEqual(t, [5]uint8{}, iface.IpNet, "interface IP should be set")

		dn.log.Debug("--> Loopback updated with public IP successfully")
	}) {
		t.FailNow()
	}

	// Test 8: ip_net on plain physical interface (no CYOA, no DIA, no UTE) should be rejected on create
	if !t.Run("ip_net_on_plain_physical_create_rejected", func(t *testing.T) {
		testInterfaceName := "Ethernet11"

		dn.log.Debug("==> Attempting to create plain physical interface with ip_net (should fail)", "device", testDeviceCode)

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--ip-net", "45.33.100.50/31",
			"--bandwidth", "10G",
		})

		require.Error(t, err, "expected error when creating plain physical interface with ip_net")
		require.True(t,
			strings.Contains(string(output), "Invalid interface IP") ||
				strings.Contains(string(output), "InvalidInterfaceIp") ||
				strings.Contains(string(output), "0x2f"),
			"expected InvalidInterfaceIp error, got: %s", string(output))

		dn.log.Debug("--> Correctly rejected plain physical interface with ip_net")
	}) {
		t.FailNow()
	}

	// Test 9: ip_net on CYOA physical interface should be saved on create
	if !t.Run("ip_net_on_cyoa_physical_saved_on_create", func(t *testing.T) {
		testInterfaceName := "Ethernet12"

		dn.log.Debug("==> Creating CYOA physical interface with ip_net", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--interface-cyoa", "gre-over-dia",
			"--ip-net", "45.33.100.60/31",
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create CYOA physical interface with ip_net")

		// Verify the interface was created with correct CYOA and ip_net values
		iface, err := waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not found")
		require.Equal(t, serviceability.InterfaceCYOAGREOverDIA, iface.InterfaceCYOA, "interface CYOA mismatch")
		require.NotEqual(t, [5]uint8{}, iface.IpNet, "ip_net should be set on CYOA physical interface")

		dn.log.Debug("--> CYOA physical interface with ip_net created and verified")
	}) {
		t.FailNow()
	}

	// Test 10: ip_net on plain physical interface should be rejected on update
	if !t.Run("ip_net_on_plain_physical_update_rejected", func(t *testing.T) {
		testInterfaceName := "Ethernet13"

		// First create a plain physical interface without ip_net
		dn.log.Debug("==> Creating plain physical interface for update test", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create plain physical interface")

		// Wait for interface to exist
		_, err = waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not found")

		// Attempt to update the plain physical interface with ip_net - should fail
		dn.log.Debug("==> Attempting to update plain physical interface with ip_net (should fail)", "device", testDeviceCode)

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "update",
			testDeviceCode, testInterfaceName,
			"--ip-net", "45.33.100.70/31",
		})

		require.Error(t, err, "expected error when updating plain physical interface with ip_net")
		require.True(t,
			strings.Contains(string(output), "Invalid interface IP") ||
				strings.Contains(string(output), "InvalidInterfaceIp") ||
				strings.Contains(string(output), "0x2f"),
			"expected InvalidInterfaceIp error, got: %s", string(output))

		dn.log.Debug("--> Correctly rejected update of plain physical interface with ip_net")
	}) {
		t.FailNow()
	}

	// Test 11: Full lifecycle - create, update, delete
	t.Run("full_lifecycle", func(t *testing.T) {
		testInterfaceName := "Loopback106"
		publicIP := "203.0.113.40/32" // TEST-NET-3 public IP

		// Step 1: Create interface with public IP and user_tunnel_endpoint
		dn.log.Debug("==> Creating Loopback interface for lifecycle test", "device", testDeviceCode)

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			testDeviceCode, testInterfaceName,
			"--ip-net", publicIP,
			"--user-tunnel-endpoint", "true",
			"--bandwidth", "10G",
		})
		require.NoError(t, err, "failed to create loopback interface")

		// Step 2: Wait for activation
		err = waitForDeviceInterfaceActivated(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not activated")

		// Step 3: Update interface (change loopback type AND mtu to test multiple fields)
		dn.log.Debug("==> Updating interface loopback type and mtu")

		updateOutput, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "update",
			testDeviceCode, testInterfaceName,
			"--loopback-type", "ipv4",
			"--mtu", "9000",
		})
		dn.log.Debug("==> Update command output", "output", string(updateOutput))
		require.NoError(t, err, "failed to update loopback interface")

		// Poll until the Go SDK sees the updated values
		iface, err := waitForInterfaceUpdate(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, serviceability.LoopbackTypeIpv4, 9000, 30*time.Second)
		if err != nil {
			// If polling times out, get the final state for debugging
			finalIface, _ := waitForDeviceInterface(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 5*time.Second)
			if finalIface != nil {
				dn.log.Debug("==> Final interface state after timeout", "loopback_type", finalIface.LoopbackType, "mtu", finalIface.Mtu, "version", finalIface.Version, "name", finalIface.Name)
			}
			require.NoError(t, err, "timed out waiting for interface update to propagate to Go SDK")
		}
		dn.log.Debug("==> Retrieved interface via SDK", "loopback_type", iface.LoopbackType, "mtu", iface.Mtu, "version", iface.Version, "name", iface.Name)
		require.Equal(t, uint16(9000), iface.Mtu, "mtu mismatch - update not reflected in SDK")
		require.Equal(t, serviceability.LoopbackTypeIpv4, iface.LoopbackType, "loopback type mismatch")

		// Step 5: Delete interface
		dn.log.Debug("==> Deleting interface")

		err = dn.DeleteDeviceLoopbackInterface(t.Context(), testDeviceCode, testInterfaceName)
		require.NoError(t, err, "failed to delete loopback interface")

		// Step 6: Verify deletion
		err = waitForDeviceInterfaceRemoved(t.Context(), dn.Devnet, testDeviceCode, testInterfaceName, 60*time.Second)
		require.NoError(t, err, "interface was not removed")

		dn.log.Debug("--> Full lifecycle test completed successfully")
	})
}

// waitForDeviceInterface waits until the specified interface exists on a device and returns it.
func waitForDeviceInterface(ctx context.Context, dn *devnet.Devnet, deviceCode, interfaceName string, timeout time.Duration) (*serviceability.Interface, error) {
	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return nil, err
	}

	var foundInterface *serviceability.Interface

	condition := func() (bool, error) {
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false, err
		}

		for _, device := range data.Devices {
			if device.Code == deviceCode {
				for _, iface := range device.Interfaces {
					if iface.Name == interfaceName {
						foundInterface = &iface
						return true, nil
					}
				}
			}
		}
		return false, nil
	}

	err = poll.Until(ctx, condition, timeout, 2*time.Second)
	if err != nil {
		return nil, err
	}

	return foundInterface, nil
}

// waitForInterfaceUpdate waits until the specified interface has the expected loopback type.
// This is used to verify that updates have propagated to the Go SDK.
func waitForInterfaceUpdate(ctx context.Context, dn *devnet.Devnet, deviceCode, interfaceName string, expectedLoopbackType serviceability.LoopbackType, expectedMtu uint16, timeout time.Duration) (*serviceability.Interface, error) {
	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return nil, err
	}

	var foundInterface *serviceability.Interface

	pollCount := 0
	condition := func() (bool, error) {
		pollCount++
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false, err
		}

		for _, device := range data.Devices {
			if device.Code == deviceCode {
				for _, iface := range device.Interfaces {
					if iface.Name == interfaceName {
						foundInterface = &iface
						// Log current vs expected values on each poll
						log.Printf("waitForInterfaceUpdate poll #%d: version=%d, loopbackType=%d (expected=%d), mtu=%d (expected=%d), status=%d, interfaceType=%d",
							pollCount, iface.Version, iface.LoopbackType, expectedLoopbackType, iface.Mtu, expectedMtu, iface.Status, iface.InterfaceType)
						// Check if both values match expected
						if iface.LoopbackType == expectedLoopbackType && iface.Mtu == expectedMtu {
							return true, nil
						}
						// Interface found but values don't match yet - keep polling
						return false, nil
					}
				}
			}
		}
		return false, nil
	}

	err = poll.Until(ctx, condition, timeout, 500*time.Millisecond)
	if err != nil {
		return foundInterface, err
	}

	return foundInterface, nil
}
