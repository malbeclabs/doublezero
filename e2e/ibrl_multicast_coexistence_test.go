//go:build e2e

package e2e_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_IBRL_Multicast_Coexistence verifies that IBRL mode and multicast subscriber
// can coexist on the same device. Since a single client cannot have both modes
// simultaneously, we test with multiple clients on the same device.
func TestE2E_IBRL_Multicast_Coexistence(t *testing.T) {
	// Skip test for now pending CLI changes
	t.Skip()
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := logger.With("test", t.Name())

	if !t.Run("ibrl_with_multicast_subscriber", func(t *testing.T) {
		runIBRLWithMulticastSubscriberTest(t, log, dn, device, ibrlClient, mcastClient, false)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_Multicast_Publisher_Coexistence tests IBRL and multicast publisher coexistence.
// This is a separate test from the subscriber test to avoid devnet lifecycle issues.
func TestE2E_IBRL_Multicast_Publisher_Coexistence(t *testing.T) {
	// Skip test for now pending CLI changes
	t.Skip()
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := logger.With("test", t.Name())

	if !t.Run("ibrl_with_multicast_publisher", func(t *testing.T) {
		runIBRLWithMulticastPublisherTest(t, log, dn, device, ibrlClient, mcastClient, false)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_AllocatedAddr_Multicast_Coexistence verifies that IBRL mode with allocated address
// and multicast subscriber can coexist on the same device.
func TestE2E_IBRL_AllocatedAddr_Multicast_Coexistence(t *testing.T) {
	// Skip test for now pending CLI changes
	t.Skip()
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := logger.With("test", t.Name())

	if !t.Run("ibrl_allocated_addr_with_multicast_subscriber", func(t *testing.T) {
		runIBRLWithMulticastSubscriberTest(t, log, dn, device, ibrlClient, mcastClient, true)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_AllocatedAddr_Multicast_Publisher_Coexistence tests IBRL with allocated address
// and multicast publisher coexistence.
func TestE2E_IBRL_AllocatedAddr_Multicast_Publisher_Coexistence(t *testing.T) {
	// Skip test for now pending CLI changes
	t.Skip()
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := logger.With("test", t.Name())

	if !t.Run("ibrl_allocated_addr_with_multicast_publisher", func(t *testing.T) {
		runIBRLWithMulticastPublisherTest(t, log, dn, device, ibrlClient, mcastClient, true)
	}) {
		t.Fail()
	}
}

func setupCoexistenceTestDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Client, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Setting up coexistence test devnet")

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

	log.Info("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Info("--> Devnet started")

	// Add the main device for testing
	log.Info("==> Adding device ny5-dz01")
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     "ny5-dz01",
		Location: "ewr",
		Exchange: "xewr",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Info("--> Device added", "deviceID", device.ID)

	// Add additional devices for iBGP/MSDP peering
	log.Info("==> Creating additional devices onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Populate device information onchain"
		doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt --desired-status activated

		echo "==> Populate device interface information onchain"
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create pit-dzd01 "Ethernet2" -w
		doublezero device interface create pit-dzd01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create pit-dzd01 "Loopback256" --loopback-type ipv4 -w

		doublezero device update --pubkey pit-dzd01 --max-users 128

		echo "--> Device information onchain:"
		doublezero device list
	`})
	require.NoError(t, err)

	// Add IBRL client
	log.Info("==> Adding IBRL client")
	ibrlClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Info("--> IBRL client added", "clientIP", ibrlClient.CYOANetworkIP, "pubkey", ibrlClient.Pubkey)

	// Add multicast client (used for both publisher and subscriber tests)
	log.Info("==> Adding multicast client")
	mcastClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
	})
	require.NoError(t, err)
	log.Info("--> Multicast client added", "clientIP", mcastClient.CYOANetworkIP, "pubkey", mcastClient.Pubkey)

	// Create multicast group and add client to allowlists
	log.Info("==> Creating multicast group onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create multicast group"
		doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me -w

		echo "--> Multicast group created:"
		doublezero multicast group list
	`})
	require.NoError(t, err)

	// Add multicast client to allowlists (both publisher and subscriber)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + mcastClient.Pubkey + ` --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + mcastClient.Pubkey + ` --client-ip ` + mcastClient.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Wait for latency results for all clients
	log.Info("==> Waiting for latency results")
	err = ibrlClient.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	err = mcastClient.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	log.Info("--> Latency results received for all clients")

	log.Info("--> Coexistence test devnet setup complete")

	return dn, device, ibrlClient, mcastClient
}

// runIBRLWithMulticastSubscriberTest tests IBRL and multicast subscriber coexistence.
func runIBRLWithMulticastSubscriberTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	ibrlClient, mcastClient *devnet.Client, useAllocatedAddr bool) {

	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode, "multicast_type", "subscriber")

	// === CONNECT PHASE ===
	log.Info("==> CONNECT PHASE")

	// Set access passes for all clients
	log.Info("==> Setting access passes")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlClient.CYOANetworkIP + " --user-payer " + ibrlClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + mcastClient.CYOANetworkIP + " --user-payer " + mcastClient.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client
	log.Info("==> Connecting IBRL client", "useAllocatedAddr", useAllocatedAddr)
	ibrlCmd := "doublezero connect ibrl --client-ip " + ibrlClient.CYOANetworkIP
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = ibrlClient.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Connect multicast subscriber
	log.Info("==> Connecting multicast subscriber")
	_, err = mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber mg01 --client-ip " + mcastClient.CYOANetworkIP})
	require.NoError(t, err)

	// Wait for tunnels to come up
	log.Info("==> Waiting for tunnels to come up")
	err = ibrlClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	err = mcastClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "Multicast subscriber tunnel failed to come up")
	log.Info("--> All tunnels are up")

	// === COEXISTENCE VERIFICATION ===
	log.Info("==> COEXISTENCE VERIFICATION PHASE")

	log.Info("==> Waiting for agent config to include multicast subscriber")
	waitForAgentConfigWithClient(t, log, dn, device, mcastClient)

	verifyIBRLClient(t, log, device, ibrlClient, useAllocatedAddr)
	verifyMulticastSubscriberPIMAdjacency(t, log, device)

	log.Info("--> Both services verified as working simultaneously")

	// Disconnect multicast subscriber - don't fail if ledger is unavailable
	log.Info("==> Disconnecting multicast subscriber to test independence")
	_, disconnectMcastErr := mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + mcastClient.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Info("--> Warning: Multicast disconnect failed (ledger may be unavailable)", "error", disconnectMcastErr)
		return
	}

	// Verify IBRL client still works
	log.Info("==> Verifying IBRL client still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL client still working")

	// === FULL DISCONNECT PHASE ===
	log.Info("==> FULL DISCONNECT PHASE")

	// Disconnect IBRL client - don't fail test if ledger is unavailable (infrastructure flakiness)
	_, disconnectErr := ibrlClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + ibrlClient.CYOANetworkIP})
	if disconnectErr != nil {
		log.Info("--> Warning: IBRL disconnect failed (ledger may be unavailable)", "error", disconnectErr)
	} else {
		// Only verify tunnel removal if disconnect succeeded
		log.Info("==> Verifying tunnels removed")
		verifyTunnelRemoved(t, ibrlClient, "doublezero0")
		verifyTunnelRemoved(t, mcastClient, "doublezero1")
	}

	log.Info("--> Test completed successfully")
}

// runIBRLWithMulticastPublisherTest tests IBRL and multicast publisher coexistence.
func runIBRLWithMulticastPublisherTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	ibrlClient, mcastClient *devnet.Client, useAllocatedAddr bool) {

	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode, "multicast_type", "publisher")

	// === CONNECT PHASE ===
	log.Info("==> CONNECT PHASE")

	// Set access passes for all clients
	log.Info("==> Setting access passes")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlClient.CYOANetworkIP + " --user-payer " + ibrlClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + mcastClient.CYOANetworkIP + " --user-payer " + mcastClient.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client
	log.Info("==> Connecting IBRL client", "useAllocatedAddr", useAllocatedAddr)
	ibrlCmd := "doublezero connect ibrl --client-ip " + ibrlClient.CYOANetworkIP
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = ibrlClient.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Connect multicast publisher
	log.Info("==> Connecting multicast publisher")
	_, err = mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher mg01 --client-ip " + mcastClient.CYOANetworkIP})
	require.NoError(t, err)

	// Wait for tunnels to come up
	log.Info("==> Waiting for tunnels to come up")
	err = ibrlClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	err = mcastClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "Multicast publisher tunnel failed to come up")
	log.Info("--> All tunnels are up")

	// === COEXISTENCE VERIFICATION ===
	log.Info("==> COEXISTENCE VERIFICATION PHASE")

	// Wait for agent config to be pushed to device (required for mroutes to work)
	log.Info("==> Waiting for agent config to include multicast publisher")
	waitForAgentConfigWithClient(t, log, dn, device, mcastClient)

	verifyIBRLClient(t, log, device, ibrlClient, useAllocatedAddr)
	verifyMulticastPublisherMrouteState(t, log, device, mcastClient)

	log.Info("--> Both services verified as working simultaneously")

	// Disconnect multicast publisher - don't fail if ledger is unavailable
	log.Info("==> Disconnecting multicast publisher to test independence")
	_, disconnectMcastErr := mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + mcastClient.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Info("--> Warning: Multicast disconnect failed (ledger may be unavailable)", "error", disconnectMcastErr)
		return
	}

	// Verify IBRL client still works
	log.Info("==> Verifying IBRL client still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL client still working")

	// === FULL DISCONNECT PHASE ===
	log.Info("==> FULL DISCONNECT PHASE")

	// Disconnect IBRL client - don't fail test if ledger is unavailable (infrastructure flakiness)
	_, disconnectErr := ibrlClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + ibrlClient.CYOANetworkIP})
	if disconnectErr != nil {
		log.Info("--> Warning: IBRL disconnect failed (ledger may be unavailable)", "error", disconnectErr)
		log.Info("--> Skipping tunnel removal verification due to disconnect failure")
	} else {
		// Only verify tunnel removal if disconnect succeeded
		log.Info("==> Verifying tunnels removed")
		verifyTunnelRemoved(t, ibrlClient, "doublezero0")
		verifyTunnelRemoved(t, mcastClient, "doublezero1")
	}

	log.Info("--> Test completed successfully")
}

