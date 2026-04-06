//go:build e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// topologyListEntry is the JSON shape emitted by `doublezero link topology list --json`.
type topologyListEntry struct {
	Name  string `json:"name"`
	Links int    `json:"links"`
}

// TestE2E_FlexAlgo_TopologyLifecycle verifies the topology delete/clear guard:
//   - Deleting a topology while links reference it must be rejected
//   - Clearing a topology from all links (auto-discovery) succeeds
//   - Deleting the topology after clearing it succeeds
func TestE2E_FlexAlgo_TopologyLifecycle(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

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

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	// Create UNICAST-DEFAULT topology before any link activation — required by the
	// onchain program to auto-tag links at activation time.
	log.Debug("==> Creating UNICAST-DEFAULT topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "unicast-default",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Create two devices and their WAN interfaces.
	log.Debug("==> Creating devices and interfaces")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code lc-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.100.1" --dz-prefixes "45.33.100.8/29" --desired-status activated 2>&1
		doublezero device create --code lc-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.100.2" --dz-prefixes "45.33.100.16/29" --desired-status activated 2>&1
		doublezero device interface create lc-dz01 "Ethernet2" --bandwidth 10Gbps 2>&1
		doublezero device interface create lc-dz02 "Ethernet2" --bandwidth 10Gbps 2>&1
	`})
	require.NoError(t, err)

	// Wait for interfaces to reach Unlinked state (activator processes them).
	log.Debug("==> Waiting for interfaces to be unlinked")
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		unlinked := 0
		for _, d := range data.Devices {
			for _, iface := range d.Interfaces {
				if (d.Code == "lc-dz01" || d.Code == "lc-dz02") &&
					iface.Name == "Ethernet2" &&
					iface.Status == serviceability.InterfaceStatusUnlinked {
					unlinked++
				}
			}
		}
		return unlinked == 2
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create a WAN link and wait for activation.
	log.Debug("==> Creating WAN link")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero link create wan \
			--code "lc-dz01:lc-dz02" \
			--contributor co01 \
			--side-a lc-dz01 \
			--side-a-interface Ethernet2 \
			--side-z lc-dz02 \
			--side-z-interface Ethernet2 \
			--bandwidth "10 Gbps" \
			--delay-ms 10 \
			--jitter-ms 1 \
			--desired-status activated \
			-w
	`})
	require.NoError(t, err)

	var linkPubkey string
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "lc-dz01:lc-dz02" && link.Status == serviceability.LinkStatusActivated {
				linkPubkey = base58.Encode(link.PubKey[:])
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "link was not activated within timeout")
	log.Debug("--> Link activated", "pubkey", linkPubkey)

	// Create a second topology to test the lifecycle against.
	log.Debug("==> Creating test topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "lifecycle-test",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Tag the link with the test topology.
	log.Debug("==> Tagging link with lifecycle-test topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", linkPubkey,
		"--link-topology", "lifecycle-test",
	})
	require.NoError(t, err)

	// Verify the link now references the topology.
	require.Eventually(t, func() bool {
		out, execErr := dn.Manager.Exec(ctx, []string{
			"doublezero", "link", "topology", "list", "--json",
		})
		if execErr != nil {
			return false
		}
		var entries []topologyListEntry
		if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
			return false
		}
		for _, e := range entries {
			if e.Name == "lifecycle-test" && e.Links == 1 {
				return true
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "lifecycle-test topology did not show 1 link")

	// Attempt to delete the topology while the link still references it — must fail.
	log.Debug("==> Attempting topology delete with active reference (expect failure)")
	out, err := dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "delete",
		"--name", "lifecycle-test",
	}, docker.NoPrintOnError())
	require.Error(t, err, "delete should fail while link references topology")
	require.Contains(t, string(out), "still reference it")
	log.Debug("--> Delete correctly rejected")

	// Clear the topology from all links (auto-discovers referenced links).
	log.Debug("==> Clearing topology from all links")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "clear",
		"--name", "lifecycle-test",
	})
	require.NoError(t, err)

	// Now delete the topology — must succeed.
	log.Debug("==> Deleting topology after clear")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "delete",
		"--name", "lifecycle-test",
	})
	require.NoError(t, err)
	log.Debug("--> Topology deleted")

	// Verify topology is no longer present in the list.
	out, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "list", "--json",
	})
	require.NoError(t, err)
	var finalEntries []topologyListEntry
	require.NoError(t, json.Unmarshal(out, &finalEntries))
	for _, e := range finalEntries {
		require.NotEqual(t, "lifecycle-test", e.Name, "deleted topology still appears in list")
	}

	// Verify the link no longer carries the lifecycle-test topology (only UNICAST-DEFAULT).
	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)
	for _, link := range data.Links {
		if link.Code == "lc-dz01:lc-dz02" {
			require.Equal(t, 1, len(link.LinkTopologies),
				"link should have exactly one topology (unicast-default) after clearing lifecycle-test")
			return
		}
	}
	t.Fatal("link lc-dz01:lc-dz02 not found in program data")
}
