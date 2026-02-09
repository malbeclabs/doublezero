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

// TestE2E_DzPrefix_RolloverAllocation tests the rollover allocation behavior when a device's
// first dz_prefix is fully allocated. It verifies that:
// - Users are initially allocated from the first dz_prefix
// - When the first prefix is exhausted, subsequent users get IPs from the next available prefix
// - All allocations are properly deallocated when users are deleted
//
// Test uses two /30 prefixes (2 usable IPs each, .0 and .1 are pre-reserved for device tunnel endpoints):
// - Prefix 1: 45.33.101.0/30 → usable IPs: .2, .3 (.0 and .1 reserved)
// - Prefix 2: 45.33.101.4/30 → usable IPs: .6, .7 (.4 and .5 reserved)
func TestE2E_DzPrefix_RolloverAllocation(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Starting test devnet with on-chain allocation enabled")

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
			OnchainAllocation: devnet.BoolPtr(true),
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create device with two /30 prefixes for rollover testing
	// /30 prefix has 4 IPs total, with first 2 reserved for device tunnel endpoints
	// Using public IPs (smart contract requires global unicast addresses)
	// - 45.33.101.0/30: reserved=.0,.1, usable=.2,.3
	// - 45.33.101.4/30: reserved=.4,.5, usable=.6,.7
	log.Debug("==> Creating device with two /30 dz_prefixes for rollover testing")
	output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		echo "==> Creating device with multiple dz_prefixes"
		doublezero device create --code test-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.101.0/30,45.33.101.4/30" --mgmt-vrf mgmt --desired-status activated 2>&1
		doublezero device update --pubkey test-dz01 --max-users 128 2>&1
		doublezero device interface create test-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create test-dz01 "Loopback256" --loopback-type ipv4 -w
	`})
	log.Debug("Device creation output", "output", string(output))
	require.NoError(t, err, "Device creation failed")

	// Wait for device to be activated and capture device pubkey
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

	// Create allocation verifier
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	verifier := allocation.NewVerifier(serviceabilityClient)

	// Capture snapshot BEFORE any user creation
	log.Debug("==> Capturing ResourceExtension state before user creation")
	beforeAlloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture pre-allocation snapshot")

	// Log initial DzPrefix state
	initialDzPrefixAllocated := verifier.GetTotalDzPrefixAllocatedForDevice(beforeAlloc, devicePubkey)
	log.Debug("Initial DzPrefix allocation state",
		"device", devicePubkey.String()[:8]+"...",
		"total_allocated", initialDzPrefixAllocated)

	// Helper function to check if an IP is within a CIDR
	ipInCIDR := func(ipStr string, cidr string) bool {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return false
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}
		return network.Contains(ip)
	}

	// Define the two prefixes
	prefix1 := "45.33.101.0/30" // Usable: .2, .3
	prefix2 := "45.33.101.4/30" // Usable: .6, .7

	// =========================================================================
	// Phase 1: Create first two users (should exhaust first prefix)
	// =========================================================================
	log.Debug("==> Phase 1: Creating first two users to exhaust first prefix")

	type userInfo struct {
		client *devnet.Client
		pubkey string
		dzIP   string
	}
	var users []userInfo

	for i := 1; i <= 2; i++ {
		log.Debug("==> Creating user", "number", i)

		client, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: uint32(99 + i), // 100, 101
		})
		require.NoError(t, err)

		// Set access pass
		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
		require.NoError(t, err)

		// Create user with allocated IP
		_, err = client.Exec(ctx, []string{"bash", "-c", "doublezero user create --device test-dz01 --client-ip " + client.CYOANetworkIP + " --allocate-addr"})
		require.NoError(t, err)

		// Wait for user to be activated - match by ClientIp to this specific client
		log.Debug("==> Waiting for user activation", "number", i, "clientIP", client.CYOANetworkIP)
		var activatedUser *serviceability.User
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			// Find user by matching ClientIp to this client's CYOANetworkIP
			for _, user := range data.Users {
				userClientIP := net.IP(user.ClientIp[:]).String()
				if userClientIP == client.CYOANetworkIP && user.Status == serviceability.UserStatusActivated {
					activatedUser = &user
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "user %d was not activated within timeout", i)

		userPubkey := base58.Encode(activatedUser.PubKey[:])
		dzIP := net.IP(activatedUser.DzIp[:]).String()

		log.Debug("==> User activated",
			"number", i,
			"pubkey", userPubkey,
			"dz_ip", dzIP)

		users = append(users, userInfo{
			client: client,
			pubkey: userPubkey,
			dzIP:   dzIP,
		})

		// Verify this user got an IP from the first prefix
		require.True(t, ipInCIDR(dzIP, prefix1),
			"User %d dz_ip (%s) should be from first prefix (%s)", i, dzIP, prefix1)
		log.Debug("==> Verified user IP is from first prefix", "number", i, "dz_ip", dzIP, "prefix", prefix1)
	}

	// Verify first prefix is now exhausted (2 usable IPs allocated)
	afterFirstTwoUsers, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err)
	dzPrefixAfterTwo := verifier.GetTotalDzPrefixAllocatedForDevice(afterFirstTwoUsers, devicePubkey)
	log.Debug("DzPrefix state after first two users",
		"total_allocated", dzPrefixAfterTwo,
		"expected_delta", 2)
	require.Equal(t, initialDzPrefixAllocated+2, dzPrefixAfterTwo,
		"Expected 2 IPs allocated from DzPrefixBlocks after creating 2 users")

	// =========================================================================
	// Phase 2: Create third user (should trigger rollover to second prefix)
	// =========================================================================
	log.Debug("==> Phase 2: Creating third user to trigger rollover to second prefix")

	client3, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 102,
	})
	require.NoError(t, err)

	// Set access pass
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client3.CYOANetworkIP + " --user-payer " + client3.Pubkey})
	require.NoError(t, err)

	// Create user with allocated IP
	_, err = client3.Exec(ctx, []string{"bash", "-c", "doublezero user create --device test-dz01 --client-ip " + client3.CYOANetworkIP + " --allocate-addr"})
	require.NoError(t, err)

	// Wait for third user to be activated - match by ClientIp
	log.Debug("==> Waiting for third user activation", "clientIP", client3.CYOANetworkIP)
	var user3Activated *serviceability.User
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			userClientIP := net.IP(user.ClientIp[:]).String()
			if userClientIP == client3.CYOANetworkIP && user.Status == serviceability.UserStatusActivated {
				user3Activated = &user
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "third user was not activated within timeout")

	user3Pubkey := base58.Encode(user3Activated.PubKey[:])
	user3DzIP := net.IP(user3Activated.DzIp[:]).String()

	log.Debug("==> Third user activated",
		"pubkey", user3Pubkey,
		"dz_ip", user3DzIP)

	users = append(users, userInfo{
		client: client3,
		pubkey: user3Pubkey,
		dzIP:   user3DzIP,
	})

	// CRITICAL: Verify third user got an IP from the SECOND prefix (rollover)
	require.True(t, ipInCIDR(user3DzIP, prefix2),
		"Third user dz_ip (%s) should be from second prefix (%s) - rollover failed!", user3DzIP, prefix2)
	require.False(t, ipInCIDR(user3DzIP, prefix1),
		"Third user dz_ip (%s) should NOT be from first prefix (%s)", user3DzIP, prefix1)
	log.Debug("==> ROLLOVER VERIFIED: Third user IP is from second prefix", "dz_ip", user3DzIP, "prefix", prefix2)

	// Verify total allocation count
	afterThirdUser, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err)
	dzPrefixAfterThree := verifier.GetTotalDzPrefixAllocatedForDevice(afterThirdUser, devicePubkey)
	log.Debug("DzPrefix state after third user",
		"total_allocated", dzPrefixAfterThree,
		"expected_delta", 3)
	require.Equal(t, initialDzPrefixAllocated+3, dzPrefixAfterThree,
		"Expected 3 IPs allocated from DzPrefixBlocks after creating 3 users")

	// =========================================================================
	// Phase 3: Delete all users and verify deallocation
	// =========================================================================
	log.Debug("==> Phase 3: Deleting all users to verify deallocation")

	for i, user := range users {
		log.Debug("==> Deleting user", "number", i+1, "pubkey", user.pubkey)
		_, err = user.client.Exec(ctx, []string{"bash", "-c", "doublezero user delete --pubkey " + user.pubkey})
		require.NoError(t, err)
	}

	// Wait for all users to be closed
	log.Debug("==> Waiting for all users to be closed")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			userPubkey := base58.Encode(user.PubKey[:])
			for _, trackedUser := range users {
				if trackedUser.pubkey == userPubkey {
					log.Debug("User still exists", "pubkey", userPubkey, "status", user.Status)
					return false
				}
			}
		}
		return true
	}, 90*time.Second, 2*time.Second, "users were not closed within timeout")

	// Capture snapshot AFTER deallocation
	log.Debug("==> Capturing ResourceExtension state after user deletion")
	afterDealloc, err := verifier.CaptureSnapshot(ctx)
	require.NoError(t, err, "failed to capture post-deallocation snapshot")

	// Verify DzPrefix allocations returned to baseline
	dzPrefixAfterDealloc := verifier.GetTotalDzPrefixAllocatedForDevice(afterDealloc, devicePubkey)
	log.Debug("DzPrefix state after deallocation",
		"total_allocated", dzPrefixAfterDealloc,
		"initial_allocated", initialDzPrefixAllocated)

	require.Equal(t, initialDzPrefixAllocated, dzPrefixAfterDealloc,
		"DzPrefixBlock allocations should return to baseline (initial=%d, after=%d)",
		initialDzPrefixAllocated, dzPrefixAfterDealloc)

	// Verify other device-specific resources returned to baseline
	beforeTunnelIds, err := verifier.FindTunnelIdsForDevice(beforeAlloc, devicePubkey)
	require.NoError(t, err)
	afterDeallocTunnelIds, err := verifier.FindTunnelIdsForDevice(afterDealloc, devicePubkey)
	require.NoError(t, err)

	require.Equal(t, beforeTunnelIds.Allocated, afterDeallocTunnelIds.Allocated,
		"TunnelIds allocations should return to baseline (initial=%d, after=%d)",
		beforeTunnelIds.Allocated, afterDeallocTunnelIds.Allocated)

	// Verify global UserTunnelBlock returned to baseline
	if beforeAlloc.UserTunnelBlock != nil && afterDealloc.UserTunnelBlock != nil {
		require.Equal(t, beforeAlloc.UserTunnelBlock.Allocated, afterDealloc.UserTunnelBlock.Allocated,
			"UserTunnelBlock allocations should return to baseline (initial=%d, after=%d)",
			beforeAlloc.UserTunnelBlock.Allocated, afterDealloc.UserTunnelBlock.Allocated)
	}

	log.Debug("==> DzPrefix rollover allocation test completed successfully")
}
