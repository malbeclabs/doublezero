//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_Activator_AccessPassExpiryRenewalCycle verifies the activator handles the
// InvalidStatus race condition gracefully. The scenario is:
//
//  1. User connects as multicast subscriber → Activated
//  2. Access pass expires (--epochs 0) → OutOfCredits
//  3. Access pass renewed (--epochs max) → user goes back through Pending/Updating → Activated
//  4. Subscribe as publisher → triggers Updating → Activated with publishers
//  5. Activator should remain running (no crash from stale events)
//  6. Delete user → cleanup
//
// This exercises the pre-flight status check in ActivateUserCommand and the improved
// error handling in the activator's log_error_ignore_invalid_status().
func TestE2E_Activator_AccessPassExpiryRenewalCycle(t *testing.T) {
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

	// =========================================================================
	// Setup: Add device, multicast group, and client
	// =========================================================================
	log.Info("==> Adding device to devnet")
	device, err := dn.AddDevice(ctx, devnet.DeviceSpec{
		Code:     "test-dz01",
		Location: "lax",
		Exchange: "xlax",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
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
			if d.Code == "test-dz01" && d.Status == serviceability.DeviceStatusActivated {
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

	// Add client
	log.Info("==> Adding client")
	client, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Wait for client to discover device
	log.Info("==> Waiting for client to discover device via latency probing")
	err = client.WaitForLatencyResults(ctx, device.ID, 90*time.Second)
	require.NoError(t, err)

	// Set access pass (unlimited epochs)
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
	})
	require.NoError(t, err)

	// Add client to multicast group allowlists (both subscriber and publisher)
	log.Info("==> Adding client to multicast group subscriber and publisher allowlists")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero multicast group allowlist subscriber add --code test-mc01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code test-mc01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	// =========================================================================
	// Phase 1: Connect as multicast subscriber → Activated
	// =========================================================================
	log.Info("==> Phase 1: Connecting as multicast subscriber")
	_, err = client.Exec(ctx, []string{"bash", "-c",
		"doublezero connect multicast subscriber test-mc01 --client-ip " + client.CYOANetworkIP,
	})
	require.NoError(t, err, "failed to connect as multicast subscriber")

	log.Info("==> Waiting for user activation")
	var userPubkey string
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if user.Status == serviceability.UserStatusActivated && len(user.Subscribers) > 0 {
				userPubkey = base58.Encode(user.PubKey[:])
				return true
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "user was not activated within timeout")
	log.Info("==> Phase 1 complete: User activated", "userPubkey", userPubkey)

	// =========================================================================
	// Phase 2: Expire access pass → OutOfCredits
	// =========================================================================
	log.Info("==> Phase 2: Expiring access pass (--epochs 0)")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey + " --epochs 0",
	})
	require.NoError(t, err, "failed to expire access pass")

	// Wait for user to reach OutOfCredits status
	log.Info("==> Waiting for user to reach OutOfCredits status")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == userPubkey {
				return user.Status == serviceability.UserStatusOutOfCredits
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "user did not reach OutOfCredits status within timeout")
	log.Info("==> Phase 2 complete: User is OutOfCredits")

	// =========================================================================
	// Phase 3: Renew access pass → user should return to Activated
	// =========================================================================
	log.Info("==> Phase 3: Renewing access pass (--epochs max)")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey + " --epochs max",
	})
	require.NoError(t, err, "failed to renew access pass")

	// Wait for user to return to Activated status
	log.Info("==> Waiting for user to return to Activated status")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == userPubkey {
				return user.Status == serviceability.UserStatusActivated
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "user did not return to Activated status within timeout")
	log.Info("==> Phase 3 complete: User re-activated after renewal")

	// =========================================================================
	// Phase 4: Subscribe as publisher → triggers Updating → Activated with publishers
	// =========================================================================
	log.Info("==> Phase 4: Subscribing as publisher to trigger re-activation")
	_, err = client.Exec(ctx, []string{"bash", "-c",
		"doublezero connect multicast publisher test-mc01 --client-ip " + client.CYOANetworkIP,
	})
	require.NoError(t, err, "failed to subscribe as publisher")

	// Wait for user to be Activated with publishers
	log.Info("==> Waiting for user to be Activated with publishers")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == userPubkey {
				return user.Status == serviceability.UserStatusActivated && len(user.Publishers) > 0
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "user was not re-activated with publishers within timeout")
	log.Info("==> Phase 4 complete: User activated with publishers")

	// =========================================================================
	// Phase 5: Verify activator is still running (no crash from stale events)
	// =========================================================================
	log.Info("==> Phase 5: Verifying activator container is still running")
	activatorState, err := dn.Activator.GetContainerState(ctx)
	require.NoError(t, err, "failed to check activator status")
	require.True(t, activatorState.Running, "activator container is not running - it likely crashed")
	log.Info("==> Activator is still running")

	// =========================================================================
	// Phase 6: Cleanup - delete user and verify removal
	// =========================================================================
	log.Info("==> Phase 6: Deleting user")
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
	log.Info("==> Phase 6 complete: User deleted")

	log.Info("==> Test completed successfully - activator survived access pass expiry/renewal cycle")
}

