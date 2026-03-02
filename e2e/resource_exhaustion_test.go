//go:build e2e

package e2e_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/allocation"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_DzPrefix_ResourceExhaustion tests the behavior when a device's DzPrefix IP pool
// is fully exhausted. It verifies that:
//   - Users consume IPs until the pool is full
//   - When the pool is exhausted, new users remain stuck in Pending (not rejected)
//   - The activator continues to function normally after allocation failures
//   - Freeing a slot (deleting a user) allows the stuck user to be activated
//   - All resources return to baseline after cleanup
//
// This covers a gap in PR #2848 (stateless activator mode): when onchain resource allocation
// fails (AllocationFailed error code 63), the activator logs the error but does NOT reject
// the user. The user stays in Pending indefinitely with no retry signal.
//
// Test uses a single /30 prefix (4 IPs total, 3 usable after loopback reservation):
//   - 45.33.105.0/30: loopback=.0 (pre-reserved), usable=.1, .2, .3
func TestE2E_DzPrefix_ResourceExhaustion(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Starting test devnet with on-chain allocation enabled")

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

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
			OnchainAllocation: devnet.BoolPtr(true),
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create device with a single /30 prefix — only 3 usable IPs after loopback reservation.
	log.Debug("==> Creating device with single /30 dz_prefix for exhaustion testing")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.104.1" --dz-prefixes "45.33.105.0/30" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
		doublezero device interface create test-dz01 "Loopback255" --loopback-type vpnv4 --bandwidth 10G -w
		doublezero device interface create test-dz01 "Loopback256" --loopback-type ipv4 --bandwidth 10G -w
	`})
	log.Debug("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed")

	// Wait for device activation.
	log.Debug("==> Waiting for device activation")
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

	// Create allocation verifier.
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Wait for DzPrefixBlock to be created by the activator, then capture baseline.
	log.Debug("==> Waiting for DzPrefixBlock creation and capturing baseline")
	var baseline *allocation.ResourceSnapshot
	require.Eventually(t, func() bool {
		snap, err := verifier.CaptureSnapshot(ctx)
		if err != nil {
			return false
		}
		blocks := verifier.FindDzPrefixBlocksForDevice(snap, devicePubkey)
		if len(blocks) == 1 {
			baseline = snap
			return true
		}
		log.Debug("Waiting for DzPrefixBlock creation", "count", len(blocks))
		return false
	}, 60*time.Second, 2*time.Second, "expected 1 DzPrefixBlock after device activation")

	initialDzPrefixAllocated := verifier.GetTotalDzPrefixAllocatedForDevice(baseline, devicePubkey)
	log.Debug("Baseline DzPrefix state",
		"device", devicePubkey.String()[:8]+"...",
		"total_allocated", initialDzPrefixAllocated)

	// Helper types and functions.
	type userInfo struct {
		client *devnet.Client
		pubkey string
		dzIP   string
	}

	createUserOnchain := func(t *testing.T, hostID uint32) *devnet.Client {
		t.Helper()
		client, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: hostID,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
		require.NoError(t, err)

		_, err = client.Exec(ctx, []string{"bash", "-c",
			"doublezero user create --device test-dz01 --client-ip " + client.CYOANetworkIP + " --allocate-addr"})
		require.NoError(t, err)

		return client
	}

	waitForUserActivated := func(t *testing.T, clientIP string) *serviceability.User {
		t.Helper()
		var activated *serviceability.User
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if net.IP(user.ClientIp[:]).String() == clientIP && user.Status == serviceability.UserStatusActivated {
					activated = &user
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "user with client IP %s was not activated within timeout", clientIP)
		return activated
	}

	dzPrefix := "45.33.105.0/30"
	var fillUsers []userInfo
	var stuckClient *devnet.Client

	// =========================================================================
	// Subtest 1: Fill the pool — create 3 users to exhaust all usable IPs.
	// =========================================================================
	if !t.Run("fill_pool", func(t *testing.T) {
		log.Debug("==> Creating 3 users to fill the /30 prefix pool")

		for i := 1; i <= 3; i++ {
			log.Debug("==> Creating fill user", "number", i)
			client := createUserOnchain(t, uint32(99+i)) // host IDs: 100, 101, 102

			activated := waitForUserActivated(t, client.CYOANetworkIP)
			pubkey := base58.Encode(activated.PubKey[:])
			dzIP := net.IP(activated.DzIp[:]).String()

			log.Debug("==> Fill user activated", "number", i, "pubkey", pubkey, "dz_ip", dzIP)

			// Verify IP is from the /30 prefix.
			_, network, err := net.ParseCIDR(dzPrefix)
			require.NoError(t, err)
			require.True(t, network.Contains(net.ParseIP(dzIP)),
				"user %d dz_ip (%s) should be from prefix %s", i, dzIP, dzPrefix)

			fillUsers = append(fillUsers, userInfo{client: client, pubkey: pubkey, dzIP: dzIP})
		}

		// Verify all 3 IPs allocated from DzPrefix.
		afterFill, err := verifier.CaptureSnapshot(ctx)
		require.NoError(t, err)
		dzPrefixAfterFill := verifier.GetTotalDzPrefixAllocatedForDevice(afterFill, devicePubkey)
		log.Debug("DzPrefix state after filling pool",
			"total_allocated", dzPrefixAfterFill,
			"initial_allocated", initialDzPrefixAllocated,
			"delta", dzPrefixAfterFill-initialDzPrefixAllocated)
		require.Equal(t, initialDzPrefixAllocated+3, dzPrefixAfterFill,
			"Expected 3 additional IPs allocated from DzPrefixBlock after filling pool")

		log.Debug("==> Pool filled — all usable IPs in the /30 prefix are allocated")
	}) {
		t.FailNow()
	}

	// =========================================================================
	// Subtest 2: Exhaustion blocks activation — 4th user stays Pending.
	// The activator encounters AllocationFailed (error 63) but does NOT reject
	// the user, leaving it stuck in Pending indefinitely.
	// =========================================================================
	if !t.Run("exhaustion_blocks_activation", func(t *testing.T) {
		log.Debug("==> Creating 4th user — expect stuck in Pending (pool exhausted)")
		stuckClient = createUserOnchain(t, 103)

		// The user should remain in Pending because the DzPrefix pool is exhausted.
		log.Debug("==> Verifying 4th user remains in Pending state")
		require.Never(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if net.IP(user.ClientIp[:]).String() == stuckClient.CYOANetworkIP &&
					user.Status == serviceability.UserStatusActivated {
					return true
				}
			}
			return false
		}, 30*time.Second, 3*time.Second, "4th user should NOT be activated when DzPrefix pool is exhausted")

		// Confirm: user exists onchain in Pending state.
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		var foundPending bool
		for _, user := range data.Users {
			if net.IP(user.ClientIp[:]).String() == stuckClient.CYOANetworkIP {
				require.Equal(t, serviceability.UserStatusPending, user.Status,
					"exhausted user should be in Pending state, got %v", user.Status)
				foundPending = true
				break
			}
		}
		require.True(t, foundPending, "4th user should exist onchain in Pending state")

		// Verify DzPrefix allocation unchanged — no new IPs allocated.
		afterExhaustion, err := verifier.CaptureSnapshot(ctx)
		require.NoError(t, err)
		dzPrefixAfterExhaustion := verifier.GetTotalDzPrefixAllocatedForDevice(afterExhaustion, devicePubkey)
		require.Equal(t, initialDzPrefixAllocated+3, dzPrefixAfterExhaustion,
			"DzPrefix allocation count should not increase when pool is exhausted")

		log.Debug("==> Confirmed: 4th user stuck in Pending, DzPrefix pool fully exhausted")
	}) {
		t.FailNow()
	}

	// =========================================================================
	// Subtest 3: Activator still healthy — verify it hasn't crashed after
	// repeated AllocationFailed errors.
	// =========================================================================
	if !t.Run("activator_still_healthy", func(t *testing.T) {
		log.Debug("==> Verifying activator is still operational after AllocationFailed errors")

		// If the activator crashed, the 3 fill users would not still be in
		// Activated state and the stuck user wouldn't still be Pending.
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)

		activatedCount := 0
		pendingCount := 0
		for _, user := range data.Users {
			switch user.Status {
			case serviceability.UserStatusActivated:
				activatedCount++
			case serviceability.UserStatusPending:
				pendingCount++
			}
		}
		require.Equal(t, 3, activatedCount, "all 3 fill users should still be Activated")
		require.Equal(t, 1, pendingCount, "stuck user should still be Pending")

		log.Debug("==> Activator confirmed healthy — 3 Activated, 1 Pending")
	}) {
		t.FailNow()
	}

	// =========================================================================
	// Subtest 4: Recovery — free a slot by deleting a fill user, then verify
	// the stuck user gets activated on the activator's next retry.
	// =========================================================================
	if !t.Run("recovery_after_freeing_slot", func(t *testing.T) {
		freedUser := fillUsers[0]
		log.Debug("==> Deleting first fill user to free a DzPrefix slot", "pubkey", freedUser.pubkey)

		_, err := freedUser.client.Exec(ctx, []string{"bash", "-c",
			"doublezero user delete --pubkey " + freedUser.pubkey})
		require.NoError(t, err)

		// Wait for the deleted user to be fully closed (IP deallocated).
		log.Debug("==> Waiting for deleted user to be closed")
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if base58.Encode(user.PubKey[:]) == freedUser.pubkey {
					return false
				}
			}
			return true
		}, 90*time.Second, 2*time.Second, "deleted user was not closed within timeout")

		// The stuck user should now get activated as the activator retries.
		log.Debug("==> Waiting for previously stuck user to be activated")
		activated := waitForUserActivated(t, stuckClient.CYOANetworkIP)
		recoveredDzIP := net.IP(activated.DzIp[:]).String()

		log.Debug("==> Previously stuck user now activated", "dz_ip", recoveredDzIP)

		// Verify the recovered user got an IP from the /30 prefix.
		_, network, err := net.ParseCIDR(dzPrefix)
		require.NoError(t, err)
		require.True(t, network.Contains(net.ParseIP(recoveredDzIP)),
			"recovered user dz_ip (%s) should be from prefix %s", recoveredDzIP, dzPrefix)

		log.Debug("==> Recovery verified: stuck user activated after freeing a slot")
	}) {
		t.FailNow()
	}

	// =========================================================================
	// Subtest 5: Cleanup — delete all remaining users and verify resources
	// return to baseline.
	// =========================================================================
	t.Run("cleanup_returns_to_baseline", func(t *testing.T) {
		log.Debug("==> Deleting all remaining users to verify resource cleanup")

		// Delete remaining fill users (skip [0], already deleted in recovery).
		for _, fillUser := range fillUsers[1:] {
			log.Debug("==> Deleting fill user", "pubkey", fillUser.pubkey)
			_, err := fillUser.client.Exec(ctx, []string{"bash", "-c",
				"doublezero user delete --pubkey " + fillUser.pubkey})
			require.NoError(t, err)
		}

		// Delete the recovered stuck user.
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		for _, user := range data.Users {
			if net.IP(user.ClientIp[:]).String() == stuckClient.CYOANetworkIP {
				pubkey := base58.Encode(user.PubKey[:])
				log.Debug("==> Deleting recovered stuck user", "pubkey", pubkey)
				_, err := stuckClient.Exec(ctx, []string{"bash", "-c",
					"doublezero user delete --pubkey " + pubkey})
				require.NoError(t, err)
				break
			}
		}

		// Wait for all users to be closed.
		log.Debug("==> Waiting for all users to be closed")
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			return len(data.Users) == 0
		}, 90*time.Second, 2*time.Second, "users were not all closed within timeout")

		// Verify DzPrefix allocations returned to baseline.
		afterCleanup, err := verifier.CaptureSnapshot(ctx)
		require.NoError(t, err)

		dzPrefixAfterCleanup := verifier.GetTotalDzPrefixAllocatedForDevice(afterCleanup, devicePubkey)
		log.Debug("DzPrefix state after cleanup",
			"total_allocated", dzPrefixAfterCleanup,
			"initial_allocated", initialDzPrefixAllocated)
		require.Equal(t, initialDzPrefixAllocated, dzPrefixAfterCleanup,
			"DzPrefixBlock allocations should return to baseline after all users deleted")

		// Verify TunnelIds returned to baseline.
		beforeTunnelIds, err := verifier.FindTunnelIdsForDevice(baseline, devicePubkey)
		require.NoError(t, err)
		afterTunnelIds, err := verifier.FindTunnelIdsForDevice(afterCleanup, devicePubkey)
		require.NoError(t, err)
		require.Equal(t, beforeTunnelIds.Allocated, afterTunnelIds.Allocated,
			"TunnelIds should return to baseline (initial=%d, after=%d)",
			beforeTunnelIds.Allocated, afterTunnelIds.Allocated)

		// Verify global UserTunnelBlock returned to baseline.
		if baseline.UserTunnelBlock != nil && afterCleanup.UserTunnelBlock != nil {
			require.Equal(t, baseline.UserTunnelBlock.Allocated, afterCleanup.UserTunnelBlock.Allocated,
				"UserTunnelBlock should return to baseline (initial=%d, after=%d)",
				baseline.UserTunnelBlock.Allocated, afterCleanup.UserTunnelBlock.Allocated)
		}

		log.Debug("==> Cleanup verified — all resources returned to baseline")
	})
}
