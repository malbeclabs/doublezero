//go:build e2e

package e2e_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/allocation"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_User_AllocationLifecycle tests the full allocation/deallocation lifecycle for user resources.
// It verifies that when a user is created and deleted:
// - tunnel_net is allocated from and returned to UserTunnelBlock
// - tunnel_id is allocated from and returned to TunnelIds[device]
// - dz_ip is allocated from and returned to DzPrefixBlock[device] (for IBRL with allocated IP)
func TestE2E_User_AllocationLifecycle(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create device for user connection
	log.Info("==> Creating device for user allocation test")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		echo "==> Creating device"
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.100.8/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
		doublezero device interface create test-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create test-dz01 "Loopback256" --loopback-type ipv4 -w
	`})
	log.Info("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed")

	// Wait for device to be activated and capture device pubkey
	log.Info("==> Waiting for device activation")
	var devicePubkey solana.PublicKey
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, device := range data.Devices {
			if device.Code == "test-dz01" && device.Status == serviceability.DeviceStatusActivated {
				devicePubkey = solana.PublicKeyFromBytes(device.PubKey[:])
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "device was not activated within timeout")

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Capture snapshot BEFORE user creation
	log.Info("==> Capturing ResourceExtension state before user creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// Log initial state
	if beforeAlloc.UserTunnelBlock != nil {
		log.Info("UserTunnelBlock before allocation",
			"allocated", beforeAlloc.UserTunnelBlock.Allocated,
			"available", beforeAlloc.UserTunnelBlock.Available,
			"total", beforeAlloc.UserTunnelBlock.Total)
	}

	// Add a client and create user
	log.Info("==> Adding client and creating user")
	client, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Set access pass and create user with allocated IP
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Create user with allocated IP (IBRL with allocated IP allocates from DzPrefixBlock)
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero user create --device test-dz01 --client-ip " + client.CYOANetworkIP + " --allocate-addr"})
	require.NoError(t, err)

	// Wait for user to be activated
	log.Info("==> Waiting for user activation")
	var activatedUser *serviceability.User
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if user.Status == serviceability.UserStatusActivated {
				activatedUser = &user
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "user was not activated within timeout")

	log.Info("==> User activated",
		"tunnel_id", activatedUser.TunnelId,
		"tunnel_net", activatedUser.TunnelNet,
		"dz_ip", activatedUser.DzIp)

	// Capture snapshot AFTER user creation
	log.Info("==> Capturing ResourceExtension state after user creation")
	afterAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-allocation snapshot")

	// Verify global resources were allocated
	if beforeAlloc.UserTunnelBlock != nil && afterAlloc.UserTunnelBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "UserTunnelBlock", 2) // /31 = 2 IPs
		require.NoError(t, err, "UserTunnelBlock allocation verification failed")
		log.Info("UserTunnelBlock after allocation",
			"allocated", afterAlloc.UserTunnelBlock.Allocated,
			"available", afterAlloc.UserTunnelBlock.Available)
	}

	// Verify device-specific resources were allocated (TunnelIds and DzPrefixBlock)
	// For IBRL with allocated IP: 1 TunnelId + 1 DzPrefix IP
	err = verifier.AssertDeviceResourcesAllocated(beforeAlloc, afterAlloc, devicePubkey, 1, 1)
	require.NoError(t, err, "Device-specific resource allocation verification failed")

	// Log device-specific resource state
	beforeTunnelIds, _ := verifier.FindTunnelIdsForDevice(beforeAlloc, devicePubkey)
	afterTunnelIds, _ := verifier.FindTunnelIdsForDevice(afterAlloc, devicePubkey)
	beforeDzPrefix := verifier.GetTotalDzPrefixAllocatedForDevice(beforeAlloc, devicePubkey)
	afterDzPrefix := verifier.GetTotalDzPrefixAllocatedForDevice(afterAlloc, devicePubkey)
	log.Info("Device-specific resources after allocation",
		"device", devicePubkey.String()[:8]+"...",
		"TunnelIds_before", beforeTunnelIds.Allocated, "TunnelIds_after", afterTunnelIds.Allocated,
		"DzPrefix_before", beforeDzPrefix, "DzPrefix_after", afterDzPrefix)

	// Delete user to trigger deallocation
	userPubkey := base58.Encode(activatedUser.PubKey[:])
	log.Info("==> Deleting user to trigger deallocation", "pubkey", userPubkey)
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero user delete --pubkey " + userPubkey})
	require.NoError(t, err)

	// Wait for user to be deleted (removed from program data)
	log.Info("==> Waiting for user to be closed")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == userPubkey {
				log.Debug("User still exists", "status", user.Status)
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "user was not closed within timeout")

	// Capture snapshot AFTER deallocation
	log.Info("==> Capturing ResourceExtension state after user deletion")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify global resources were returned
	if afterAlloc.UserTunnelBlock != nil && afterDealloc.UserTunnelBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "UserTunnelBlock", 2)
		require.NoError(t, err, "tunnel_net not properly deallocated from UserTunnelBlock")
		log.Info("UserTunnelBlock after deallocation",
			"allocated", afterDealloc.UserTunnelBlock.Allocated,
			"available", afterDealloc.UserTunnelBlock.Available)
	}

	// Verify device-specific resources were deallocated (TunnelIds and DzPrefixBlock)
	err = verifier.AssertDeviceResourcesDeallocated(afterAlloc, afterDealloc, devicePubkey, 1, 1)
	require.NoError(t, err, "Device-specific resource deallocation verification failed")

	// Log device-specific resource state after deallocation
	afterDeallocTunnelIds, _ := verifier.FindTunnelIdsForDevice(afterDealloc, devicePubkey)
	afterDeallocDzPrefix := verifier.GetTotalDzPrefixAllocatedForDevice(afterDealloc, devicePubkey)
	log.Info("Device-specific resources after deallocation",
		"device", devicePubkey.String()[:8]+"...",
		"TunnelIds_after", afterDeallocTunnelIds.Allocated,
		"DzPrefix_after", afterDeallocDzPrefix)

	// Verify UserTunnelBlock returned to pre-allocation state
	// Note: We only check UserTunnelBlock here because that's what the user test uses.
	// Other global pools (DeviceTunnelBlock, LinkIds) may have allocations from device
	// activation that are unrelated to the user lifecycle being tested.
	log.Info("==> Verifying UserTunnelBlock returned to pre-allocation state")
	if beforeAlloc.UserTunnelBlock != nil && afterDealloc.UserTunnelBlock != nil {
		if beforeAlloc.UserTunnelBlock.Allocated != afterDealloc.UserTunnelBlock.Allocated {
			t.Errorf("UserTunnelBlock: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
				beforeAlloc.UserTunnelBlock.Allocated, afterDealloc.UserTunnelBlock.Allocated)
		}
	}

	// Verify device-specific resources returned to pre-allocation state
	log.Info("==> Verifying device-specific resources returned to pre-allocation state")
	if beforeTunnelIds.Allocated != afterDeallocTunnelIds.Allocated {
		t.Errorf("TunnelIds[%s]: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
			devicePubkey.String()[:8]+"...", beforeTunnelIds.Allocated, afterDeallocTunnelIds.Allocated)
	}
	if beforeDzPrefix != afterDeallocDzPrefix {
		t.Errorf("DzPrefixBlock[%s]: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
			devicePubkey.String()[:8]+"...", beforeDzPrefix, afterDeallocDzPrefix)
	}

	log.Info("==> User allocation lifecycle test completed successfully")
}

// TestE2E_MulticastGroup_AllocationLifecycle tests the full allocation/deallocation lifecycle
// for multicast group resources. It verifies that when a multicast group is created and deleted:
// - multicast_ip is allocated from and returned to MulticastGroupBlock
func TestE2E_MulticastGroup_AllocationLifecycle(t *testing.T) {

	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Capture snapshot BEFORE multicast group creation
	log.Info("==> Capturing ResourceExtension state before multicast group creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	if beforeAlloc.MulticastGroupBlock != nil {
		log.Info("MulticastGroupBlock before allocation",
			"allocated", beforeAlloc.MulticastGroupBlock.Allocated,
			"available", beforeAlloc.MulticastGroupBlock.Available,
			"total", beforeAlloc.MulticastGroupBlock.Total)
	}

	// Create multicast group
	// Note: We don't use -w (wait for activation) here because there's a race condition
	// between the activator's initial fetch and the multicast group creation. The activator
	// polls every 60 seconds, which matches the CLI's -w timeout, causing failures.
	// Instead, we let require.Eventually below handle the wait for activation.
	log.Info("==> Creating multicast group")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code test-mc01 --max-bandwidth 10Gbps --owner me
	`})
	require.NoError(t, err)

	// Wait for multicast group to be activated
	// Note: Activator polls every 60 seconds, so we need a timeout > 60s to be safe
	log.Info("==> Waiting for multicast group activation")
	var activatedMC *serviceability.MulticastGroup
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, mc := range data.MulticastGroups {
			if mc.Code == "test-mc01" && mc.Status == serviceability.MulticastGroupStatusActivated {
				activatedMC = &mc
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "multicast group was not activated within timeout")

	log.Info("==> Multicast group activated", "multicast_ip", activatedMC.MulticastIp)

	// Capture snapshot AFTER multicast group creation
	log.Info("==> Capturing ResourceExtension state after multicast group creation")
	afterAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-allocation snapshot")

	// Verify multicast_ip was allocated
	if beforeAlloc.MulticastGroupBlock != nil && afterAlloc.MulticastGroupBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "MulticastGroupBlock", 1) // 1 IP allocated
		require.NoError(t, err, "MulticastGroupBlock allocation verification failed")
		log.Info("MulticastGroupBlock after allocation",
			"allocated", afterAlloc.MulticastGroupBlock.Allocated,
			"available", afterAlloc.MulticastGroupBlock.Available)
	}

	// Delete multicast group to trigger deallocation
	mcPubkey := base58.Encode(activatedMC.PubKey[:])
	log.Info("==> Deleting multicast group to trigger deallocation", "pubkey", mcPubkey)
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero multicast group delete --pubkey " + mcPubkey})
	require.NoError(t, err)

	// Wait for multicast group to be deleted
	log.Info("==> Waiting for multicast group to be closed")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, mc := range data.MulticastGroups {
			if mc.Code == "test-mc01" {
				log.Debug("Multicast group still exists", "status", mc.Status)
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "multicast group was not closed within timeout")

	// Capture snapshot AFTER deallocation
	log.Info("==> Capturing ResourceExtension state after multicast group deletion")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify multicast_ip was returned
	if afterAlloc.MulticastGroupBlock != nil && afterDealloc.MulticastGroupBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "MulticastGroupBlock", 1)
		require.NoError(t, err, "multicast_ip not properly deallocated from MulticastGroupBlock")
		log.Info("MulticastGroupBlock after deallocation",
			"allocated", afterDealloc.MulticastGroupBlock.Allocated,
			"available", afterDealloc.MulticastGroupBlock.Available)
	}

	// Verify full lifecycle
	log.Info("==> Verifying resources returned to pre-allocation state")
	err = verifier.AssertResourcesReturned(beforeAlloc, afterDealloc)
	require.NoError(t, err, "resources were not properly returned to pre-allocation state")

	log.Info("==> Multicast group allocation lifecycle test completed successfully")
}

