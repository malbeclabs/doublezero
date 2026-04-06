//go:build e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// linkListEntry is a minimal subset of the JSON shape emitted by
// `doublezero link list --json` / `--json-compact`.
type linkListEntry struct {
	Account        string `json:"account"`
	Code           string `json:"code"`
	LinkTopologies string `json:"link_topologies"`
	UnicastDrained bool   `json:"unicast_drained"`
}

// TestE2E_FlexAlgo_TopologyFilter verifies that `doublezero link list --topology`
// correctly filters links by assigned topology.
func TestE2E_FlexAlgo_TopologyFilter(t *testing.T) {
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
		doublezero device create --code tf-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.101.1" --dz-prefixes "45.33.101.8/29" --desired-status activated 2>&1
		doublezero device create --code tf-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.101.2" --dz-prefixes "45.33.101.16/29" --desired-status activated 2>&1
		doublezero device create --code tf-dz03 --contributor co01 --location fra --exchange xfra --public-ip "45.33.101.3" --dz-prefixes "45.33.101.24/29" --desired-status activated 2>&1
		doublezero device interface create tf-dz01 "Ethernet2" --bandwidth 10Gbps 2>&1
		doublezero device interface create tf-dz02 "Ethernet2" --bandwidth 10Gbps 2>&1
		doublezero device interface create tf-dz03 "Ethernet2" --bandwidth 10Gbps 2>&1
	`})
	require.NoError(t, err)

	// Wait for all three Ethernet2 interfaces to reach Unlinked.
	log.Debug("==> Waiting for interfaces to be unlinked")
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		unlinked := 0
		for _, d := range data.Devices {
			for _, iface := range d.Interfaces {
				if (d.Code == "tf-dz01" || d.Code == "tf-dz02" || d.Code == "tf-dz03") &&
					iface.Name == "Ethernet2" &&
					iface.Status == serviceability.InterfaceStatusUnlinked {
					unlinked++
				}
			}
		}
		return unlinked == 3
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create two WAN links (link1: dz01↔dz02, link2: dz02↔dz03).
	log.Debug("==> Creating WAN links")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero link create wan \
			--code "tf-dz01:tf-dz02" \
			--contributor co01 \
			--side-a tf-dz01 --side-a-interface Ethernet2 \
			--side-z tf-dz02 --side-z-interface Ethernet2 \
			--bandwidth "10 Gbps" --delay-ms 10 --jitter-ms 1 \
			--desired-status activated -w 2>&1
		doublezero link create wan \
			--code "tf-dz02:tf-dz03" \
			--contributor co01 \
			--side-a tf-dz02 --side-a-interface Ethernet2 \
			--side-z tf-dz03 --side-z-interface Ethernet2 \
			--bandwidth "10 Gbps" --delay-ms 20 --jitter-ms 2 \
			--desired-status activated -w 2>&1
	`})
	require.NoError(t, err)

	// Wait for both links to be activated and capture their pubkeys.
	log.Debug("==> Waiting for both links to be activated")
	var link1Pubkey, link2Pubkey string
	require.Eventually(t, func() bool {
		data, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range data.Links {
			if link.Status != serviceability.LinkStatusActivated {
				continue
			}
			switch link.Code {
			case "tf-dz01:tf-dz02":
				link1Pubkey = base58.Encode(link.PubKey[:])
			case "tf-dz02:tf-dz03":
				link2Pubkey = base58.Encode(link.PubKey[:])
			}
		}
		return link1Pubkey != "" && link2Pubkey != ""
	}, 90*time.Second, 2*time.Second, "links were not activated within timeout")
	log.Debug("--> Links activated", "link1", link1Pubkey, "link2", link2Pubkey)

	// Create the filter topology.
	log.Debug("==> Creating filter-alpha topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "filter-alpha",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Before tagging: --topology filter-alpha should return 0 links.
	out, err := dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "list",
		"--topology", "filter-alpha",
		"--json-compact",
	})
	require.NoError(t, err)
	var entries []linkListEntry
	require.NoError(t, json.Unmarshal(out, &entries))
	require.Empty(t, entries, "no links should be tagged with filter-alpha yet")

	// Tag link1 with filter-alpha.
	log.Debug("==> Tagging link1 with filter-alpha")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", link1Pubkey,
		"--link-topology", "filter-alpha",
	})
	require.NoError(t, err)

	// After tagging: --topology filter-alpha returns exactly link1.
	require.Eventually(t, func() bool {
		out, execErr := dn.Manager.Exec(ctx, []string{
			"doublezero", "link", "list",
			"--topology", "filter-alpha",
			"--json-compact",
		})
		if execErr != nil {
			return false
		}
		var e []linkListEntry
		if jsonErr := json.Unmarshal(out, &e); jsonErr != nil {
			return false
		}
		return len(e) == 1 && e[0].Account == link1Pubkey
	}, 30*time.Second, 2*time.Second, "filter-alpha filter should return exactly link1")

	// --topology unicast-default returns both links (both auto-tagged at activation).
	out, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "list",
		"--topology", "unicast-default",
		"--json-compact",
	})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(out, &entries))
	require.Equal(t, 2, len(entries), "--topology unicast-default should return both links")

	// --topology default returns 0 links (both links carry at least unicast-default).
	out, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "list",
		"--topology", "default",
		"--json-compact",
	})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(out, &entries))
	require.Empty(t, entries, "--topology default should return no links (all are tagged)")
}