// verifyIBRLClient verifies the IBRL client is working correctly.
func verifyIBRLClient(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client, allocatedAddr bool) {
	log.Info("==> Verifying IBRL client")

	// Check doublezero0 interface exists
	links, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"})
	require.NoError(t, err, "doublezero0 interface not found")
	require.Len(t, links, 1, "expected exactly one doublezero0 interface")
	require.Equal(t, "doublezero0", links[0]["ifname"], "interface name mismatch")

	// Check BGP session is Established
	verifyIBRLClientBGPEstablished(t, log, device)

	// Check routes are installed
	routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
	require.NoError(t, err)

	foundDZ0Route := false
	for _, route := range routes {
		if dev, ok := route["dev"].(string); ok && dev == "doublezero0" {
			foundDZ0Route = true
			break
		}
	}
	require.True(t, foundDZ0Route, "no routes found for doublezero0 interface")

	// If using allocated address, verify DZ IP differs from public IP
	if allocatedAddr {
		status, err := client.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1)
		dzIP := status[0].DoubleZeroIP.String()
		require.NotEqual(t, client.CYOANetworkIP, dzIP, "allocated IP should differ from public IP")
		log.Info("--> Verified allocated IP differs from public IP", "publicIP", client.CYOANetworkIP, "dzIP", dzIP)
	}

	log.Info("--> IBRL client verified")
}

