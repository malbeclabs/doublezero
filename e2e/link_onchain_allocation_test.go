//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/allocation"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_Link_OnchainAllocation tests that links are activated with on-chain resource allocation.
// When the activator runs with --onchain-allocation flag, it should allocate tunnel_id and tunnel_net
// from the ResourceExtension bitmaps on-chain, rather than from local allocators.
func TestE2E_Link_OnchainAllocation(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	// Create devnet with on-chain allocation enabled for the activator
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
			OnchainAllocation: devnet.BoolPtr(true), // Enable on-chain resource allocation
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create two devices for the link endpoints
	// Note: Must use globally routable IPs - smart contract rejects private, documentation, and other reserved IPs
	log.Debug("==> Creating devices for link test")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.100.8/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device create --code test-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.100.2" --dz-prefixes "45.33.100.16/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
		doublezero device update --pubkey test-dz02 --max-users 128 2>&1
	`})
	log.Debug("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed with output: %s", string(output))

	// Create interfaces on the devices (no CYOA/DIA assignment so they can be used for WAN links)
	log.Debug("==> Creating device interfaces")
	output, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device interface create test-dz01 "Ethernet1" 2>&1
		doublezero device interface create test-dz02 "Ethernet1" 2>&1
	`})
	log.Debug("Interface creation output", "output", string(output))
	require.NoError(t, err)

	// Wait for the activator to unlink the interfaces (physical interfaces start as Pending,
	// activator transitions them to Unlinked so they can be used for links)
	log.Debug("==> Waiting for interfaces to be unlinked by activator")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			log.Debug("Failed to get serviceability client", "error", err)
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			log.Debug("Failed to get program data", "error", err)
			return false
		}

		// Track whether we found both devices with their interfaces
		dz01Found := false
		dz02Found := false

		for _, device := range data.Devices {
			if device.Code == "test-dz01" {
				for _, iface := range device.Interfaces {
					if iface.Name == "Ethernet1" {
						if iface.Status == serviceability.InterfaceStatusUnlinked {
							dz01Found = true
						} else {
							log.Debug("Interface not yet unlinked", "device", device.Code, "interface", iface.Name, "status", iface.Status)
							return false
						}
					}
				}
			}
			if device.Code == "test-dz02" {
				for _, iface := range device.Interfaces {
					if iface.Name == "Ethernet1" {
						if iface.Status == serviceability.InterfaceStatusUnlinked {
							dz02Found = true
						} else {
							log.Debug("Interface not yet unlinked", "device", device.Code, "interface", iface.Name, "status", iface.Status)
							return false
						}
					}
				}
			}
		}

		if !dz01Found || !dz02Found {
			log.Debug("Waiting for interfaces to appear", "dz01Found", dz01Found, "dz02Found", dz02Found)
			return false
		}

		return true
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create allocation verifier and capture snapshot BEFORE link creation
	client, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(client)

	log.Debug("==> Capturing ResourceExtension state before link creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// Log initial state for debugging
	if beforeAlloc.DeviceTunnelBlock != nil {
		log.Debug("DeviceTunnelBlock before allocation",
			"allocated", beforeAlloc.DeviceTunnelBlock.Allocated,
			"available", beforeAlloc.DeviceTunnelBlock.Available,
			"total", beforeAlloc.DeviceTunnelBlock.Total)
	}
	if beforeAlloc.LinkIds != nil {
		log.Debug("LinkIds before allocation",
			"allocated", beforeAlloc.LinkIds.Allocated,
			"available", beforeAlloc.LinkIds.Available,
			"total", beforeAlloc.LinkIds.Total)
	}

	// Create a link between the two devices with desired-status activated
	// The activator should pick this up and activate it with on-chain allocation
	log.Debug("==> Creating link with on-chain allocation")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero link create wan \
			--code "test-dz01:test-dz02" \
			--contributor co01 \
			--side-a test-dz01 \
			--side-a-interface Ethernet1 \
			--side-z test-dz02 \
			--side-z-interface Ethernet1 \
			--bandwidth "10 Gbps" \
			--mtu 9000 \
			--delay-ms 10 \
			--jitter-ms 1 \
			--desired-status activated \
			-w
	`})
	require.NoError(t, err)

	// Wait for the link to be activated
	log.Debug("==> Waiting for link activation")
	var activatedLink *serviceability.Link
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			log.Debug("Failed to get serviceability client", "error", err)
			return false
		}

		data, err := client.GetProgramData(ctx)
		if err != nil {
			log.Debug("Failed to get program data", "error", err)
			return false
		}

		for _, link := range data.Links {
			if link.Code == "test-dz01:test-dz02" {
				if link.Status == serviceability.LinkStatusActivated {
					activatedLink = &link
					return true
				}
				log.Debug("Link found but not yet activated", "status", link.Status)
				return false
			}
		}
		log.Debug("Link not found yet")
		return false
	}, 60*time.Second, 2*time.Second, "link was not activated within timeout")

	// Verify the link has allocated resources from on-chain
	// Note: tunnel_id=0 is valid (first allocation from LinkIds range 0-65535)
	// We verify tunnel_net is not default, which confirms on-chain allocation worked
	log.Debug("==> Verifying link has allocated resources", "tunnel_id", activatedLink.TunnelId, "tunnel_net", activatedLink.TunnelNet)
	require.NotEmpty(t, activatedLink.TunnelNet, "tunnel_net should be allocated (non-empty)")
	require.NotEqual(t, [5]uint8{0, 0, 0, 0, 0}, activatedLink.TunnelNet, "tunnel_net should not be default/zero")

	// Capture snapshot AFTER allocation to verify resources were consumed
	log.Debug("==> Capturing ResourceExtension state after link activation")
	afterAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-allocation snapshot")

	// Verify resources were allocated from the pools
	// Link allocates: tunnel_net (2 IPs from /31) from DeviceTunnelBlock, tunnel_id (1) from LinkIds
	if beforeAlloc.DeviceTunnelBlock != nil && afterAlloc.DeviceTunnelBlock != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "DeviceTunnelBlock", 2)
		require.NoError(t, err, "DeviceTunnelBlock allocation verification failed")
		log.Debug("DeviceTunnelBlock after allocation",
			"allocated", afterAlloc.DeviceTunnelBlock.Allocated,
			"available", afterAlloc.DeviceTunnelBlock.Available)
	}
	if beforeAlloc.LinkIds != nil && afterAlloc.LinkIds != nil {
		err = verifier.AssertAllocated(beforeAlloc, afterAlloc, "LinkIds", 1)
		require.NoError(t, err, "LinkIds allocation verification failed")
		log.Debug("LinkIds after allocation",
			"allocated", afterAlloc.LinkIds.Allocated,
			"available", afterAlloc.LinkIds.Available)
	}

	// Verify link details via CLI
	log.Debug("==> Verifying link details via CLI")
	output, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero link get --code test-dz01:test-dz02"})
	require.NoError(t, err)
	outputStr := string(output)
	require.Contains(t, outputStr, "status: activated", "link should show activated status")

	// === Part 2: Test on-chain deallocation ===
	// The closeaccount handler should deallocate tunnel_id and tunnel_net back to ResourceExtension

	// Log allocated resources before deletion (for debugging/audit purposes)
	log.Debug("==> Allocated resources before deletion", "tunnel_id", activatedLink.TunnelId, "tunnel_net", activatedLink.TunnelNet)

	// Drain link before deletion (delete not allowed from Activated status)
	linkPubkey := base58.Encode(activatedLink.PubKey[:])
	log.Debug("==> Draining link before deletion", "pubkey", linkPubkey)
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", fmt.Sprintf(`
		set -euo pipefail
		doublezero link update --pubkey "%s" --status soft-drained
	`, linkPubkey)})
	require.NoError(t, err)

	// Delete link to trigger transition to Deleting status
	log.Debug("==> Deleting link to trigger deallocation", "pubkey", linkPubkey)
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", fmt.Sprintf(`
		set -euo pipefail
		doublezero link delete --pubkey "%s"
	`, linkPubkey)})
	require.NoError(t, err)

	// Wait for link to transition to Deleting status
	// Note: Go SDK uses LinkStatusDeleting (value 3) which corresponds to Rust's Deleting status
	log.Debug("==> Waiting for link to transition to Deleting")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "test-dz01:test-dz02" {
				// LinkStatusDeleting in Go SDK = Deleting in Rust (value 3)
				if link.Status == serviceability.LinkStatusDeleting {
					return true
				}
				log.Debug("Link not yet in Deleting status", "status", link.Status)
				return false
			}
		}
		// Link not found means it was already closed
		return true
	}, 60*time.Second, 2*time.Second, "link did not transition to Deleting within timeout")

	// Wait for link to be closed (removed from program data)
	// Note: We don't capture a snapshot here because the link may close very quickly,
	// causing a race where we capture the snapshot after deallocation already happened.
	// Instead, we use afterAlloc (captured after link creation) as the baseline.
	log.Debug("==> Waiting for link to be closed by activator")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "test-dz01:test-dz02" {
				log.Debug("Link still exists", "status", link.Status)
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "link was not closed within timeout")

	// Capture snapshot AFTER link is closed to verify deallocation
	log.Debug("==> Capturing ResourceExtension state after link closure")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify resources were deallocated back to the pools
	// Use afterAlloc as baseline since beforeDealloc may miss the window due to fast link closure
	if afterAlloc.DeviceTunnelBlock != nil && afterDealloc.DeviceTunnelBlock != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "DeviceTunnelBlock", 2)
		require.NoError(t, err, "tunnel_net not properly deallocated from DeviceTunnelBlock")
		log.Debug("DeviceTunnelBlock after deallocation",
			"allocated", afterDealloc.DeviceTunnelBlock.Allocated,
			"available", afterDealloc.DeviceTunnelBlock.Available)
	}
	if afterAlloc.LinkIds != nil && afterDealloc.LinkIds != nil {
		err = verifier.AssertDeallocated(afterAlloc, afterDealloc, "LinkIds", 1)
		require.NoError(t, err, "tunnel_id not properly deallocated from LinkIds")
		log.Debug("LinkIds after deallocation",
			"allocated", afterDealloc.LinkIds.Allocated,
			"available", afterDealloc.LinkIds.Available)
	}

	// Verify resources returned to pre-allocation state
	log.Debug("==> Verifying resources returned to pre-allocation state")
	err = verifier.AssertResourcesReturned(beforeAlloc, afterDealloc)
	require.NoError(t, err, "resources were not properly returned to pre-allocation state")

	// Verify interfaces are back to Unlinked status after link closure
	log.Debug("==> Verifying interfaces returned to Unlinked status")
	require.Eventually(t, func() bool {
		client, err := dn.Ledger.GetServiceabilityClient()
		if err != nil {
			return false
		}
		data, err := client.GetProgramData(ctx)
		if err != nil {
			return false
		}

		dz01Unlinked := false
		dz02Unlinked := false

		for _, device := range data.Devices {
			if device.Code == "test-dz01" {
				for _, iface := range device.Interfaces {
					if iface.Name == "Ethernet1" && iface.Status == serviceability.InterfaceStatusUnlinked {
						dz01Unlinked = true
					}
				}
			}
			if device.Code == "test-dz02" {
				for _, iface := range device.Interfaces {
					if iface.Name == "Ethernet1" && iface.Status == serviceability.InterfaceStatusUnlinked {
						dz02Unlinked = true
					}
				}
			}
		}

		if !dz01Unlinked || !dz02Unlinked {
			log.Debug("Interfaces not yet unlinked", "dz01", dz01Unlinked, "dz02", dz02Unlinked)
			return false
		}
		return true
	}, 30*time.Second, 2*time.Second, "interfaces did not return to Unlinked status")

	log.Debug("==> Link on-chain allocation and deallocation test completed successfully")
}
