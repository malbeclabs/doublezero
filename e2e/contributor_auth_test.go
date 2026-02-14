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
	t.Run("contributor_owner_update_ops_manager", func(t *testing.T) {
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
	})
}