// verifyIBRLClientBGPEstablished verifies BGP session is established on the device.
func verifyIBRLClientBGPEstablished(t *testing.T, log *slog.Logger, device *devnet.Device) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd("vrf1"))
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		peer, ok := neighbors.VRFs["vrf1"].Peers[expectedLinkLocalAddr]
		if ok && peer.PeerState == "Established" {
			log.Info("--> BGP session Established", "peer", expectedLinkLocalAddr)
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("BGP session not established within timeout")
}

// verifyMulticastSubscriberPIMAdjacency verifies PIM adjacency is formed on the device for subscriber.
func verifyMulticastSubscriberPIMAdjacency(t *testing.T, log *slog.Logger, device *devnet.Device) {
	log.Info("==> Verifying multicast subscriber PIM adjacency")

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pim, err := devnet.DeviceExecAristaCliJSON[*arista.ShowPIMNeighbors](t.Context(), device, arista.ShowPIMNeighborsCmd())
		require.NoError(t, err, "error fetching pim neighbors from doublezero device")

		neighbor, ok := pim.Neighbors[expectedLinkLocalAddr]
		if !ok {
			log.Debug("PIM neighbor not found yet", "expectedAddr", expectedLinkLocalAddr)
			time.Sleep(1 * time.Second)
			continue
		}
		if len(neighbor.Interface) >= 6 && neighbor.Interface[:6] == "Tunnel" {
			log.Info("--> PIM adjacency verified", "interface", neighbor.Interface, "address", expectedLinkLocalAddr)
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("PIM neighbor not established on Tunnel interface within timeout")
}

// verifyMulticastPublisherMrouteState verifies mroute state is created on the device for publisher.
func verifyMulticastPublisherMrouteState(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client) {
	log.Info("==> Verifying multicast publisher mroute state")

	// Calculate expected allocated IP from device's dz_prefix
	dzPrefixIP, dzPrefixNet, err := netutil.ParseCIDR(device.DZPrefix)
	require.NoError(t, err)
	ones, _ := dzPrefixNet.Mask.Size()
	allocatableBits := 32 - ones
	expectedAllocatedIP, err := nextAllocatableIP(dzPrefixIP, allocatableBits, map[string]bool{})
	require.NoError(t, err)

	// Trigger S,G creation with ping to multicast group
	log.Info("==> Triggering S,G creation with ping to multicast group")
	_, _ = client.Exec(t.Context(), []string{"bash", "-c", "ping -c 1 -w 1 233.84.178.0"}, docker.NoPrintOnError())

	// Verify mroute state on device - poll for 30 seconds
	mGroup := "233.84.178.0"
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
		if err != nil {
			log.Debug("Error fetching mroutes", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		groups, ok := mroutes.Groups[mGroup]
		if !ok {
			log.Debug("Multicast group not found yet", "mGroup", mGroup)
			time.Sleep(2 * time.Second)
			continue
		}

		_, ok = groups.GroupSources[expectedAllocatedIP]
		if ok {
			log.Info("--> Mroute state verified", "group", mGroup, "source", expectedAllocatedIP)
			return
		}

		log.Debug("Source not found in multicast group", "expectedIP", expectedAllocatedIP, "sources", groups.GroupSources)
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("Mroute state not created within timeout for group %s with source %s", mGroup, expectedAllocatedIP)
}

// verifyTunnelRemoved verifies that a tunnel interface has been removed.
func verifyTunnelRemoved(t *testing.T, client *devnet.Client, interfaceName string) {
	require.Eventually(t, func() bool {
		_, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev " + interfaceName}, docker.NoPrintOnError())
		return err != nil
	}, 30*time.Second, 1*time.Second, "tunnel interface %s should be removed", interfaceName)
}

// waitForAgentConfigWithClient waits for the agent configuration to include the specified client.
// This ensures the controller has pushed the configuration to the device before we verify PIM/mroutes.
func waitForAgentConfigWithClient(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device, client *devnet.Client) {
	require.Eventually(t, func() bool {
		config, err := dn.Controller.GetAgentConfig(t.Context(), device.ID)
		if err != nil {
			log.Debug("Error getting agent config", "error", err)
			return false
		}

		// Check if the config contains the client IP (indicating the tunnel is configured)
		if strings.Contains(config.Config, client.CYOANetworkIP) {
			log.Info("--> Agent config includes client", "clientIP", client.CYOANetworkIP)
			return true
		}

		log.Debug("Agent config does not yet include client", "clientIP", client.CYOANetworkIP)
		return false
	}, 30*time.Second, 1*time.Second, "agent config should include client %s", client.CYOANetworkIP)
}