// TestE2E_MultipleLinks_AllocationLifecycle tests allocation/deallocation with multiple links.
// It verifies that multiple links can be created and deleted without resource leaks.
func TestE2E_MultipleLinks_AllocationLifecycle(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create three devices for multiple links
	log.Info("==> Creating devices for multi-link test")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.100.8/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device create --code test-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.100.2" --dz-prefixes "45.33.100.16/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device create --code test-dz03 --contributor co01 --location lhr --exchange xlhr --public-ip "45.33.100.3" --dz-prefixes "45.33.100.24/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
		doublezero device update --pubkey test-dz02 --max-users 128 2>&1
		doublezero device update --pubkey test-dz03 --max-users 128 2>&1
		doublezero device interface create test-dz01 "Ethernet1" 2>&1
		doublezero device interface create test-dz01 "Ethernet2" 2>&1
		doublezero device interface create test-dz02 "Ethernet1" 2>&1
		doublezero device interface create test-dz02 "Ethernet2" 2>&1
		doublezero device interface create test-dz03 "Ethernet1" 2>&1
	`})
	log.Info("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed")

	// Wait for all interfaces to be unlinked
	log.Info("==> Waiting for interfaces to be unlinked")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}

		unlinkedCount := 0
		for _, device := range data.Devices {
			for _, iface := range device.Interfaces {
				if iface.Status == serviceability.InterfaceStatusUnlinked {
					unlinkedCount++
				}
			}
		}
		return unlinkedCount >= 5 // We created 5 Ethernet interfaces
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Capture snapshot BEFORE link creation
	log.Info("==> Capturing ResourceExtension state before link creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// Create multiple links
	log.Info("==> Creating multiple links")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero link create wan --code "test-dz01:test-dz02" --contributor co01 --side-a test-dz01 --side-a-interface Ethernet1 --side-z test-dz02 --side-z-interface Ethernet1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 10 --jitter-ms 1 --desired-status activated -w
		doublezero link create wan --code "test-dz02:test-dz03" --contributor co01 --side-a test-dz02 --side-a-interface Ethernet2 --side-z test-dz03 --side-z-interface Ethernet1 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 15 --jitter-ms 1 --desired-status activated -w
	`})
	require.NoError(t, err)

	// Wait for both links to be activated
	log.Info("==> Waiting for links to be activated")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		activatedCount := 0
		for _, link := range data.Links {
			if link.Status == serviceability.LinkStatusActivated {
				activatedCount++
			}
		}
		return activatedCount >= 2
	}, 60*time.Second, 2*time.Second, "links were not activated within timeout")

	// Capture snapshot AFTER link creation
	log.Info("==> Capturing ResourceExtension state after link creation")
	afterAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-allocation snapshot")

	// Verify resources were allocated for 2 links
	if beforeAlloc.DeviceTunnelBlock != nil && afterAlloc.DeviceTunnelBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "DeviceTunnelBlock", 4) // 2 links * 2 IPs
		require.NoError(t, err, "DeviceTunnelBlock allocation verification failed")
	}
	if beforeAlloc.LinkIds != nil && afterAlloc.LinkIds != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "LinkIds", 2) // 2 tunnel IDs
		require.NoError(t, err, "LinkIds allocation verification failed")
	}

	// Get link pubkeys for deletion
	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)

	var linkPubkeys []string
	for _, link := range data.Links {
		if link.Code == "test-dz01:test-dz02" || link.Code == "test-dz02:test-dz03" {
			linkPubkeys = append(linkPubkeys, base58.Encode(link.PubKey[:]))
		}
	}

	// Delete both links
	log.Info("==> Deleting links to trigger deallocation")
	for _, pubkey := range linkPubkeys {
		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero link delete --pubkey " + pubkey})
		require.NoError(t, err)
	}

	// Wait for links to be closed
	log.Info("==> Waiting for links to be closed")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "test-dz01:test-dz02" || link.Code == "test-dz02:test-dz03" {
				return false
			}
		}
		return true
	}, 90*time.Second, 2*time.Second, "links were not closed within timeout")

	// Capture snapshot AFTER deallocation
	log.Info("==> Capturing ResourceExtension state after link deletion")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify resources were returned
	if afterAlloc.DeviceTunnelBlock != nil && afterDealloc.DeviceTunnelBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "DeviceTunnelBlock", 4)
		require.NoError(t, err, "tunnel_net not properly deallocated from DeviceTunnelBlock")
	}
	if afterAlloc.LinkIds != nil && afterDealloc.LinkIds != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "LinkIds", 2)
		require.NoError(t, err, "tunnel_id not properly deallocated from LinkIds")
	}

	// Verify full lifecycle
	log.Info("==> Verifying resources returned to pre-allocation state")
	err = verifier.AssertResourcesReturned(beforeAlloc, afterDealloc)
	require.NoError(t, err, "resources were not properly returned to pre-allocation state")

	log.Info("==> Multiple links allocation lifecycle test completed successfully")
}

