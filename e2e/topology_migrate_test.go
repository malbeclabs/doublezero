//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_FlexAlgo_Migrate verifies the `doublezero-admin migrate flex-algo` command:
//   - Dry-run correctly identifies a link whose topologies have been cleared
//   - Live run re-tags the link with UNICAST-DEFAULT
//
// Setup: activate a link (auto-tagged with UNICAST-DEFAULT), clear all topologies
// from it to simulate a pre-RFC-18 link, then run the migrate command.
func TestE2E_FlexAlgo_Migrate(t *testing.T) {
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

	// Create UNICAST-DEFAULT topology before link activation.
	log.Debug("==> Creating UNICAST-DEFAULT topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "unicast-default",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Create two devices with WAN interfaces.
	log.Debug("==> Creating devices and interfaces")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code mg-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.103.1" --dz-prefixes "45.33.103.8/29" --desired-status activated 2>&1
		doublezero device create --code mg-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.103.2" --dz-prefixes "45.33.103.16/29" --desired-status activated 2>&1
		doublezero device interface create mg-dz01 "Ethernet2" --bandwidth 10Gbps 2>&1
		doublezero device interface create mg-dz02 "Ethernet2" --bandwidth 10Gbps 2>&1
	`})
	require.NoError(t, err)

	log.Debug("==> Waiting for interfaces to be unlinked")
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		unlinked := 0
		for _, d := range data.Devices {
			for _, iface := range d.Interfaces {
				if (d.Code == "mg-dz01" || d.Code == "mg-dz02") &&
					iface.Name == "Ethernet2" &&
					iface.Status == serviceability.InterfaceStatusUnlinked {
					unlinked++
				}
			}
		}
		return unlinked == 2
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create WAN link and wait for activation.
	log.Debug("==> Creating WAN link")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero link create wan \
			--code "mg-dz01:mg-dz02" \
			--contributor co01 \
			--side-a mg-dz01 --side-a-interface Ethernet2 \
			--side-z mg-dz02 --side-z-interface Ethernet2 \
			--bandwidth "10 Gbps" --delay-ms 10 --jitter-ms 1 \
			--desired-status activated -w
	`})
	require.NoError(t, err)

	var linkPubkey string
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "mg-dz01:mg-dz02" && link.Status == serviceability.LinkStatusActivated {
				linkPubkey = base58.Encode(link.PubKey[:])
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "link was not activated within timeout")
	log.Debug("--> Link activated and auto-tagged with UNICAST-DEFAULT", "pubkey", linkPubkey)

	// Clear all topologies from the link to simulate a pre-RFC-18 state.
	log.Debug("==> Clearing all topologies from link (simulating pre-RFC-18)")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", linkPubkey,
		"--link-topology", "default",
	})
	require.NoError(t, err)

	// Verify the link now has no topologies.
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "mg-dz01:mg-dz02" {
				return len(link.LinkTopologies) == 0
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "link topologies were not cleared")
	log.Debug("--> Link topologies cleared (simulates pre-RFC-18 state)")

	// Run migrate in dry-run mode and verify it identifies the untagged link.
	log.Debug("==> Running migrate flex-algo --dry-run")
	out, err := dn.Manager.Exec(ctx, []string{
		"doublezero-admin", "migrate", "flex-algo", "--dry-run",
	})
	require.NoError(t, err)
	output := string(out)
	require.True(t,
		strings.Contains(output, "1 link(s) would be tagged"),
		"dry-run should report 1 link to be tagged, got: %s", output,
	)
	require.True(t,
		strings.Contains(output, "DRY RUN"),
		"dry-run output should include DRY RUN marker, got: %s", output,
	)
	log.Debug("--> Dry-run correctly identified 1 untagged link")

	// Run migrate live.
	log.Debug("==> Running migrate flex-algo (live)")
	out, err = dn.Manager.Exec(ctx, []string{
		"doublezero-admin", "migrate", "flex-algo",
	})
	require.NoError(t, err)
	output = string(out)
	require.True(t,
		strings.Contains(output, "1 link(s) tagged"),
		"live migrate should report 1 link tagged, got: %s", output,
	)
	log.Debug("--> Live migrate completed")

	// Verify the link has UNICAST-DEFAULT again.
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Code == "mg-dz01:mg-dz02" {
				return len(link.LinkTopologies) == 1
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "link was not re-tagged with UNICAST-DEFAULT after migrate")
	log.Debug("--> Link re-tagged with UNICAST-DEFAULT confirmed")
}
