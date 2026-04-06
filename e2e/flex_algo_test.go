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
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

// TestE2E_FlexAlgo_BasicWiring verifies the steady-state flex-algo wiring:
//   - When features.flex_algo.enabled is true, the controller generates
//     "router traffic-engineering", IS-IS flex-algo, and TE admin-group blocks
//   - A WAN link tagged with UNICAST-DEFAULT topology gets the admin-group attribute
//   - Vpnv4 loopbacks with backfilled node segments get the flex-algo node-segment line
//
// This test uses normal operational CLI commands (not the one-time migrate tool):
//   - doublezero link topology create  — to create the topology
//   - doublezero link topology backfill — to add flex-algo node segments to Loopback255
//   - doublezero link update --link-topology — to tag the WAN link
func TestE2E_FlexAlgo_BasicWiring(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	// Write features.yaml enabling flex_algo to a file in the test's temp dir.
	// FeaturesConfigPath is a host-side path volume-mounted into the controller container.
	deployDir := t.TempDir()
	featuresConfigPath := filepath.Join(deployDir, "features.yaml")
	err := os.WriteFile(featuresConfigPath, []byte("features:\n  flex_algo:\n    enabled: true\n"), 0o644)
	require.NoError(t, err)

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: deployDir,

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Controller: devnet.ControllerSpec{
			FeaturesConfigPath: featuresConfigPath,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	ctx := t.Context()

	log.Debug("==> Starting devnet")
	err = dn.Start(ctx, nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Create a Docker network simulating the WAN link between the two devices.
	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ewr1-dz01")
	_, err = linkNetwork.CreateIfNotExists(ctx)
	require.NoError(t, err)

	// Add both devices in parallel. Each device has:
	//   - Ethernet2: physical WAN interface (on the shared link network)
	//   - Loopback255: vpnv4 loopback (required for IS-IS SR flex-algo node segments)
	var wg sync.WaitGroup
	var device1, device2 *devnet.Device

	wg.Add(1)
	go func() {
		defer wg.Done()
		var addErr error
		device1, addErr = dn.AddDevice(ctx, devnet.DeviceSpec{
			Code:                         "la2-dz01",
			Location:                     "lax",
			Exchange:                     "xlax",
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces:                   map[string]string{"Ethernet2": "physical"},
			LoopbackInterfaces:           map[string]string{"Loopback255": "vpnv4"},
		})
		require.NoError(t, addErr)
		log.Debug("--> Device1 added", "id", device1.ID, "code", device1.Spec.Code)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var addErr error
		device2, addErr = dn.AddDevice(ctx, devnet.DeviceSpec{
			Code:                         "ewr1-dz01",
			Location:                     "ewr",
			Exchange:                     "xewr",
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces:                   map[string]string{"Ethernet2": "physical"},
			LoopbackInterfaces:           map[string]string{"Loopback255": "vpnv4"},
		})
		require.NoError(t, addErr)
		log.Debug("--> Device2 added", "id", device2.ID, "code", device2.Spec.Code)
	}()

	wg.Wait()

	// Create the UNICAST-DEFAULT topology. The smart contract auto-assigns
	// admin_group_bit and flex_algo_number.
	log.Debug("==> Creating UNICAST-DEFAULT topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "topology", "create",
		"--name", "unicast-default",
		"--constraint", "include-any",
	})
	require.NoError(t, err)
	log.Debug("--> UNICAST-DEFAULT topology created")

	// Create a WAN link between the two devices and wait for activation.
	log.Debug("==> Creating WAN link onchain")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c",
		`doublezero link create wan \
			--code "la2-dz01:ewr1-dz01" \
			--contributor co01 \
			--side-a la2-dz01 \
			--side-a-interface Ethernet2 \
			--side-z ewr1-dz01 \
			--side-z-interface Ethernet2 \
			--bandwidth "10 Gbps" \
			--mtu 2048 \
			--delay-ms 20 \
			--jitter-ms 2 \
			--desired-status activated \
			-w`,
	})
	require.NoError(t, err)
	log.Debug("--> WAN link created")

	// Wait for the link to be activated onchain and capture its pubkey.
	log.Debug("==> Waiting for link activation")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	var linkPubkey string
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			log.Debug("Failed to get program data", "error", err)
			return false
		}
		for _, link := range data.Links {
			if link.Code == "la2-dz01:ewr1-dz01" {
				if link.Status == serviceability.LinkStatusActivated {
					linkPubkey = base58.Encode(link.PubKey[:])
					return true
				}
				log.Debug("Link not yet activated", "status", link.Status)
				return false
			}
		}
		log.Debug("Link not found yet")
		return false
	}, 60*time.Second, 2*time.Second, "link was not activated within timeout")
	log.Debug("--> Link activated", "pubkey", linkPubkey)

	// Tag the WAN link with the UNICAST-DEFAULT topology.
	// In steady-state operation this is done when the link is provisioned.
	log.Debug("==> Tagging WAN link with UNICAST-DEFAULT topology")
	_, err = dn.Manager.Exec(ctx, []string{
		"doublezero", "link", "update",
		"--pubkey", linkPubkey,
		"--link-topology", "unicast-default",
	})
	require.NoError(t, err)
	log.Debug("--> WAN link tagged")

	// Note: flex-algo node segments are automatically backfilled by the activator when the
	// topology is created — no manual backfill command is required.

	// Poll the controller-rendered config for each device and assert that all
	// expected flex-algo sections are present.
	for _, device := range []*devnet.Device{device1, device2} {
		device := device
		t.Run(fmt.Sprintf("controller_config_%s", device.Spec.Code), func(t *testing.T) {
			log.Debug("==> Checking flex-algo controller config", "device", device.Spec.Code)

			// Expected strings in the EOS config when flex_algo is enabled,
			// the link is tagged, and node segments are backfilled.
			want := []string{
				// router traffic-engineering block
				"router traffic-engineering",
				// UNICAST-DEFAULT admin-group alias
				"administrative-group alias UNICAST-DEFAULT group",
				// IS-IS flex-algo advertisement
				"flex-algo UNICAST-DEFAULT level-2 advertised",
				// Ethernet2 WAN interface TE tagging
				"traffic-engineering administrative-group UNICAST-DEFAULT",
				// Loopback255 flex-algo node segment
				"flex-algo UNICAST-DEFAULT",
			}

			require.Eventually(t, func() bool {
				cfg, fetchErr := dn.Controller.GetAgentConfig(ctx, device.ID)
				if fetchErr != nil {
					log.Debug("Failed to get agent config", "device", device.Spec.Code, "error", fetchErr)
					return false
				}
				config := cfg.Config
				for _, s := range want {
					if !strings.Contains(config, s) {
						log.Debug("Config missing expected section",
							"device", device.Spec.Code,
							"missing", s,
						)
						return false
					}
				}
				return true
			}, 60*time.Second, 2*time.Second,
				"device %s controller config did not contain flex-algo sections within timeout",
				device.Spec.Code,
			)

			log.Debug("--> flex-algo controller config verified", "device", device.Spec.Code)
		})
	}
}
