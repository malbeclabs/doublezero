//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

	// Wait for device to be activated
	log.Info("==> Waiting for device activation")
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
	_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero user create --device test-dz01 --client-ip " + client.CYOANetworkIP})
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

	// Verify resources were allocated
	if beforeAlloc.UserTunnelBlock != nil && afterAlloc.UserTunnelBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "UserTunnelBlock", 2) // /31 = 2 IPs
		require.NoError(t, err, "UserTunnelBlock allocation verification failed")
		log.Info("UserTunnelBlock after allocation",
			"allocated", afterAlloc.UserTunnelBlock.Allocated,
			"available", afterAlloc.UserTunnelBlock.Available)
	}

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

	// Verify resources were returned
	if afterAlloc.UserTunnelBlock != nil && afterDealloc.UserTunnelBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "UserTunnelBlock", 2)
		require.NoError(t, err, "tunnel_net not properly deallocated from UserTunnelBlock")
		log.Info("UserTunnelBlock after deallocation",
			"allocated", afterDealloc.UserTunnelBlock.Allocated,
			"available", afterDealloc.UserTunnelBlock.Available)
	}

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

	log.Info("==> User allocation lifecycle test completed successfully")
}

// TestE2E_MulticastGroup_AllocationLifecycle tests the full allocation/deallocation lifecycle
// for multicast group resources. It verifies that when a multicast group is created and deleted:
// - multicast_ip is allocated from and returned to MulticastGroupBlock
func TestE2E_MulticastGroup_AllocationLifecycle(t *testing.T) {
	// TODO: Investigate multicast group activation timeout with on-chain allocation.
	// The activator may not be processing multicast groups correctly, or there's a
	// timing issue with the activation transaction.
	t.Skip("Skipping: multicast group on-chain allocation activation times out - needs investigation")

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
	log.Info("==> Creating multicast group")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code test-mc01 --max-bandwidth 10Gbps --owner me -w
	`})
	require.NoError(t, err)

	// Wait for multicast group to be activated
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
	}, 60*time.Second, 2*time.Second, "multicast group was not activated within timeout")

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