// TestE2E_FlexAlgo_UnicastDrained verifies that the unicast_drained flag on a link
// can be set and cleared by the contributor, and that the change is reflected in
// the onchain state.
func TestE2E_FlexAlgo_UnicastDrained(t *testing.T) {
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

	// Create UNICAST-DEFAULT before link activation.
	log.Debug("==> Creating UNICAST-DEFAULT topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "unicast-default",
		"--constraint", "include-any",
	})
	require.NoError(t, err)

	// Create devices and interfaces.
	log.Debug("==> Creating devices and interfaces")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code ud-dz01 --contributor co01 --location lax --exchange xlax --public-ip "45.33.102.1" --dz-prefixes "45.33.102.8/29" --desired-status activated 2>&1
		doublezero device create --code ud-dz02 --contributor co01 --location ewr --exchange xewr --public-ip "45.33.102.2" --dz-prefixes "45.33.102.16/29" --desired-status activated 2>&1
		doublezero device interface create ud-dz01 "Ethernet2" --bandwidth 10Gbps 2>&1
		doublezero device interface create ud-dz02 "Ethernet2" --bandwidth 10Gbps 2>&1
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
				if (d.Code == "ud-dz01" || d.Code == "ud-dz02") &&
					iface.Name == "Ethernet2" &&
					iface.Status == serviceability.InterfaceStatusUnlinked {
					unlinked++
				}
			}
		}
		return unlinked == 2
	}, 60*time.Second, 2*time.Second, "interfaces were not unlinked within timeout")

	// Create WAN link.
	log.Debug("==> Creating WAN link")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		doublezero link create wan \
			--code "ud-dz01:ud-dz02" \
			--contributor co01 \
			--side-a ud-dz01 --side-a-interface Ethernet2 \
			--side-z ud-dz02 --side-z-interface Ethernet2 \
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
			if link.Code == "ud-dz01:ud-dz02" && link.Status == serviceability.LinkStatusActivated {
				linkPubkey = base58.Encode(link.PubKey[:])
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "link was not activated within timeout")
	log.Debug("--> Link activated", "pubkey", linkPubkey)

	// Verify unicast_drained starts false.
	data, err := serviceabilityClient.GetProgramData(ctx)
	require.NoError(t, err)
	for _, link := range data.Links {
		if link.Code == "ud-dz01:ud-dz02" {
			require.Equal(t, uint8(0), link.LinkFlags&0x01, "unicast_drained should start as false")
		}
	}

	// Set unicast_drained = true.
	log.Debug("==> Setting unicast_drained = true")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", linkPubkey,
		"--unicast-drained", "true",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		d, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range d.Links {
			if link.Code == "ud-dz01:ud-dz02" {
				return link.LinkFlags&0x01 != 0
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "unicast_drained did not become true")
	log.Debug("--> unicast_drained = true confirmed")

	// Clear unicast_drained.
	log.Debug("==> Clearing unicast_drained")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", linkPubkey,
		"--unicast-drained", "false",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		d, fetchErr := serviceabilityClient.GetProgramData(ctx)
		if fetchErr != nil {
			return false
		}
		for _, link := range d.Links {
			if link.Code == "ud-dz01:ud-dz02" {
				return link.LinkFlags&0x01 == 0
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "unicast_drained did not clear")
	log.Debug("--> unicast_drained cleared")
}
