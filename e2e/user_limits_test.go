//go:build e2e

package e2e_test

import (
	"encoding/json"
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
// When max_unicast_users, max_multicast_subscribers, or max_multicast_publishers
// is set to a value > 0, attempts to create users beyond that limit should fail
// with the appropriate error (MaxUnicastUsersExceeded, MaxMulticastSubscribersExceeded,
// or MaxMulticastPublishersExceeded).
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
			"doublezero device get --code " + deviceCode + " --json"})
		require.NoError(t, err)
		log.Info("Device state after first unicast user", "device", string(deviceOut))
		var deviceState struct {
			UnicastUsersCount uint16 `json:"unicast_users_count"`
		}
		require.NoError(t, json.Unmarshal(deviceOut, &deviceState))
		require.Equal(t, uint16(1), deviceState.UnicastUsersCount,
			"Expected unicast_users_count=1, got: %d", deviceState.UnicastUsersCount)

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
	// Test: Multicast subscriber limit enforcement
	// =========================================================================
	t.Run("multicast_limit_enforcement", func(t *testing.T) {
		log.Info("==> Setting max_multicast_subscribers=1 on device")

		// Set max_multicast_subscribers to 1
		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-multicast-subscribers 1"})
		require.NoError(t, err)

		// Create first multicast subscriber (should succeed)
		log.Info("==> Creating first multicast subscriber (should succeed)")
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

		// Wait for first multicast subscriber to be activated
		serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if user.Status == serviceability.UserStatusActivated &&
					user.UserType == serviceability.UserTypeMulticast &&
					len(user.Subscribers) > 0 {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "first multicast subscriber was not activated within timeout")
		log.Info("First multicast subscriber activated successfully")

		// Verify device state shows multicast_subscribers_count=1
		deviceOut, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device get --code " + deviceCode + " --json"})
		require.NoError(t, err)
		log.Info("Device state after first multicast subscriber", "device", string(deviceOut))
		var deviceState struct {
			MulticastSubscribersCount uint16 `json:"multicast_subscribers_count"`
		}
		require.NoError(t, json.Unmarshal(deviceOut, &deviceState))
		require.Equal(t, uint16(1), deviceState.MulticastSubscribersCount,
			"Expected multicast_subscribers_count=1, got: %d", deviceState.MulticastSubscribersCount)

		// Try to create second multicast subscriber (should fail with limit exceeded)
		log.Info("==> Creating second multicast subscriber (should fail)")
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

		// This should fail because we've hit the multicast subscriber limit
		output, err := client2.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast subscriber limit-mc01 --device " + deviceCode + " --client-ip " + client2.CYOANetworkIP + " 2>&1; echo EXIT_CODE=$?"})
		outputStr := string(output)
		log.Info("Second multicast subscriber creation result", "output", outputStr, "err", err)

		// Command should have failed (non-zero exit) and output should contain the limit error
		require.Contains(t, outputStr, "multicast subscriber limit",
			"Expected multicast subscriber limit error, got: %s", outputStr)

		log.Info("==> PASS: Multicast subscriber limit enforcement working correctly")

		// Reset limit for next test
		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-multicast-subscribers 0"})
		require.NoError(t, err)
	})

	// =========================================================================
	// Test: Multicast publisher limit enforcement
	// =========================================================================
	t.Run("multicast_publisher_limit_enforcement", func(t *testing.T) {
		log.Info("==> Setting max_multicast_publishers=1 on device")

		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-multicast-publishers 1"})
		require.NoError(t, err)

		// Create first publisher (should succeed)
		log.Info("==> Creating first multicast publisher (should succeed)")
		client1, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 130,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group allowlist publisher add --code limit-mc01 --user-payer " + client1.Pubkey + " --client-ip " + client1.CYOANetworkIP})
		require.NoError(t, err)

		// Use --device to specify device explicitly (no latency probing needed)
		_, err = client1.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast publisher limit-mc01 --device " + deviceCode + " --client-ip " + client1.CYOANetworkIP})
		require.NoError(t, err)

		// Wait for first multicast publisher to be activated
		serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			for _, user := range data.Users {
				if user.Status == serviceability.UserStatusActivated &&
					user.UserType == serviceability.UserTypeMulticast &&
					len(user.Publishers) > 0 {
					return true
				}
			}
			return false
		}, 90*time.Second, 2*time.Second, "first multicast publisher was not activated within timeout")
		log.Info("First multicast publisher activated successfully")

		// Verify device state shows multicast_publishers_count=1
		deviceOut, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device get --code " + deviceCode + " --json"})
		require.NoError(t, err)
		log.Info("Device state after first multicast publisher", "device", string(deviceOut))
		var deviceState struct {
			MulticastPublishersCount uint16 `json:"multicast_publishers_count"`
		}
		require.NoError(t, json.Unmarshal(deviceOut, &deviceState))
		require.Equal(t, uint16(1), deviceState.MulticastPublishersCount,
			"Expected multicast_publishers_count=1, got: %d", deviceState.MulticastPublishersCount)

		// Try to create second multicast publisher (should fail with limit exceeded)
		log.Info("==> Creating second multicast publisher (should fail)")
		client2, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: 131,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group allowlist publisher add --code limit-mc01 --user-payer " + client2.Pubkey + " --client-ip " + client2.CYOANetworkIP})
		require.NoError(t, err)

		// This should fail because we've hit the multicast publisher limit
		output, err := client2.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast publisher limit-mc01 --device " + deviceCode + " --client-ip " + client2.CYOANetworkIP + " 2>&1; echo EXIT_CODE=$?"})
		outputStr := string(output)
		log.Info("Second multicast publisher creation result", "output", outputStr, "err", err)

		// Command should have failed (non-zero exit) and output should contain the limit error
		require.Contains(t, outputStr, "multicast publisher limit",
			"Expected multicast publisher limit error, got: %s", outputStr)

		log.Info("==> PASS: Multicast publisher limit enforcement working correctly")

		// Reset limit
		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero device update --pubkey " + devicePubkey.String() + " --max-multicast-publishers 0"})
		require.NoError(t, err)
	})

	log.Info("==> All user limit tests passed")
}
