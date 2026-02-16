//go:build e2e

package e2e_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

// TestE2E_UserLimits verifies that per-device unicast and multicast
// user limits are enforced correctly.
//
// When max_unicast_users or max_multicast_users is set to a value > 0,
// attempts to create users beyond that limit should fail with the appropriate
// error (MaxUnicastUsersExceeded or MaxMulticastUsersExceeded).
//
// This test creates the device onchain only (no cEOS container) since we're
// testing limit enforcement which happens in the client-side pre-flight check.
// By specifying --device explicitly, we skip latency-based device selection
// and the capacity check runs before any tunnel setup is attempted.
func TestE2E_UserLimits(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

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
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// Create device onchain only (no container needed for limit testing)
	log.Info("==> Creating device onchain")
	deviceCode := "limit-dz01"

	// Derive a valid public IP from the CYOA network subnet (uses 9.x.x.x range which passes global IP check)
	const deviceHostID = 8
	publicIP, err := netutil.DeriveIPFromCIDR(dn.CYOANetwork.SubnetCIDR, deviceHostID)
	require.NoError(t, err)

	// Calculate dz-prefix: add 128 to last octet, round to /29 boundary
	publicIPBytes := publicIP.To4()
	dzPrefixBytes := make(net.IP, 4)
	copy(dzPrefixBytes, publicIPBytes)
	if publicIPBytes[3] >= 128 {
		dzPrefixBytes[2]++
		dzPrefixBytes[3] = ((publicIPBytes[3] - 128) / 8) * 8
	} else {
		dzPrefixBytes[3] = ((publicIPBytes[3] + 128) / 8) * 8
	}
	dzPrefix := dzPrefixBytes.String() + "/29"

	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero device create --contributor co01 --code " + deviceCode + " --location lax --exchange xlax --public-ip " + publicIP.String() + " --dz-prefixes " + dzPrefix + " --mgmt-vrf mgmt"})
	require.NoError(t, err)

	// Get device pubkey
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
			if d.Code == deviceCode {
				devicePubkey = solana.PublicKeyFromBytes(d.PubKey[:])
				return true
			}
		}
		return false
	}, 30*time.Second, 1*time.Second, "device was not found onchain")
	log.Info("Device created onchain", "pubkey", devicePubkey.String())

	// Set device health to ready-for-users and desired status to activated
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero device update --pubkey " + devicePubkey.String() + " --max-users 128 --desired-status activated"})
	require.NoError(t, err)

	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero device set-health --pubkey " + devicePubkey.String() + " --health ready-for-users"})
	require.NoError(t, err)

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
		for _, d := range data.Devices {
			if d.Code == deviceCode && d.Status == serviceability.DeviceStatusActivated {
				return true
			}
		}
		return false
	}, 30*time.Second, 1*time.Second, "device was not activated within timeout")
	log.Info("Device activated", "pubkey", devicePubkey.String())

	// Create multicast group for multicast user tests
	log.Info("==> Creating multicast group")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code limit-mc01 --max-bandwidth 10Gbps --owner me
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
			if mc.Code == "limit-mc01" && mc.Status == serviceability.MulticastGroupStatusActivated {
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "multicast group was not activated within timeout")

	// =========================================================================
	// Test: Unicast user limit enforcement
	// =========================================================================
	t.Run("unicast_limit_enforcement", func(t *testing.T) {
		log.Info("==> Setting max_unicast_users=1 on device")

		// Set max_unicast_users to 1
		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-unicast-users 1"})
		require.NoError(t, err)

		// Create first unicast user (should succeed)
		log.Info("==> Creating first unicast user (should succeed)")
		client1, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 110,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
		require.NoError(t, err)

		// Use --device to specify device explicitly (no latency probing needed)
		_, err = client1.Exec(ctx, []string{"bash", "-c",
			"doublezero connect ibrl --device " + deviceCode + " --client-ip " + client1.CYOANetworkIP + " --allocate-addr"})
		require.NoError(t, err)

		// Wait for first user to be activated
		serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if user.Status == serviceability.UserStatusActivated &&
					user.UserType == serviceability.UserTypeIBRLWithAllocIP {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "first unicast user was not activated within timeout")
		log.Info("First unicast user activated successfully")

		// Verify device state shows unicast_users_count=1
		deviceOut, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device get --code " + deviceCode})
		require.NoError(t, err)
		log.Info("Device state after first unicast user", "device", string(deviceOut))
		require.Contains(t, string(deviceOut), "unicast_users_count: 1",
			"Expected unicast_users_count=1, got: %s", string(deviceOut))

		// Try to create second unicast user (should fail with limit exceeded)
		log.Info("==> Creating second unicast user (should fail)")
		client2, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 111,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
		require.NoError(t, err)

		// This should fail because we've hit the unicast limit
		output, err := client2.Exec(ctx, []string{"bash", "-c",
			"doublezero connect ibrl --device " + deviceCode + " --client-ip " + client2.CYOANetworkIP + " --allocate-addr 2>&1; echo EXIT_CODE=$?"})
		outputStr := string(output)
		log.Info("Second unicast user creation result", "output", outputStr, "err", err)

		// Command should have failed (non-zero exit) and output should contain the limit error
		require.Contains(t, outputStr, "unicast user limit",
			"Expected unicast limit error, got: %s", outputStr)

		log.Info("==> PASS: Unicast limit enforcement working correctly")

		// Reset limit for next test
		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-unicast-users 0"})
		require.NoError(t, err)
	})

	// =========================================================================
	// Test: Multicast user limit enforcement
	// =========================================================================
	t.Run("multicast_limit_enforcement", func(t *testing.T) {
		log.Info("==> Setting max_multicast_users=1 on device")

		// Set max_multicast_users to 1
		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-multicast-users 1"})
		require.NoError(t, err)

		// Create first multicast user (should succeed)
		log.Info("==> Creating first multicast user (should succeed)")
		client1, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 120,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group allowlist subscriber add --code limit-mc01 --user-payer " + client1.Pubkey + " --client-ip " + client1.CYOANetworkIP})
		require.NoError(t, err)

		// Use --device to specify device explicitly (no latency probing needed)
		_, err = client1.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast subscriber limit-mc01 --device " + deviceCode + " --client-ip " + client1.CYOANetworkIP})
		require.NoError(t, err)

		// Wait for first multicast user to be activated
		serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if user.Status == serviceability.UserStatusActivated &&
					user.UserType == serviceability.UserTypeMulticast {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "first multicast user was not activated within timeout")
		log.Info("First multicast user activated successfully")

		// Verify device state shows multicast_users_count=1
		deviceOut, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device get --code " + deviceCode})
		require.NoError(t, err)
		log.Info("Device state after first multicast user", "device", string(deviceOut))
		require.Contains(t, string(deviceOut), "multicast_users_count: 1",
			"Expected multicast_users_count=1, got: %s", string(deviceOut))

		// Try to create second multicast user (should fail with limit exceeded)
		log.Info("==> Creating second multicast user (should fail)")
		client2, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 121,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group allowlist subscriber add --code limit-mc01 --user-payer " + client2.Pubkey + " --client-ip " + client2.CYOANetworkIP})
		require.NoError(t, err)

		// This should fail because we've hit the multicast limit
		output, err := client2.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast subscriber limit-mc01 --device " + deviceCode + " --client-ip " + client2.CYOANetworkIP + " 2>&1; echo EXIT_CODE=$?"})
		outputStr := string(output)
		log.Info("Second multicast user creation result", "output", outputStr, "err", err)

		// Command should have failed (non-zero exit) and output should contain the limit error
		require.Contains(t, outputStr, "multicast user limit",
			"Expected multicast limit error, got: %s", outputStr)

		log.Info("==> PASS: Multicast limit enforcement working correctly")
	})

	log.Info("==> All user limit tests passed")
}