// TestE2E_Multicast_ReactivationPreservesAllocations is a regression test for Bug #2798.
// It verifies that when a Multicast user subscribes as a publisher and gets re-activated:
// - tunnel_net and tunnel_id remain unchanged (no leak)
// - dz_ip gets allocated from DzPrefixBlock (since publishers list is now non-empty)
// - Resource bitmap allocation counts stay stable (no leaks)
//
// Bug scenario:
// 1. User with Multicast type is activated → allocates tunnel_net, tunnel_id; dz_ip = client_ip
// 2. User subscribes as publisher to multicast group → sets status to Updating
// 3. Activator re-activates user → BUG: would allocate NEW resources instead of keeping existing
//
// The fix preserves existing tunnel_net/tunnel_id and only allocates dz_ip when needed.
func TestE2E_Multicast_ReactivationPreservesAllocations(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Add a real device to the devnet (required for multicast connect to discover devices)
	log.Info("==> Adding device to devnet for multicast test")
	device, err := dn.AddDevice(ctx, devnet.DeviceSpec{
		Code:     "test-dz01",
		Location: "lax",
		Exchange: "xlax",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)

	// Wait for device to be activated and capture device pubkey
	log.Info("==> Waiting for device activation")
	var devicePubkey solana.PublicKey
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, d := range data.Devices {
			if d.Code == "test-dz01" && d.Status == serviceability.DeviceStatusActivated {
				devicePubkey = solana.PublicKeyFromBytes(d.PubKey[:])
				return true
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "device was not activated within timeout")

	// Create multicast group
	log.Info("==> Creating multicast group")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code test-mc01 --max-bandwidth 10Gbps --owner me
	`})
	require.NoError(t, err)

	// Wait for multicast group to be activated
	log.Info("==> Waiting for multicast group activation")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, mc := range data.MulticastGroups {
			if mc.Code == "test-mc01" && mc.Status == serviceability.MulticastGroupStatusActivated {
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "multicast group was not activated within timeout")

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Add client
	log.Info("==> Adding client for multicast publisher")
	client, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Wait for client to get latency results from device
	log.Info("==> Waiting for client to discover device via latency probing")
	err = client.WaitForLatencyResults(ctx, device.ID, 90*time.Second)
	require.NoError(t, err)

	// Set access pass
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Add client to multicast group allowlists (both subscriber and publisher)
	// - subscriber allowlist: needed for initial connection as subscriber
	// - publisher allowlist: needed for later subscription as publisher
	log.Info("==> Adding client to multicast group subscriber and publisher allowlists")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero multicast group allowlist subscriber add --code test-mc01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code test-mc01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Capture snapshot BEFORE user creation
	log.Info("==> Capturing ResourceExtension state before user creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// =========================================================================
	// Phase 1: Initial activation as Multicast user (no publishers yet)
	// =========================================================================
	log.Info("==> Phase 1: Connecting as multicast subscriber (no publishers yet)")
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero connect multicast subscriber test-mc01 --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to connect as multicast subscriber")

	// Wait for user to be activated
	log.Info("==> Waiting for initial user activation")
	var activatedUser *serviceability.User
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if user.Status == serviceability.UserStatusActivated && len(user.Subscribers) > 0 {
				activatedUser = &user
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "user was not activated within timeout")

	// Capture original allocations
	originalTunnelNet := activatedUser.TunnelNet
	originalTunnelId := activatedUser.TunnelId
	originalDzIp := net.IP(activatedUser.DzIp[:]).String()
	clientIP := client.CYOANetworkIP

	log.Info("==> Phase 1 complete: Initial activation",
		"tunnel_id", originalTunnelId,
		"tunnel_net", originalTunnelNet,
		"dz_ip", originalDzIp,
		"client_ip", clientIP)

	// Verify dz_ip equals client_ip (no publishers yet, so no allocation from DzPrefixBlock)
	require.Equal(t, clientIP, originalDzIp, "dz_ip should equal client_ip when no publishers")

	// Capture snapshot AFTER initial activation
	afterInitialAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-initial-activation snapshot")

	// Log initial resource state
	userTunnelBefore := afterInitialAlloc.UserTunnelBlock.Allocated
	tunnelIdsBefore, _ := verifier.FindTunnelIdsForDevice(afterInitialAlloc, devicePubkey)
	dzPrefixBefore := verifier.GetTotalDzPrefixAllocatedForDevice(afterInitialAlloc, devicePubkey)

	log.Info("==> Resource state after initial activation",
		"UserTunnelBlock_allocated", userTunnelBefore,
		"TunnelIds_allocated", tunnelIdsBefore.Allocated,
		"DzPrefixBlock_allocated", dzPrefixBefore)

	// =========================================================================
	// Phase 2: Subscribe as publisher → triggers Updating status
	// =========================================================================
	log.Info("==> Phase 2: Subscribing as publisher to trigger re-activation")

	// Subscribe as publisher using the CLI (adds to publishers list, triggers re-activation)
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero connect multicast publisher test-mc01 --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to subscribe as publisher")

	// =========================================================================
	// Phase 3: Wait for re-activation with publishers
	// Note: The Updating status is transient and may complete very quickly,
	// so we wait directly for Activated status with publishers > 0
	// =========================================================================
	log.Info("==> Phase 3: Waiting for re-activation with publishers")
	var reactivatedUser *serviceability.User
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if len(user.Publishers) > 0 && user.Status == serviceability.UserStatusActivated {
				reactivatedUser = &user
				return true
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "user was not re-activated within timeout")

	// =========================================================================
	// Phase 4: Verify allocations are preserved (REGRESSION TEST)
	// =========================================================================
	log.Info("==> Phase 4: Verifying allocations are preserved after re-activation")

	// Get re-activated user values
	reactivatedTunnelNet := reactivatedUser.TunnelNet
	reactivatedTunnelId := reactivatedUser.TunnelId
	reactivatedDzIp := net.IP(reactivatedUser.DzIp[:]).String()

	log.Info("==> Re-activated user state",
		"tunnel_id", reactivatedTunnelId,
		"tunnel_net", reactivatedTunnelNet,
		"dz_ip", reactivatedDzIp,
		"publishers", len(reactivatedUser.Publishers))

	// CRITICAL: tunnel_net must be unchanged
	require.Equal(t, originalTunnelNet, reactivatedTunnelNet,
		"tunnel_net should be preserved after re-activation (was: %v, now: %v)",
		originalTunnelNet, reactivatedTunnelNet)

	// CRITICAL: tunnel_id must be unchanged
	require.Equal(t, originalTunnelId, reactivatedTunnelId,
		"tunnel_id should be preserved after re-activation (was: %d, now: %d)",
		originalTunnelId, reactivatedTunnelId)

	// dz_ip should now be allocated from DzPrefixBlock (since publishers is non-empty)
	require.NotEqual(t, clientIP, reactivatedDzIp,
		"dz_ip should be allocated from DzPrefixBlock after publisher subscription (was: %s, now: %s)",
		clientIP, reactivatedDzIp)

	// Verify dz_ip is from device's dz_prefix range
	dzPrefixBase := strings.Split(device.DZPrefix, "/")[0]   // e.g., "9.179.159.8" from "9.179.159.8/29"
	dzPrefixParts := strings.Split(dzPrefixBase, ".")[:3]    // e.g., ["9", "179", "159"]
	expectedPrefix := strings.Join(dzPrefixParts, ".") + "." // e.g., "9.179.159."
	require.True(t, strings.HasPrefix(reactivatedDzIp, expectedPrefix),
		"dz_ip should be from device's dz_prefix (%s), got: %s", device.DZPrefix, reactivatedDzIp)

	// Capture snapshot AFTER re-activation
	afterReactivation, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-reactivation snapshot")

	// Verify resource bitmap counts
	userTunnelAfter := afterReactivation.UserTunnelBlock.Allocated
	tunnelIdsAfter, _ := verifier.FindTunnelIdsForDevice(afterReactivation, devicePubkey)
	dzPrefixAfter := verifier.GetTotalDzPrefixAllocatedForDevice(afterReactivation, devicePubkey)

	log.Info("==> Resource state after re-activation",
		"UserTunnelBlock_allocated", userTunnelAfter,
		"TunnelIds_allocated", tunnelIdsAfter.Allocated,
		"DzPrefixBlock_allocated", dzPrefixAfter)

	// UserTunnelBlock should be unchanged (no leak)
	require.Equal(t, userTunnelBefore, userTunnelAfter,
		"UserTunnelBlock allocation count should be unchanged (was: %d, now: %d) - potential leak!",
		userTunnelBefore, userTunnelAfter)

	// TunnelIds should be unchanged (no leak)
	require.Equal(t, tunnelIdsBefore.Allocated, tunnelIdsAfter.Allocated,
		"TunnelIds allocation count should be unchanged (was: %d, now: %d) - potential leak!",
		tunnelIdsBefore.Allocated, tunnelIdsAfter.Allocated)

	// DzPrefixBlock should increase by 1 (new dz_ip allocation)
	require.Equal(t, dzPrefixBefore+1, dzPrefixAfter,
		"DzPrefixBlock should have 1 more allocation for dz_ip (was: %d, now: %d)",
		dzPrefixBefore, dzPrefixAfter)

	// =========================================================================
	// Phase 5: Cleanup - delete user and verify full deallocation
	// =========================================================================
	log.Info("==> Phase 5: Cleanup - deleting user")

	userPubkey := base58.Encode(reactivatedUser.PubKey[:])
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero user delete --pubkey " + userPubkey})
	require.NoError(t, err)

	// Wait for user to be closed
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == userPubkey {
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "user was not closed within timeout")

	// Capture snapshot AFTER deallocation
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify resources returned to pre-allocation state
	log.Info("==> Verifying resources returned to pre-allocation state")

	// Check global pools
	require.Equal(t, beforeAlloc.UserTunnelBlock.Allocated, afterDealloc.UserTunnelBlock.Allocated,
		"UserTunnelBlock not properly returned (before: %d, after: %d)",
		beforeAlloc.UserTunnelBlock.Allocated, afterDealloc.UserTunnelBlock.Allocated)

	// Check device-specific pools
	beforeTunnelIds, _ := verifier.FindTunnelIdsForDevice(beforeAlloc, devicePubkey)
	afterDeallocTunnelIds, _ := verifier.FindTunnelIdsForDevice(afterDealloc, devicePubkey)
	require.Equal(t, beforeTunnelIds.Allocated, afterDeallocTunnelIds.Allocated,
		"TunnelIds not properly returned (before: %d, after: %d)",
		beforeTunnelIds.Allocated, afterDeallocTunnelIds.Allocated)

	beforeDzPrefix := verifier.GetTotalDzPrefixAllocatedForDevice(beforeAlloc, devicePubkey)
	afterDeallocDzPrefix := verifier.GetTotalDzPrefixAllocatedForDevice(afterDealloc, devicePubkey)
	require.Equal(t, beforeDzPrefix, afterDeallocDzPrefix,
		"DzPrefixBlock not properly returned (before: %d, after: %d)",
		beforeDzPrefix, afterDeallocDzPrefix)

	log.Info("==> Multicast re-activation test completed successfully - Bug #2798 regression verified")
}

// TestE2E_LoopbackInterface_AllocationLifecycle tests the full allocation/deallocation lifecycle
// for loopback interface resources. It verifies that when a loopback interface is created and deleted:
// - ip_net is allocated from and returned to DeviceTunnelBlock
// - node_segment_idx is allocated from and returned to SegmentRoutingIds
func TestE2E_LoopbackInterface_AllocationLifecycle(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Activator: devnet.ActivatorSpec{
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create device (without loopback interfaces initially)
	// Note: We don't wait for device activation here because devices may require
	// loopback interfaces to be fully activated. We'll wait for the loopback
	// interface itself to be activated, which implies sufficient device setup.
	log.Info("==> Creating device without loopback interfaces")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.100.8/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
	`})
	log.Info("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed")

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Capture snapshot BEFORE loopback interface creation
	log.Info("==> Capturing ResourceExtension state before loopback interface creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// Log initial state
	if beforeAlloc.DeviceTunnelBlock != nil {
		log.Info("DeviceTunnelBlock before allocation",
			"allocated", beforeAlloc.DeviceTunnelBlock.Allocated,
			"available", beforeAlloc.DeviceTunnelBlock.Available,
			"total", beforeAlloc.DeviceTunnelBlock.Total)
	}
	if beforeAlloc.SegmentRoutingIds != nil {
		log.Info("SegmentRoutingIds before allocation",
			"allocated", beforeAlloc.SegmentRoutingIds.Allocated,
			"available", beforeAlloc.SegmentRoutingIds.Available,
			"total", beforeAlloc.SegmentRoutingIds.Total)
	}

	// Create loopback interface with vpnv4 type (allocates ip_net and node_segment_idx)
	log.Info("==> Creating loopback interface")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero device interface create test-dz01 "Loopback255" --loopback-type vpnv4 -w
	`})
	require.NoError(t, err, "Loopback interface creation failed")

	// Wait for interface to be activated and verify it has allocated resources
	log.Info("==> Waiting for loopback interface activation")
	var activatedInterface *serviceability.Interface
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, device := range data.Devices {
			if device.Code == "test-dz01" {
				for i := range device.Interfaces {
					iface := &device.Interfaces[i]
					if iface.Name == "Loopback255" && iface.Status == serviceability.InterfaceStatusActivated {
						activatedInterface = iface
						return true
					}
				}
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "loopback interface was not activated within timeout")

	log.Info("==> Loopback interface activated",
		"ip_net", activatedInterface.IpNet,
		"node_segment_idx", activatedInterface.NodeSegmentIdx)

	// Verify interface has allocated resources
	require.NotEqual(t, [5]uint8{0, 0, 0, 0, 0}, activatedInterface.IpNet, "ip_net should be allocated (non-zero)")
	require.NotZero(t, activatedInterface.NodeSegmentIdx, "node_segment_idx should be allocated (non-zero)")

	// Capture snapshot AFTER loopback interface creation
	log.Info("==> Capturing ResourceExtension state after loopback interface creation")
	afterAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-allocation snapshot")

	// Verify ip_net was allocated from DeviceTunnelBlock
	// Loopback interface allocates a /32 (1 IP) from DeviceTunnelBlock
	if beforeAlloc.DeviceTunnelBlock != nil && afterAlloc.DeviceTunnelBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "DeviceTunnelBlock", 1)
		require.NoError(t, err, "DeviceTunnelBlock allocation verification failed")
		log.Info("DeviceTunnelBlock after allocation",
			"allocated", afterAlloc.DeviceTunnelBlock.Allocated,
			"available", afterAlloc.DeviceTunnelBlock.Available)
	}

	// Verify node_segment_idx was allocated from SegmentRoutingIds
	if beforeAlloc.SegmentRoutingIds != nil && afterAlloc.SegmentRoutingIds != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "SegmentRoutingIds", 1)
		require.NoError(t, err, "SegmentRoutingIds allocation verification failed")
		log.Info("SegmentRoutingIds after allocation",
			"allocated", afterAlloc.SegmentRoutingIds.Allocated,
			"available", afterAlloc.SegmentRoutingIds.Available)
	}

	// Delete loopback interface to trigger deallocation
	log.Info("==> Deleting loopback interface to trigger deallocation")
	err = dn.DeleteDeviceLoopbackInterface(ctx, "test-dz01", "Loopback255")
	require.NoError(t, err, "Loopback interface deletion failed")

	// Wait for interface to be removed
	log.Info("==> Waiting for loopback interface to be removed")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, device := range data.Devices {
			if device.Code == "test-dz01" {
				for _, iface := range device.Interfaces {
					if iface.Name == "Loopback255" {
						log.Debug("Loopback interface still exists", "status", iface.Status)
						return false
					}
				}
				// Interface not found - it has been removed
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "loopback interface was not removed within timeout")

	// Capture snapshot AFTER deallocation
	log.Info("==> Capturing ResourceExtension state after loopback interface deletion")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify ip_net was returned to DeviceTunnelBlock
	if afterAlloc.DeviceTunnelBlock != nil && afterDealloc.DeviceTunnelBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "DeviceTunnelBlock", 1)
		require.NoError(t, err, "ip_net not properly deallocated from DeviceTunnelBlock")
		log.Info("DeviceTunnelBlock after deallocation",
			"allocated", afterDealloc.DeviceTunnelBlock.Allocated,
			"available", afterDealloc.DeviceTunnelBlock.Available)
	}

	// Verify node_segment_idx was returned to SegmentRoutingIds
	if afterAlloc.SegmentRoutingIds != nil && afterDealloc.SegmentRoutingIds != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "SegmentRoutingIds", 1)
		require.NoError(t, err, "node_segment_idx not properly deallocated from SegmentRoutingIds")
		log.Info("SegmentRoutingIds after deallocation",
			"allocated", afterDealloc.SegmentRoutingIds.Allocated,
			"available", afterDealloc.SegmentRoutingIds.Available)
	}

	// Verify resources returned to pre-allocation state
	log.Info("==> Verifying resources returned to pre-allocation state")
	if beforeAlloc.DeviceTunnelBlock != nil && afterDealloc.DeviceTunnelBlock != nil {
		require.Equal(t, beforeAlloc.DeviceTunnelBlock.Allocated, afterDealloc.DeviceTunnelBlock.Allocated,
			"DeviceTunnelBlock: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
			beforeAlloc.DeviceTunnelBlock.Allocated, afterDealloc.DeviceTunnelBlock.Allocated)
	}
	if beforeAlloc.SegmentRoutingIds != nil && afterDealloc.SegmentRoutingIds != nil {
		require.Equal(t, beforeAlloc.SegmentRoutingIds.Allocated, afterDealloc.SegmentRoutingIds.Allocated,
			"SegmentRoutingIds: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
			beforeAlloc.SegmentRoutingIds.Allocated, afterDealloc.SegmentRoutingIds.Allocated)
	}

	log.Info("==> Loopback interface allocation lifecycle test completed successfully")
}
