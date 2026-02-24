//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

// TestE2E_ContributorAuth tests contributor-owner authorization and reference-count
// guards. Basic CRUD (create, get, list, update, delete) is covered by the backward
// compatibility test; this test focuses on ownership semantics: non-foundation signers
// updating their own devices/interfaces, reference-count delete rejection, and
// ops_manager self-service.
//
// All subtests share a single devnet to avoid Docker network pool exhaustion.
func TestE2E_ContributorAuth(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Starting test devnet for contributor lifecycle tests")

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

	// === Tier 2: Contributor CRUD ===

	// Subtest: create_and_get
	if !t.Run("create_and_get", func(t *testing.T) {
		log.Debug("==> Creating contributor test-co02")

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "create",
			"--code", "test-co02",
			"--owner", "me",
		})
		require.NoError(t, err, "failed to create contributor test-co02")

		// Verify via contributor get
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co02",
		})
		require.NoError(t, err, "failed to get contributor test-co02")
		outputStr := string(output)
		require.Contains(t, outputStr, "test-co02", "contributor get should contain code")
		require.Contains(t, outputStr, "activated", "contributor should be activated")

		log.Debug("--> Contributor test-co02 created and verified")
	}) {
		t.FailNow()
	}

	// Subtest: list_includes_new_contributor
	if !t.Run("list_includes_new_contributor", func(t *testing.T) {
		log.Debug("==> Listing contributors")

		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "list",
		})
		require.NoError(t, err, "failed to list contributors")
		outputStr := string(output)
		require.Contains(t, outputStr, "co01", "contributor list should contain co01")
		require.Contains(t, outputStr, "test-co02", "contributor list should contain test-co02")

		log.Debug("--> Contributor list verified")
	}) {
		t.FailNow()
	}

	// Subtest: update_code
	if !t.Run("update_code", func(t *testing.T) {
		log.Debug("==> Updating contributor test-co02 code to test-co02-renamed")

		// contributor update --pubkey requires an actual base58 pubkey, not a code.
		// Extract the pubkey from contributor get output.
		getOutput, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co02",
		})
		require.NoError(t, err, "failed to get contributor test-co02 for pubkey lookup")
		// Output format: "account: <pubkey>,\r\ncode: ..."
		var co02Pubkey string
		for _, line := range strings.Split(string(getOutput), "\n") {
			line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
			if strings.HasPrefix(line, "account:") {
				co02Pubkey = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "account:"), ","))
				break
			}
		}
		require.NotEmpty(t, co02Pubkey, "could not extract pubkey from contributor get output: %s", string(getOutput))

		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "update",
			"--pubkey", co02Pubkey,
			"--code", "test-co02-renamed",
		})
		require.NoError(t, err, "failed to update contributor code")

		// Verify the update
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co02-renamed",
		})
		require.NoError(t, err, "failed to get renamed contributor")
		require.Contains(t, string(output), "test-co02-renamed", "contributor should have updated code")

		log.Debug("--> Contributor code updated and verified")
	}) {
		t.FailNow()
	}

	// Subtest: delete_empty_contributor
	if !t.Run("delete_empty_contributor", func(t *testing.T) {
		log.Debug("==> Deleting contributor test-co02-renamed (no devices)")

		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "delete",
			"--pubkey", "test-co02-renamed",
		})
		require.NoError(t, err, "failed to delete empty contributor")

		// Verify it's gone from the list
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "list",
		})
		require.NoError(t, err, "failed to list contributors after delete")
		require.NotContains(t, string(output), "test-co02-renamed",
			"deleted contributor should not appear in list")

		log.Debug("--> Empty contributor deleted and verified")
	}) {
		t.FailNow()
	}

	// Subtest: delete_with_devices_rejected
	if !t.Run("delete_with_devices_rejected", func(t *testing.T) {
		log.Debug("==> Creating contributor test-co03 with a device to test delete rejection")

		// Create the contributor
		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "create",
			"--code", "test-co03",
			"--owner", "me",
		})
		require.NoError(t, err, "failed to create contributor test-co03")

		// Create a device under test-co03
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			doublezero device create --code test-dev99 --contributor test-co03 --location lax --exchange xlax --public-ip "45.33.100.3" --dz-prefixes "45.33.100.24/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		`})
		require.NoError(t, err, "failed to create device under test-co03")

		// Attempt to delete the contributor - should fail due to reference_count > 0
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "delete",
			"--pubkey", "test-co03",
		})
		require.Error(t, err, "expected error when deleting contributor with active devices")
		log.Debug("Delete with devices error output", "output", string(output))

		log.Debug("--> Correctly rejected deletion of contributor with active devices")
	}) {
		t.FailNow()
	}

	// Subtest: contributor_owner_interface_update
	if !t.Run("contributor_owner_interface_update", func(t *testing.T) {
		log.Debug("==> Testing contributor owner can update interfaces on their own devices")

		// Step 1: Generate a new keypair inside the manager container
		_, err := dn.Manager.Exec(t.Context(), []string{
			"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/co-owner.json",
		})
		require.NoError(t, err, "failed to generate contributor owner keypair")

		// Step 2: Get the new keypair's pubkey
		pubkeyOutput, err := dn.Manager.Exec(t.Context(), []string{
			"solana", "address", "-k", "/tmp/co-owner.json",
		})
		require.NoError(t, err, "failed to get contributor owner pubkey")
		coOwnerPubkey := strings.TrimSpace(string(pubkeyOutput))
		log.Debug("Contributor owner pubkey", "pubkey", coOwnerPubkey)

		// Step 3: Fund the new keypair
		_, err = dn.Manager.Exec(t.Context(), []string{
			"solana", "transfer", "--allow-unfunded-recipient", coOwnerPubkey, "10",
		})
		require.NoError(t, err, "failed to fund contributor owner keypair")

		// Step 4: Create contributor test-co04 with the new pubkey as owner
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "create",
			"--code", "test-co04",
			"--owner", coOwnerPubkey,
		})
		require.NoError(t, err, "failed to create contributor test-co04 with custom owner")

		// Step 5: Create a device under test-co04 (via foundation)
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			doublezero device create --code test-dev-co04 --contributor test-co04 --location ewr --exchange xewr --public-ip "45.33.100.4" --dz-prefixes "45.33.100.32/29" --mgmt-vrf mgmt --desired-status activated 2>&1
			doublezero device update --pubkey test-dev-co04 --max-users 128 2>&1
		`})
		require.NoError(t, err, "failed to create device under test-co04")

		// Step 6: Create a CYOA interface on the device (via foundation)
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			"test-dev-co04", "Ethernet1",
			"--interface-cyoa", "gre-over-dia",
			"--ip-net", "45.33.100.80/31",
		})
		require.NoError(t, err, "failed to create interface on test-dev-co04")

		// Step 7: Wait for interface to exist and be unlinked by activator
		log.Debug("==> Waiting for interface to be unlinked by activator")
		require.Eventually(t, func() bool {
			client, err := dn.Ledger.GetServiceabilityClient()
			if err != nil {
				return false
			}
			data, err := client.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, device := range data.Devices {
				if device.Code == "test-dev-co04" {
					for _, iface := range device.Interfaces {
						if iface.Name == "Ethernet1" && iface.Status == serviceability.InterfaceStatusUnlinked {
							return true
						}
					}
				}
			}
			return false
		}, 60*time.Second, 2*time.Second, "interface was not unlinked within timeout")

		// Step 8: Update the interface using the contributor owner's keypair via env var
		log.Debug("==> Updating interface as contributor owner")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf(`
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner.json doublezero device interface update test-dev-co04 Ethernet1 --mtu 9000 2>&1
		`)})
		require.NoError(t, err, "contributor owner should be able to update interface on their own device")

		// Step 9: Verify the update via Go SDK
		log.Debug("==> Verifying interface update via SDK")
		iface, err := waitForInterfaceUpdate(t.Context(), dn, "test-dev-co04", "Ethernet1", serviceability.LoopbackTypeNone, 9000, 30*time.Second)
		require.NoError(t, err, "timed out waiting for interface update")
		require.Equal(t, uint16(9000), iface.Mtu, "mtu should be updated to 9000")

		// Step 10: Negative test - random third keypair should be rejected
		log.Debug("==> Testing that random keypair is rejected")
		_, err = dn.Manager.Exec(t.Context(), []string{
			"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/random-signer.json", "--force",
		})
		require.NoError(t, err, "failed to generate random keypair")

		randomPubkeyOutput, err := dn.Manager.Exec(t.Context(), []string{
			"solana", "address", "-k", "/tmp/random-signer.json",
		})
		require.NoError(t, err, "failed to get random pubkey")
		randomPubkey := strings.TrimSpace(string(randomPubkeyOutput))

		_, err = dn.Manager.Exec(t.Context(), []string{
			"solana", "transfer", "--allow-unfunded-recipient", randomPubkey, "10",
		})
		require.NoError(t, err, "failed to fund random keypair")

		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/random-signer.json doublezero device interface update test-dev-co04 Ethernet1 --mtu 9001 2>&1
		`})
		require.Error(t, err, "random keypair should not be able to update interface on another contributor's device")

		log.Debug("--> Contributor owner interface update test completed")
	}) {
		t.FailNow()
	}

	// === Tier 3: Contributor Owner Update Operations ===

	// Subtest: contributor_owner_update_ops_manager
	if !t.Run("contributor_owner_update_ops_manager", func(t *testing.T) {
		log.Debug("==> Testing contributor owner can update their own ops_manager")

		// Generate a pubkey to use as the ops manager
		_, err := dn.Manager.Exec(t.Context(), []string{
			"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/ops-mgr.json", "--force",
		})
		require.NoError(t, err, "failed to generate ops manager keypair")

		opsMgrPubkeyOutput, err := dn.Manager.Exec(t.Context(), []string{
			"solana", "address", "-k", "/tmp/ops-mgr.json",
		})
		require.NoError(t, err, "failed to get ops manager pubkey")
		opsMgrPubkey := strings.TrimSpace(string(opsMgrPubkeyOutput))

		// Extract the actual pubkey for test-co04 (--pubkey requires base58 pubkey, not code)
		co04GetOutput, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co04",
		})
		require.NoError(t, err, "failed to get contributor test-co04 for pubkey lookup")
		var co04Pubkey string
		for _, line := range strings.Split(string(co04GetOutput), "\n") {
			line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
			if strings.HasPrefix(line, "account:") {
				co04Pubkey = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "account:"), ","))
				break
			}
		}
		require.NotEmpty(t, co04Pubkey, "could not extract pubkey from contributor get output: %s", string(co04GetOutput))

		// Update ops_manager using the contributor owner's keypair
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf(`
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner.json doublezero contributor update --pubkey %s --ops-manager %s 2>&1
		`, co04Pubkey, opsMgrPubkey)})
		require.NoError(t, err, "contributor owner should be able to update their own ops_manager")

		// Verify ops_manager was updated
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co04",
		})
		require.NoError(t, err, "failed to get contributor test-co04")
		require.Contains(t, string(output), opsMgrPubkey,
			"contributor should show updated ops_manager pubkey")

		// Attempt code update using contributor owner keypair - should fail (only foundation can update code)
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf(`
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner.json doublezero contributor update --pubkey %s --code new-code 2>&1
		`, co04Pubkey)})
		require.Error(t, err, "contributor owner should not be able to update contributor code (foundation only)")

		log.Debug("--> Contributor owner ops_manager update test completed")
	}) {
		t.FailNow()
	}

	// === Tier 4: Ownership Transfer, Ref-Count Lifecycle, Cross-Contributor Isolation ===

	// Subtest: owner_transfer_revokes_old_owner_access
	if !t.Run("owner_transfer_revokes_old_owner_access", func(t *testing.T) {
		log.Debug("==> Testing that transferring contributor ownership revokes old owner's access")

		// Step 1: Generate a new keypair for the new owner
		_, err := dn.Manager.Exec(t.Context(), []string{
			"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/new-co-owner.json", "--force",
		})
		require.NoError(t, err, "failed to generate new contributor owner keypair")

		newOwnerPubkeyOutput, err := dn.Manager.Exec(t.Context(), []string{
			"solana", "address", "-k", "/tmp/new-co-owner.json",
		})
		require.NoError(t, err, "failed to get new contributor owner pubkey")
		newOwnerPubkey := strings.TrimSpace(string(newOwnerPubkeyOutput))
		log.Debug("New contributor owner pubkey", "pubkey", newOwnerPubkey)

		// Step 2: Fund the new owner
		_, err = dn.Manager.Exec(t.Context(), []string{
			"solana", "transfer", "--allow-unfunded-recipient", newOwnerPubkey, "10",
		})
		require.NoError(t, err, "failed to fund new contributor owner keypair")

		// Step 3: Extract test-co04 account pubkey
		co04GetOutput, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "get",
			"--code", "test-co04",
		})
		require.NoError(t, err, "failed to get contributor test-co04")
		var co04Pubkey string
		for _, line := range strings.Split(string(co04GetOutput), "\n") {
			line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
			if strings.HasPrefix(line, "account:") {
				co04Pubkey = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "account:"), ","))
				break
			}
		}
		require.NotEmpty(t, co04Pubkey, "could not extract pubkey from contributor get output: %s", string(co04GetOutput))

		// Step 4: Foundation transfers ownership of test-co04 to the new owner
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "update",
			"--pubkey", co04Pubkey,
			"--owner", newOwnerPubkey,
		})
		require.NoError(t, err, "failed to transfer contributor ownership to new owner")

		// Step 5: Old owner attempts interface update → expect error
		log.Debug("==> Old owner attempting interface update (should fail)")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner.json doublezero device interface update test-dev-co04 Ethernet1 --mtu 1400 2>&1
		`})
		require.Error(t, err, "old owner should no longer be able to update interface after ownership transfer")

		// Step 6: New owner updates MTU → expect success
		log.Debug("==> New owner updating interface (should succeed)")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/new-co-owner.json doublezero device interface update test-dev-co04 Ethernet1 --mtu 1500 2>&1
		`})
		require.NoError(t, err, "new owner should be able to update interface after ownership transfer")

		// Step 7: Verify via SDK
		iface, err := waitForInterfaceUpdate(t.Context(), dn, "test-dev-co04", "Ethernet1", serviceability.LoopbackTypeNone, 1500, 30*time.Second)
		require.NoError(t, err, "timed out waiting for interface update after ownership transfer")
		require.Equal(t, uint16(1500), iface.Mtu, "mtu should be updated to 1500 by new owner")

		log.Debug("--> Owner transfer access revocation test completed")
	}) {
		t.FailNow()
	}

	// Subtest: reference_count_full_lifecycle
	if !t.Run("reference_count_full_lifecycle", func(t *testing.T) {
		log.Debug("==> Testing reference count decrement on device deletion enables contributor deletion")

		// Step 1: Create contributor test-co05
		_, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "create",
			"--code", "test-co05",
			"--owner", "me",
		})
		require.NoError(t, err, "failed to create contributor test-co05")

		// Step 2: Create device under test-co05
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			doublezero device create --code test-dev-co05 --contributor test-co05 --location lax --exchange xlax --public-ip "45.33.100.5" --dz-prefixes "45.33.100.40/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		`})
		require.NoError(t, err, "failed to create device test-dev-co05")

		// Step 3: Wait for device activation via SDK
		log.Debug("==> Waiting for device test-dev-co05 to be activated")
		require.Eventually(t, func() bool {
			client, err := dn.Ledger.GetServiceabilityClient()
			if err != nil {
				return false
			}
			data, err := client.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, device := range data.Devices {
				if device.Code == "test-dev-co05" && device.Status == serviceability.DeviceStatusActivated {
					return true
				}
			}
			return false
		}, 60*time.Second, 2*time.Second, "device test-dev-co05 was not activated within timeout")

		// Step 4: Attempt contributor delete → expect error (ref_count > 0)
		log.Debug("==> Attempting to delete contributor with active device (should fail)")
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "delete",
			"--pubkey", "test-co05",
		})
		require.Error(t, err, "deleting contributor with active device should fail")

		// Step 5: Drain then delete the device
		log.Debug("==> Draining and deleting device test-dev-co05")
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "update",
			"--pubkey", "test-dev-co05",
			"--status", "drained",
		})
		require.NoError(t, err, "failed to drain device test-dev-co05")

		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "delete",
			"--pubkey", "test-dev-co05",
		})
		require.NoError(t, err, "failed to delete device test-dev-co05")

		// Step 6: Wait for device removal from ledger
		log.Debug("==> Waiting for device test-dev-co05 to be removed from ledger")
		require.Eventually(t, func() bool {
			client, err := dn.Ledger.GetServiceabilityClient()
			if err != nil {
				return false
			}
			data, err := client.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, device := range data.Devices {
				if device.Code == "test-dev-co05" {
					return false
				}
			}
			return true
		}, 60*time.Second, 2*time.Second, "device test-dev-co05 was not removed from ledger within timeout")

		// Step 7: Delete contributor → expect success
		log.Debug("==> Deleting contributor test-co05 (should succeed now)")
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "delete",
			"--pubkey", "test-co05",
		})
		require.NoError(t, err, "failed to delete contributor test-co05 after device removal")

		// Step 8: Verify contributor is gone
		output, err := dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "list",
		})
		require.NoError(t, err, "failed to list contributors after delete")
		require.NotContains(t, string(output), "test-co05",
			"deleted contributor test-co05 should not appear in list")

		log.Debug("--> Reference count full lifecycle test completed")
	}) {
		t.FailNow()
	}

	// Subtest: cross_contributor_isolation
	t.Run("cross_contributor_isolation", func(t *testing.T) {
		log.Debug("==> Testing cross-contributor isolation: owners cannot modify other contributors' devices")

		// Step 1: Generate keypair for owner-B
		_, err := dn.Manager.Exec(t.Context(), []string{
			"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/co-owner-b.json", "--force",
		})
		require.NoError(t, err, "failed to generate owner-B keypair")

		ownerBPubkeyOutput, err := dn.Manager.Exec(t.Context(), []string{
			"solana", "address", "-k", "/tmp/co-owner-b.json",
		})
		require.NoError(t, err, "failed to get owner-B pubkey")
		ownerBPubkey := strings.TrimSpace(string(ownerBPubkeyOutput))
		log.Debug("Owner-B pubkey", "pubkey", ownerBPubkey)

		// Step 2: Fund owner-B
		_, err = dn.Manager.Exec(t.Context(), []string{
			"solana", "transfer", "--allow-unfunded-recipient", ownerBPubkey, "10",
		})
		require.NoError(t, err, "failed to fund owner-B keypair")

		// Step 3: Create contributor test-co06 with owner-B
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "contributor", "create",
			"--code", "test-co06",
			"--owner", ownerBPubkey,
		})
		require.NoError(t, err, "failed to create contributor test-co06 with owner-B")

		// Step 4: Create device under test-co06
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			doublezero device create --code test-dev-co06 --contributor test-co06 --location lhr --exchange xlhr --public-ip "45.33.100.6" --dz-prefixes "45.33.100.48/29" --mgmt-vrf mgmt --desired-status activated 2>&1
		`})
		require.NoError(t, err, "failed to create device test-dev-co06")

		// Step 5: Create a CYOA interface on test-dev-co06
		_, err = dn.Manager.Exec(t.Context(), []string{
			"doublezero", "device", "interface", "create",
			"test-dev-co06", "Ethernet1",
			"--interface-cyoa", "gre-over-dia",
			"--ip-net", "45.33.100.82/31",
		})
		require.NoError(t, err, "failed to create interface on test-dev-co06")

		// Step 6: Wait for interface to be unlinked
		log.Debug("==> Waiting for test-dev-co06 Ethernet1 to be unlinked")
		require.Eventually(t, func() bool {
			client, err := dn.Ledger.GetServiceabilityClient()
			if err != nil {
				return false
			}
			data, err := client.GetProgramData(t.Context())
			if err != nil {
				return false
			}
			for _, device := range data.Devices {
				if device.Code == "test-dev-co06" {
					for _, iface := range device.Interfaces {
						if iface.Name == "Ethernet1" && iface.Status == serviceability.InterfaceStatusUnlinked {
							return true
						}
					}
				}
			}
			return false
		}, 60*time.Second, 2*time.Second, "test-dev-co06 Ethernet1 was not unlinked within timeout")

		// Step 7: Owner-B tries to update test-dev-co04 (owned by different contributor) → expect error
		log.Debug("==> Owner-B attempting to update test-dev-co04 (should fail)")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner-b.json doublezero device interface update test-dev-co04 Ethernet1 --mtu 1400 2>&1
		`})
		require.Error(t, err, "owner-B should not be able to update interface on another contributor's device")

		// Step 8: test-co04 owner tries to update test-dev-co06 (owned by owner-B) → expect error
		log.Debug("==> test-co04 owner attempting to update test-dev-co06 (should fail)")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/new-co-owner.json doublezero device interface update test-dev-co06 Ethernet1 --mtu 1400 2>&1
		`})
		require.Error(t, err, "test-co04 owner should not be able to update interface on owner-B's device")

		// Step 9: Owner-B updates their own device → expect success
		log.Debug("==> Owner-B updating their own device (should succeed)")
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -euo pipefail
			DOUBLEZERO_KEYPAIR=/tmp/co-owner-b.json doublezero device interface update test-dev-co06 Ethernet1 --mtu 9000 2>&1
		`})
		require.NoError(t, err, "owner-B should be able to update interface on their own device")

		// Step 10: Verify via SDK
		iface, err := waitForInterfaceUpdate(t.Context(), dn, "test-dev-co06", "Ethernet1", serviceability.LoopbackTypeNone, 9000, 30*time.Second)
		require.NoError(t, err, "timed out waiting for owner-B interface update")
		require.Equal(t, uint16(9000), iface.Mtu, "mtu should be updated to 9000 by owner-B")

		log.Debug("--> Cross-contributor isolation test completed")
	})
}