// TestE2E_Activator_ConcurrentAccessPassCycling verifies the activator handles
// the InvalidStatus race condition when multiple users undergo rapid concurrent
// status transitions. This reproduces the testnet bug where:
//
//  1. Multiple users are connected (Activated)
//  2. Access passes expire concurrently → all go OutOfCredits
//  3. Access passes are renewed concurrently → all go back through Pending/Updating → Activated
//  4. The activator receives overlapping websocket events from these concurrent transitions
//  5. Stale events cause ActivateUser to be called on users whose status has already changed
//  6. The pre-flight check and error handler must handle this gracefully
//
// The test asserts:
//   - Activator container stays running (no crash)
//   - All users reach correct final state
//   - Activator logs contain "Skipped (InvalidStatus" if the race was triggered
func TestE2E_Activator_ConcurrentAccessPassCycling(t *testing.T) {
	t.Parallel()

	const numClients = 3
	const numCycles = 3

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet for concurrent access pass cycling",
		"numClients", numClients, "numCycles", numCycles)

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
			OnchainAllocation: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	err = dn.Start(ctx, nil)
	require.NoError(t, err)

	// =========================================================================
	// Setup: Add device, multicast group
	// =========================================================================
	log.Info("==> Adding device")
	device, err := dn.AddDevice(ctx, devnet.DeviceSpec{
		Code:                         "test-dz01",
		Location:                     "lax",
		Exchange:                     "xlax",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)

	log.Info("==> Waiting for device activation")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, d := range data.Devices {
			if d.Code == "test-dz01" && d.Status == serviceability.DeviceStatusActivated {
				return true
			}
		}
		return false
	}, 120*time.Second, 2*time.Second, "device was not activated within timeout")

	log.Info("==> Creating multicast group")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code test-mc01 --max-bandwidth 10Gbps --owner me
	`})
	require.NoError(t, err)

	log.Info("==> Waiting for multicast group activation")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
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

	// =========================================================================
	// Setup: Add multiple clients, set access passes, add to allowlists
	// =========================================================================
	type clientInfo struct {
		client     *devnet.Client
		userPubkey string
	}
	clients := make([]clientInfo, numClients)

	for i := range numClients {
		hostID := uint32(100 + i)
		log.Info("==> Adding client", "index", i, "hostID", hostID)
		c, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: hostID,
		})
		require.NoError(t, err)
		clients[i].client = c
	}

	// Wait for all clients to discover the device via latency probing.
	for i, ci := range clients {
		log.Info("==> Waiting for client to discover device", "index", i)
		err := ci.client.WaitForLatencyResults(ctx, device.ID, 90*time.Second)
		require.NoError(t, err)
	}

	// Set access passes and add to allowlists for all clients.
	for i, ci := range clients {
		log.Info("==> Setting access pass and allowlists", "index", i)
		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + ci.client.CYOANetworkIP + " --user-payer " + ci.client.Pubkey,
		})
		require.NoError(t, err)

		_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero multicast group allowlist subscriber add --code test-mc01 --user-payer " + ci.client.Pubkey + " --client-ip " + ci.client.CYOANetworkIP,
		})
		require.NoError(t, err)
	}

	// =========================================================================
	// Phase 1: Connect all clients as multicast subscribers → all Activated
	// =========================================================================
	log.Info("==> Phase 1: Connecting all clients as multicast subscribers")
	for i, ci := range clients {
		_, err := ci.client.Exec(ctx, []string{"bash", "-c",
			"doublezero connect multicast subscriber test-mc01 --client-ip " + ci.client.CYOANetworkIP,
		})
		require.NoError(t, err, "failed to connect client %d as multicast subscriber", i)
	}

	// Wait for all users to reach Activated status and capture their pubkeys.
	log.Info("==> Waiting for all users to be activated")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		activatedCount := 0
		for _, user := range data.Users {
			if user.Status == serviceability.UserStatusActivated && len(user.Subscribers) > 0 {
				pubkey := base58.Encode(user.PubKey[:])
				// Match user to client by checking all clients.
				for j := range clients {
					if clients[j].userPubkey == "" || clients[j].userPubkey == pubkey {
						clients[j].userPubkey = pubkey
						activatedCount++
						break
					}
				}
			}
		}
		return activatedCount >= numClients
	}, 120*time.Second, 2*time.Second, "not all users were activated within timeout")

	for i, ci := range clients {
		log.Info("==> User activated", "index", i, "userPubkey", ci.userPubkey)
	}
	log.Info("==> Phase 1 complete: All users activated")

	// =========================================================================
	// Phase 2: Rapid fire expire/renew cycles to trigger stale events
	//
	// We fire expire, wait just long enough for the activator to start
	// processing the suspension (but not finish), then fire renew. This creates
	// overlapping websocket events: the activator receives User(Updating) events
	// from the suspension while the renewal is already changing the access pass
	// back. By the time the activator tries to activate a user, the status may
	// have already changed — exactly the race condition seen on testnet.
	// =========================================================================
	for cycle := range numCycles {
		log.Info("==> Cycle: Firing expire + renew back-to-back for all clients", "cycle", cycle+1)

		// Expire all access passes concurrently.
		var wg sync.WaitGroup
		errCh := make(chan error, numClients*2)
		for i, ci := range clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
					"doublezero access-pass set --accesspass-type prepaid --client-ip " + ci.client.CYOANetworkIP + " --user-payer " + ci.client.Pubkey + " --epochs 0",
				})
				if err != nil {
					errCh <- fmt.Errorf("failed to expire access pass for client %d: %w", i, err)
				}
			}()
		}
		wg.Wait()

		// Wait just long enough for the activator to pick up the expire events and
		// start processing suspensions, but not long enough for it to finish. This
		// creates the overlap: the activator is mid-suspension when the renewal
		// events arrive, generating stale User(Updating) events.
		time.Sleep(3 * time.Second)
		for i, ci := range clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
					"doublezero access-pass set --accesspass-type prepaid --client-ip " + ci.client.CYOANetworkIP + " --user-payer " + ci.client.Pubkey + " --epochs max",
				})
				if err != nil {
					errCh <- fmt.Errorf("failed to renew access pass for client %d: %w", i, err)
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			require.NoError(t, err)
		}

		// Wait for all users to return to Activated.
		log.Info("==> Cycle: Waiting for all users to return to Activated", "cycle", cycle+1)
		require.Eventually(t, func() bool {
			data, err := serviceabilityClient.GetProgramData(ctx)
			if err != nil {
				return false
			}
			activatedCount := 0
			for _, user := range data.Users {
				pubkey := base58.Encode(user.PubKey[:])
				for _, ci := range clients {
					if ci.userPubkey == pubkey && user.Status == serviceability.UserStatusActivated {
						activatedCount++
						break
					}
				}
			}
			return activatedCount >= numClients
		}, 120*time.Second, 2*time.Second, "not all users returned to Activated in cycle %d", cycle+1)

		log.Info("==> Cycle complete", "cycle", cycle+1)
	}

	// =========================================================================
	// Verification: Activator survived and handled stale events
	// =========================================================================
	log.Info("==> Verifying activator is still running")
	activatorState, err := dn.Activator.GetContainerState(ctx)
	require.NoError(t, err, "failed to check activator status")
	require.True(t, activatorState.Running, "activator container is not running - it likely crashed")

	// Check activator logs for evidence that the race was triggered and handled.
	logs, err := dn.Activator.GetLogs(ctx)
	require.NoError(t, err, "failed to get activator logs")

	if strings.Contains(logs, "Skipped (InvalidStatus") {
		log.Info("==> Race condition was triggered and handled gracefully (InvalidStatus skip found in logs)")
	} else {
		log.Info("==> Race condition was not triggered in this run (no InvalidStatus skip in logs) - this is timing-dependent")
	}

	// Verify all users are in a valid final state.
	log.Info("==> Verifying all users are in Activated state")
	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)
	for _, ci := range clients {
		found := false
		for _, user := range data.Users {
			if base58.Encode(user.PubKey[:]) == ci.userPubkey {
				require.Equal(t, serviceability.UserStatusActivated, user.Status,
					"user %s should be Activated but is %s", ci.userPubkey, user.Status)
				found = true
				break
			}
		}
		require.True(t, found, "user %s not found on-chain", ci.userPubkey)
	}

	// =========================================================================
	// Cleanup: Ensure users are Activated before deleting
	//
	// After the rapid expire/renew cycles, the activator may still be processing
	// stale suspension events. Re-set access passes and wait for all users to
	// stabilize in Activated before deleting (delete requires active status).
	// =========================================================================
	log.Info("==> Cleaning up: re-setting access passes to stabilize users")
	for _, ci := range clients {
		_, err := dn.Manager.Exec(ctx, []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --client-ip " + ci.client.CYOANetworkIP + " --user-payer " + ci.client.Pubkey + " --epochs max",
		})
		require.NoError(t, err)
	}

	log.Info("==> Waiting for all users to stabilize in Activated state before delete")
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		activatedCount := 0
		for _, user := range data.Users {
			pubkey := base58.Encode(user.PubKey[:])
			for _, ci := range clients {
				if ci.userPubkey == pubkey && user.Status == serviceability.UserStatusActivated {
					activatedCount++
					break
				}
			}
		}
		return activatedCount >= numClients
	}, 120*time.Second, 2*time.Second, "not all users returned to Activated before cleanup")

	log.Info("==> Deleting all users")
	for _, ci := range clients {
		_, err := ci.client.Exec(ctx, []string{"bash", "-c", "doublezero user delete --pubkey " + ci.userPubkey})
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false
		}
		for _, ci := range clients {
			for _, user := range data.Users {
				if base58.Encode(user.PubKey[:]) == ci.userPubkey {
					return false
				}
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "not all users were deleted within timeout")

	log.Info("==> Test completed successfully - activator survived concurrent access pass cycling")
}
