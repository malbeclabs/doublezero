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
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_IBRL_Multicast_Coexistence verifies that IBRL mode and multicast subscriber
// can coexist on the same device. Since a single client cannot have both modes
// simultaneously, we test with multiple clients on the same device.
func TestE2E_IBRL_Multicast_Coexistence(t *testing.T) {
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("ibrl_with_multicast_subscriber", func(t *testing.T) {
		runIBRLWithMulticastSubscriberTest(t, log, dn, device, ibrlClient, mcastClient, false)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_Multicast_Publisher_Coexistence tests IBRL and multicast publisher coexistence.
// This is a separate test from the subscriber test to avoid devnet lifecycle issues.
func TestE2E_IBRL_Multicast_Publisher_Coexistence(t *testing.T) {
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("ibrl_with_multicast_publisher", func(t *testing.T) {
		runIBRLWithMulticastPublisherTest(t, log, dn, device, ibrlClient, mcastClient, false)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_AllocatedAddr_Multicast_Coexistence verifies that IBRL mode with allocated address
// and multicast subscriber can coexist on the same device.
func TestE2E_IBRL_AllocatedAddr_Multicast_Coexistence(t *testing.T) {
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("ibrl_allocated_addr_with_multicast_subscriber", func(t *testing.T) {
		runIBRLWithMulticastSubscriberTest(t, log, dn, device, ibrlClient, mcastClient, true)
	}) {
		t.Fail()
	}
}

// TestE2E_IBRL_AllocatedAddr_Multicast_Publisher_Coexistence tests IBRL with allocated address
// and multicast publisher coexistence.
func TestE2E_IBRL_AllocatedAddr_Multicast_Publisher_Coexistence(t *testing.T) {
	t.Parallel()

	dn, device, ibrlClient, mcastClient := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("ibrl_allocated_addr_with_multicast_publisher", func(t *testing.T) {
		runIBRLWithMulticastPublisherTest(t, log, dn, device, ibrlClient, mcastClient, true)
	}) {
		t.Fail()
	}
}

// TestE2E_SingleClient_IBRL_Multicast_PubSub_Swap tests swapping between subscriber and publisher
// on a single client. This verifies that a client can:
// 1. Connect IBRL
// 2. Add subscription → verify → remove
// 3. Add publisher → verify → remove
// 4. Re-add subscriber → verify
// 5. Disconnect
func TestE2E_SingleClient_IBRL_Multicast_PubSub_Swap(t *testing.T) {
	t.Parallel()

	dn, device, mcastDevice, client := setupSingleClientTestDevnet(t)
	log := logger.With("test", t.Name())

	if !t.Run("single_client_ibrl_multicast_pubsub_swap", func(t *testing.T) {
		runSingleClientMulticastPubSubSwapTest(t, log, dn, device, mcastDevice, client)
	}) {
		t.Fail()
	}
}

// runSingleClientMulticastPubSubSwapTest tests swapping between subscriber and publisher
// on a single client with IBRL already connected.
func runSingleClientMulticastPubSubSwapTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	mcastDevice *devnet.Device, client *devnet.Client,
) {
	log = log.With("mode", "pubsub_swap")

	// === CONNECT PHASE 1: IBRL ===
	log.Info("==> CONNECT PHASE 1: IBRL")

	// Set access pass for the client
	log.Info("==> Setting access pass")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client
	log.Info("==> Connecting client with IBRL")
	ibrlCmd := "doublezero connect ibrl --client-ip " + client.CYOANetworkIP
	_, err = client.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Wait for IBRL tunnel to come up
	log.Info("==> Waiting for IBRL tunnel to come up")
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	log.Info("--> IBRL tunnel is up")

	// Determine which device the IBRL tunnel connected to
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel")
	ibrlTunnelDst := tunnelStatus[0].TunnelDst.String()
	log.Info("==> IBRL tunnel destination", "tunnelDst", ibrlTunnelDst)

	// Determine actual IBRL and multicast devices based on tunnel destination
	var ibrlDevice, actualMcastDevice *devnet.Device
	if ibrlTunnelDst == device.CYOANetworkIP {
		ibrlDevice = device
		actualMcastDevice = mcastDevice
	} else if ibrlTunnelDst == mcastDevice.CYOANetworkIP {
		ibrlDevice = mcastDevice
		actualMcastDevice = device
	} else {
		t.Fatalf("IBRL tunnel destination %s doesn't match any device (device=%s, mcastDevice=%s)",
			ibrlTunnelDst, device.CYOANetworkIP, mcastDevice.CYOANetworkIP)
	}
	log.Info("==> Device assignment", "ibrlDevice", ibrlDevice.Spec.Code, "mcastDevice", actualMcastDevice.Spec.Code)

	log.Info("==> Verifying IBRL client")
	verifyIBRLClient(t, log, ibrlDevice, client, false)
	log.Info("--> IBRL client verified")

	log.Info("==> PHASE 2: Adding multicast subscriber")

	mcastCmd := "doublezero connect multicast subscriber mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	mcastOutput, err := client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast subscriber connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	log.Info("==> Waiting for agent config to be pushed to multicast device")
	waitForAgentConfigWithClient(t, log, dn, actualMcastDevice, client)
	log.Info("--> Agent config pushed")

	log.Info("==> Waiting for agent to apply configuration")
	time.Sleep(10 * time.Second)

	log.Info("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels should be up")
	log.Info("--> Both tunnels are up")

	log.Info("==> Verifying subscriber PIM adjacency")
	verifyConcurrentMulticastPIMAdjacency(t, log, actualMcastDevice)
	log.Info("--> Subscriber PIM adjacency verified")

	log.Info("==> PHASE 3: Removing multicast subscriber")

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Multicast subscriber disconnected")

	log.Info("==> Verifying IBRL still works after subscriber disconnect")
	verifyIBRLClientBGPEstablished(t, log, ibrlDevice)
	log.Info("--> IBRL still working")

	log.Info("==> Waiting for multicast tunnel to be removed")
	time.Sleep(5 * time.Second)

	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel after subscriber disconnect")
	log.Info("--> Back to single IBRL tunnel")

	log.Info("==> PHASE 4: Adding multicast publisher")

	mcastCmd = "doublezero connect multicast publisher mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	mcastOutput, err = client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast publisher connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	log.Info("==> Waiting for agent config to be pushed to multicast device")
	waitForAgentConfigWithClient(t, log, dn, actualMcastDevice, client)
	log.Info("--> Agent config pushed")

	log.Info("==> Waiting for agent to apply configuration")
	time.Sleep(10 * time.Second)

	log.Info("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels should be up")
	log.Info("--> Both tunnels are up")

	log.Info("==> Verifying publisher mroute state")
	verifyConcurrentMulticastPublisherMrouteState(t, log, actualMcastDevice, client)
	log.Info("--> Publisher mroute state verified")

	log.Info("==> PHASE 5: Removing multicast publisher")

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Multicast publisher disconnected")

	log.Info("==> Verifying IBRL still works after publisher disconnect")
	verifyIBRLClientBGPEstablished(t, log, ibrlDevice)
	log.Info("--> IBRL still working")

	log.Info("==> Waiting for multicast tunnel to be removed")
	time.Sleep(5 * time.Second)

	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel after publisher disconnect")
	log.Info("--> Back to single IBRL tunnel")

	log.Info("==> PHASE 6: Re-adding multicast subscriber (swap from publisher back to subscriber)")

	mcastCmd = "doublezero connect multicast subscriber mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	mcastOutput, err = client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast subscriber connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	log.Info("==> Waiting for agent config to be pushed to multicast device")
	waitForAgentConfigWithClient(t, log, dn, actualMcastDevice, client)
	log.Info("--> Agent config pushed")

	log.Info("==> Waiting for agent to apply configuration")
	time.Sleep(10 * time.Second)

	log.Info("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels should be up")
	log.Info("--> Both tunnels are up")

	log.Info("==> Verifying subscriber PIM adjacency after re-add")
	verifyConcurrentMulticastPIMAdjacency(t, log, actualMcastDevice)
	log.Info("--> Subscriber PIM adjacency verified after swap")

	log.Info("==> DISCONNECT PHASE")

	log.Info("==> Disconnecting multicast")
	_, disconnectMcastErr := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Info("--> Warning: Multicast disconnect failed", "error", disconnectMcastErr)
	}

	log.Info("==> Disconnecting IBRL")
	_, disconnectErr := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + client.CYOANetworkIP})
	if disconnectErr != nil {
		log.Info("--> Warning: IBRL disconnect failed", "error", disconnectErr)
	} else {
		log.Info("==> Verifying tunnel removed")
		verifyTunnelRemoved(t, client, "doublezero0")
	}

	log.Info("--> Pub/sub swap test completed successfully")
}

// TestE2E_SingleClient_IBRL_AllocatedAddr_Then_Multicast tests a single client with
// allocated address connecting to IBRL first, then adding multicast.
func TestE2E_SingleClient_IBRL_AllocatedAddr_Then_Multicast(t *testing.T) {
	t.Parallel()

	dn, device, mcastDevice, client := setupSingleClientTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("single_client_ibrl_allocated_then_multicast_subscriber", func(t *testing.T) {
		runSingleClientIBRLThenMulticastTest(t, log, dn, device, mcastDevice, client, true, false)
	}) {
		t.Fail()
	}
}

// TestE2E_SingleClient_IBRL_Then_Multicast_Publisher tests a single client connecting
// to IBRL first, then adding multicast publisher capability.
func TestE2E_SingleClient_IBRL_Then_Multicast_Publisher(t *testing.T) {
	t.Parallel()

	dn, device, mcastDevice, client := setupSingleClientTestDevnet(t)
	log := newTestLoggerForTest(t)

	if !t.Run("single_client_ibrl_then_multicast_publisher", func(t *testing.T) {
		runSingleClientIBRLThenMulticastTest(t, log, dn, device, mcastDevice, client, false, true)
	}) {
		t.Fail()
	}
}

// TestE2E_Multicast_PublisherAndSubscriber verifies that a single client can be
// both a multicast publisher and subscriber simultaneously using the
// --publish and --subscribe flags.
func TestE2E_Multicast_PublisherAndSubscriber(t *testing.T) {
	t.Parallel()

	dn, device, _, client := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	// Connect as both publisher and subscriber using new flags
	log.Debug("==> Connecting as both publisher and subscriber")
	cmd := "doublezero connect multicast --publish mg01 --subscribe mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	output, err := client.Exec(t.Context(), []string{"bash", "-c", cmd})
	log.Debug("==> Connect output", "output", string(output))
	require.NoError(t, err, "should be able to connect as both publisher and subscriber")

	// Wait for tunnel to come up
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "tunnel should come up")
	log.Debug("--> Tunnel is up")

	// Verify tunnel status shows multicast type
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "should have exactly one tunnel")
	require.Equal(t, devnet.ClientUserTypeMulticast, tunnelStatus[0].UserType)

	// Wait for agent config to be pushed
	log.Debug("==> Waiting for agent config")
	waitForAgentConfigWithClient(t, log, dn, device, client)

	// Verify publisher behavior: S,G mroute state
	log.Debug("==> Verifying publisher mroute state")
	verifyMulticastPublisherMrouteState(t, log, device, client)

	// Verify subscriber behavior: PIM adjacency
	log.Debug("==> Verifying subscriber PIM adjacency")
	verifyMulticastSubscriberPIMAdjacency(t, log, device)

	log.Debug("--> Both publisher and subscriber verified on single client")

	// Disconnect
	log.Debug("==> Disconnecting")
	disconnectOutput, disconnectErr := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP + " 2>&1"})
	log.Debug("==> Disconnect output", "output", string(disconnectOutput))
	require.NoError(t, disconnectErr, "disconnect should succeed")
	verifyTunnelRemoved(t, client, "doublezero1")

	log.Debug("--> Test completed successfully")
}

// setupSingleClientTestDevnet sets up a devnet with a single client for testing
// concurrent IBRL+multicast on the same user account.
func setupSingleClientTestDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Device, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Setting up single client test devnet")

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

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Add the main device for IBRL testing
	log.Debug("==> Adding device ny5-dz01")
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "ny5-dz01",
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Debug("--> Device added", "deviceID", device.ID)

	// Add second device for multicast (separate device required for concurrent tunnels)
	log.Debug("==> Adding device pit-dz01")
	mcastDevice, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "pit-dz01",
		Location:                     "pit",
		Exchange:                     "xpit",
		CYOANetworkIPHostID:          16,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Debug("--> Multicast device added", "deviceID", mcastDevice.ID)

	// Add additional devices for iBGP/MSDP peering
	log.Debug("==> Creating additional devices onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Populate device interface information onchain"
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create pit-dz01 "Ethernet2" -w
		doublezero device interface create pit-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create pit-dz01 "Loopback256" --loopback-type ipv4 -w

		echo "--> Device information onchain:"
		doublezero device list
	`})
	require.NoError(t, err)

	// Add single client that will be used for both IBRL and multicast
	log.Debug("==> Adding client for dual-mode testing")
	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Debug("--> Client added", "clientIP", client.CYOANetworkIP, "pubkey", client.Pubkey)

	// Create multicast group and add client to allowlists
	log.Debug("==> Creating multicast group onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create multicast group"
		doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me -w

		echo "--> Multicast group created:"
		doublezero multicast group list
	`})
	require.NoError(t, err)

	// Add client to multicast allowlists (both publisher and subscriber)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Wait for latency results from both devices
	log.Debug("==> Waiting for latency results")
	err = client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	err = client.WaitForLatencyResults(t.Context(), mcastDevice.ID, 75*time.Second)
	require.NoError(t, err)
	log.Debug("--> Latency results received from both devices")

	log.Debug("--> Single client test devnet setup complete")

	return dn, device, mcastDevice, client
}

// runSingleClientIBRLThenMulticastTest tests a single client connecting with IBRL first,
// then adding multicast capability. The IBRL tunnel goes to device and multicast goes to mcastDevice.
func runSingleClientIBRLThenMulticastTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	mcastDevice *devnet.Device, client *devnet.Client, useAllocatedAddr bool, asPublisher bool,
) {
	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	mcastType := "subscriber"
	if asPublisher {
		mcastType = "publisher"
	}
	log = log.With("mode", mode, "multicast_type", mcastType)

	// === CONNECT PHASE 1: IBRL ===
	log.Debug("==> CONNECT PHASE 1: IBRL")

	// Set access pass for the client
	log.Debug("==> Setting access pass")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client (latency-based device selection)
	log.Debug("==> Connecting client with IBRL", "useAllocatedAddr", useAllocatedAddr)
	ibrlCmd := "doublezero connect ibrl --client-ip " + client.CYOANetworkIP
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = client.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Wait for IBRL tunnel to come up
	log.Debug("==> Waiting for IBRL tunnel to come up")
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	log.Debug("--> IBRL tunnel is up")

	// Determine which device the IBRL tunnel connected to (may differ from expected due to latency selection)
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel")
	ibrlTunnelDst := tunnelStatus[0].TunnelDst.String()
	log.Debug("==> IBRL tunnel destination", "tunnelDst", ibrlTunnelDst)

	// Determine actual IBRL and multicast devices based on tunnel destination
	var ibrlDevice, actualMcastDevice *devnet.Device
	if ibrlTunnelDst == device.CYOANetworkIP {
		ibrlDevice = device
		actualMcastDevice = mcastDevice
	} else if ibrlTunnelDst == mcastDevice.CYOANetworkIP {
		ibrlDevice = mcastDevice
		actualMcastDevice = device
	} else {
		t.Fatalf("IBRL tunnel destination %s doesn't match any device (device=%s, mcastDevice=%s)",
			ibrlTunnelDst, device.CYOANetworkIP, mcastDevice.CYOANetworkIP)
	}
	log.Debug("==> Device assignment", "ibrlDevice", ibrlDevice.Spec.Code, "mcastDevice", actualMcastDevice.Spec.Code)

	// Verify IBRL is working on the actual device
	log.Debug("==> Verifying IBRL client")
	verifyIBRLClient(t, log, ibrlDevice, client, useAllocatedAddr)
	log.Debug("--> IBRL client verified")

	// === CONNECT PHASE 2: Add Multicast (creates separate Multicast user) ===
	log.Debug("==> CONNECT PHASE 2: Adding multicast (will create separate Multicast user)")

	// Connect multicast (creates a NEW Multicast user, separate from IBRL user)
	var mcastCmd string
	if asPublisher {
		log.Debug("==> Adding multicast publisher subscription")
		mcastCmd = "doublezero connect multicast publisher mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	} else {
		log.Debug("==> Adding multicast subscriber subscription")
		mcastCmd = "doublezero connect multicast subscriber mg01 --client-ip " + client.CYOANetworkIP + " 2>&1"
	}
	mcastOutput, err := client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Debug("==> Multicast connect command output", "output", string(mcastOutput))
	require.NoError(t, err)

	// List users to see if the Multicast user was created
	listUsersCmd := "doublezero user list --client-ip " + client.CYOANetworkIP + " 2>&1"
	listOutput, _ := client.Exec(t.Context(), []string{"bash", "-c", listUsersCmd})
	log.Debug("==> Users after multicast connect", "output", string(listOutput))

	// Wait for agent config to be pushed to the multicast device BEFORE waiting for tunnel
	// This is critical because the device needs to have the tunnel configured before the
	// client's BGP session can be established
	log.Debug("==> Waiting for agent config to be pushed to multicast device",
		"device", actualMcastDevice.Spec.Code,
		"deviceID", actualMcastDevice.ID,
		"deviceIP", actualMcastDevice.CYOANetworkIP,
		"ibrlDeviceCode", ibrlDevice.Spec.Code,
		"ibrlDeviceID", ibrlDevice.ID,
		"ibrlDeviceIP", ibrlDevice.CYOANetworkIP)
	waitForAgentConfigWithClient(t, log, dn, actualMcastDevice, client)
	log.Info("--> Agent config pushed to multicast device")

	// Give time for agent to apply the configuration
	log.Info("==> Waiting for agent to apply configuration")

	// Wait for BOTH tunnels (IBRL and Multicast) to be up on the client
	log.Debug("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels (IBRL and Multicast) should be up")
	log.Debug("--> Both tunnels are up")

	// Log tunnel status for debugging
	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	log.Debug("==> Tunnel status after multicast connect", "tunnels", tunnelStatus)

	// Verify CLI status shows correct device/metro for all tunnels
	log.Debug("==> Verifying CLI status shows correct device/metro for all tunnels")
	cliStatus, err := client.GetCLIStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, cliStatus, 2, "expected two tunnels in CLI status")

	for _, status := range cliStatus {
		switch status.Response.UserType {
		case devnet.ClientUserTypeIBRL, devnet.ClientUserTypeIBRLWithAllocated:
			require.NotEqual(t, "N/A", status.CurrentDevice,
				"IBRL tunnel should have a current_device, got N/A")
			require.NotEqual(t, "N/A", status.Metro,
				"IBRL tunnel should have a metro, got N/A")
		case devnet.ClientUserTypeMulticast:
			// Both publishers and subscribers can be matched to a device via tunnel_dst (device public IP)
			require.NotEqual(t, "N/A", status.CurrentDevice,
				"Multicast tunnel should have a current_device, got N/A")
			require.NotEqual(t, "N/A", status.Metro,
				"Multicast tunnel should have a metro, got N/A")
		default:
			t.Fatalf("unexpected user type in CLI status: %s", status.Response.UserType)
		}
	}
	log.Debug("--> CLI status verified: all tunnels have device/metro info")

	// === VERIFICATION PHASE ===
	log.Debug("==> VERIFICATION PHASE: Both tunnels should work")

	// Verify IBRL still works on its device
	log.Debug("==> Verifying IBRL still works after adding multicast", "ibrlDevice", ibrlDevice.Spec.Code)
	verifyIBRLClientBGPEstablished(t, log, ibrlDevice)
	log.Debug("--> IBRL BGP still established")

	// Verify multicast is working on the other device
	// Note: Agent config was already pushed and applied before waiting for tunnels above

	if asPublisher {
		// Publishers don't use PIM - they just send traffic and the device creates (S,G) state
		verifyConcurrentMulticastPublisherMrouteState(t, log, actualMcastDevice, client)
	} else {
		// Subscribers need PIM adjacency to receive multicast traffic
		verifyConcurrentMulticastPIMAdjacency(t, log, actualMcastDevice)
		log.Debug("--> PIM adjacency established on multicast device")
	}

	log.Debug("--> Both IBRL and multicast verified as working on same client (separate tunnels)")

	// === DISCONNECT PHASE ===
	log.Debug("==> DISCONNECT PHASE")

	// Disconnect multicast first
	log.Debug("==> Disconnecting multicast")
	_, disconnectMcastErr := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Debug("--> Warning: Multicast disconnect failed (ledger may be unavailable)", "error", disconnectMcastErr)
	}

	// Verify IBRL still works after multicast disconnect
	log.Debug("==> Verifying IBRL still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, ibrlDevice)
	log.Debug("--> IBRL still working after multicast disconnect")

	// Disconnect IBRL
	log.Debug("==> Disconnecting IBRL")
	_, disconnectErr := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + client.CYOANetworkIP})
	if disconnectErr != nil {
		log.Debug("--> Warning: IBRL disconnect failed (ledger may be unavailable)", "error", disconnectErr)
	} else {
		// Only verify tunnel removal if disconnect succeeded
		log.Debug("==> Verifying tunnel removed")
		verifyTunnelRemoved(t, client, "doublezero0")
	}

	log.Debug("--> Single client IBRL+multicast test completed successfully")
}

func setupCoexistenceTestDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Client, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Setting up coexistence test devnet")

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

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Add the main device for testing
	log.Debug("==> Adding device ny5-dz01")
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     "ny5-dz01",
		Location: "ewr",
		Exchange: "xewr",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Debug("--> Device added", "deviceID", device.ID)

	// Add additional devices for iBGP/MSDP peering
	log.Debug("==> Creating additional devices onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail
		doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt --desired-status activated
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create pit-dzd01 "Ethernet2" -w
		doublezero device interface create pit-dzd01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create pit-dzd01 "Loopback256" --loopback-type ipv4 -w
		doublezero device update --pubkey pit-dzd01 --max-users 128
	`})
	require.NoError(t, err)

	// Add IBRL client
	log.Debug("==> Adding IBRL client")
	ibrlClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Debug("--> IBRL client added", "clientIP", ibrlClient.CYOANetworkIP, "pubkey", ibrlClient.Pubkey)

	// Add multicast client (used for both publisher and subscriber tests)
	log.Debug("==> Adding multicast client")
	mcastClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
	})
	require.NoError(t, err)
	log.Debug("--> Multicast client added", "clientIP", mcastClient.CYOANetworkIP, "pubkey", mcastClient.Pubkey)

	// Create multicast group and add client to allowlists
	log.Debug("==> Creating multicast group onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail
		doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me -w
	`})
	require.NoError(t, err)

	// Add both clients to allowlists (both publisher and subscriber)
	// ibrlClient is used for the XOR constraint test as well as IBRL tests
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + ibrlClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + ibrlClient.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + ibrlClient.Pubkey + ` --client-ip ` + ibrlClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + ibrlClient.Pubkey + ` --client-ip ` + ibrlClient.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + mcastClient.Pubkey + ` --client-ip ` + mcastClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + mcastClient.Pubkey + ` --client-ip ` + mcastClient.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Set access passes for both clients
	log.Debug("==> Setting access passes for both clients")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlClient.CYOANetworkIP + " --user-payer " + ibrlClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + mcastClient.CYOANetworkIP + " --user-payer " + mcastClient.Pubkey})
	require.NoError(t, err)

	// Wait for latency results for all clients
	log.Debug("==> Waiting for latency results")
	err = ibrlClient.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	err = mcastClient.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	log.Debug("--> Latency results received for all clients")

	log.Debug("--> Coexistence test devnet setup complete")

	return dn, device, ibrlClient, mcastClient
}

// runIBRLWithMulticastSubscriberTest tests IBRL and multicast subscriber coexistence.
func runIBRLWithMulticastSubscriberTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	ibrlClient, mcastClient *devnet.Client, useAllocatedAddr bool,
) {
	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode, "multicast_type", "subscriber")

	// === CONNECT PHASE ===
	log.Debug("==> CONNECT PHASE")

	// Set access passes for all clients
	log.Debug("==> Setting access passes")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlClient.CYOANetworkIP + " --user-payer " + ibrlClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + mcastClient.CYOANetworkIP + " --user-payer " + mcastClient.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client
	log.Debug("==> Connecting IBRL client", "useAllocatedAddr", useAllocatedAddr)
	ibrlCmd := "doublezero connect ibrl --client-ip " + ibrlClient.CYOANetworkIP
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = ibrlClient.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Connect multicast subscriber
	log.Debug("==> Connecting multicast subscriber")
	_, err = mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber mg01 --client-ip " + mcastClient.CYOANetworkIP})
	require.NoError(t, err)

	// Wait for tunnels to come up
	log.Debug("==> Waiting for tunnels to come up")
	err = ibrlClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	err = mcastClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "Multicast subscriber tunnel failed to come up")
	log.Debug("--> All tunnels are up")

	// Verify CLI status shows correct device/metro for both clients
	log.Debug("==> Verifying CLI status shows correct device/metro for both clients")
	ibrlCLIStatus, err := ibrlClient.GetCLIStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, ibrlCLIStatus, 1, "expected one tunnel for IBRL client")
	require.NotEqual(t, "N/A", ibrlCLIStatus[0].CurrentDevice,
		"IBRL tunnel should have a current_device, got N/A")
	require.NotEqual(t, "N/A", ibrlCLIStatus[0].Metro,
		"IBRL tunnel should have a metro, got N/A")

	mcastCLIStatus, err := mcastClient.GetCLIStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, mcastCLIStatus, 1, "expected one tunnel for multicast client")
	// Multicast subscribers can be matched to a device via tunnel_dst (device public IP)
	require.NotEqual(t, "N/A", mcastCLIStatus[0].CurrentDevice,
		"Multicast subscriber tunnel should have a current_device, got N/A")
	require.NotEqual(t, "N/A", mcastCLIStatus[0].Metro,
		"Multicast subscriber tunnel should have a metro, got N/A")
	log.Debug("--> CLI status verified: all tunnels have device/metro info")

	// === COEXISTENCE VERIFICATION ===
	log.Debug("==> COEXISTENCE VERIFICATION PHASE")

	log.Debug("==> Waiting for agent config to include multicast subscriber")
	waitForAgentConfigWithClient(t, log, dn, device, mcastClient)

	// Wait for agent config to be applied and verifications to pass
	log.Debug("==> Waiting for agent config to be applied to device")
	require.Eventually(t, func() bool {
		return checkIBRLClientReady(t, log, device, ibrlClient, useAllocatedAddr) &&
			checkMulticastSubscriberPIMAdjacency(t, log, device)
	}, 60*time.Second, 2*time.Second, "agent config not applied within timeout")

	log.Debug("--> Agent config applied, running final verification")

	verifyIBRLClient(t, log, device, ibrlClient, useAllocatedAddr)
	verifyMulticastSubscriberPIMAdjacency(t, log, device)

	log.Debug("--> Both services verified as working simultaneously")

	// Disconnect multicast subscriber - don't fail if ledger is unavailable
	log.Debug("==> Disconnecting multicast subscriber to test independence")
	_, disconnectMcastErr := mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + mcastClient.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Debug("--> Warning: Multicast disconnect failed (ledger may be unavailable)", "error", disconnectMcastErr)
		return
	}

	// Verify IBRL client still works
	log.Debug("==> Verifying IBRL client still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Debug("--> IBRL client still working")

	// === FULL DISCONNECT PHASE ===
	log.Debug("==> FULL DISCONNECT PHASE")

	// Disconnect IBRL client - don't fail test if ledger is unavailable (infrastructure flakiness)
	_, disconnectErr := ibrlClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + ibrlClient.CYOANetworkIP})
	if disconnectErr != nil {
		log.Debug("--> Warning: IBRL disconnect failed (ledger may be unavailable)", "error", disconnectErr)
	} else {
		// Only verify tunnel removal if disconnect succeeded
		log.Debug("==> Verifying tunnels removed")
		verifyTunnelRemoved(t, ibrlClient, "doublezero0")
		verifyTunnelRemoved(t, mcastClient, "doublezero1")
	}

	log.Debug("--> Test completed successfully")
}

// runIBRLWithMulticastPublisherTest tests IBRL and multicast publisher coexistence.
func runIBRLWithMulticastPublisherTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device,
	ibrlClient, mcastClient *devnet.Client, useAllocatedAddr bool,
) {
	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode, "multicast_type", "publisher")

	// === CONNECT PHASE ===
	log.Debug("==> CONNECT PHASE")

	// Set access passes for all clients
	log.Debug("==> Setting access passes")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlClient.CYOANetworkIP + " --user-payer " + ibrlClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + mcastClient.CYOANetworkIP + " --user-payer " + mcastClient.Pubkey})
	require.NoError(t, err)

	// Connect IBRL client
	log.Debug("==> Connecting IBRL client", "useAllocatedAddr", useAllocatedAddr)
	ibrlCmd := "doublezero connect ibrl --client-ip " + ibrlClient.CYOANetworkIP
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = ibrlClient.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Connect multicast publisher
	log.Debug("==> Connecting multicast publisher")
	_, err = mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher mg01 --client-ip " + mcastClient.CYOANetworkIP})
	require.NoError(t, err)

	// Wait for tunnels to come up
	log.Debug("==> Waiting for tunnels to come up")
	err = ibrlClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	err = mcastClient.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "Multicast publisher tunnel failed to come up")
	log.Debug("--> All tunnels are up")

	// Verify CLI status shows correct device/metro for both clients
	log.Debug("==> Verifying CLI status shows correct device/metro for both clients")
	ibrlCLIStatus, err := ibrlClient.GetCLIStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, ibrlCLIStatus, 1, "expected one tunnel for IBRL client")
	require.NotEqual(t, "N/A", ibrlCLIStatus[0].CurrentDevice,
		"IBRL tunnel should have a current_device, got N/A")
	require.NotEqual(t, "N/A", ibrlCLIStatus[0].Metro,
		"IBRL tunnel should have a metro, got N/A")

	mcastCLIStatus, err := mcastClient.GetCLIStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, mcastCLIStatus, 1, "expected one tunnel for multicast client")
	// Multicast publishers can be matched to a device via tunnel_dst (device public IP)
	require.NotEqual(t, "N/A", mcastCLIStatus[0].CurrentDevice,
		"Multicast publisher tunnel should have a current_device, got N/A")
	require.NotEqual(t, "N/A", mcastCLIStatus[0].Metro,
		"Multicast publisher tunnel should have a metro, got N/A")
	log.Debug("--> CLI status verified: all tunnels have device/metro info")

	// === COEXISTENCE VERIFICATION ===
	log.Debug("==> COEXISTENCE VERIFICATION PHASE")

	// Wait for agent config to be pushed to device (required for mroutes to work)
	log.Debug("==> Waiting for agent config to include multicast publisher")
	waitForAgentConfigWithClient(t, log, dn, device, mcastClient)

	verifyIBRLClient(t, log, device, ibrlClient, useAllocatedAddr)
	verifyMulticastPublisherMrouteState(t, log, device, mcastClient)

	log.Debug("--> Both services verified as working simultaneously")

	// Disconnect multicast publisher - don't fail if ledger is unavailable
	log.Debug("==> Disconnecting multicast publisher to test independence")
	_, disconnectMcastErr := mcastClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + mcastClient.CYOANetworkIP})
	if disconnectMcastErr != nil {
		log.Debug("--> Warning: Multicast disconnect failed (ledger may be unavailable)", "error", disconnectMcastErr)
		return
	}

	// Verify IBRL client still works
	log.Debug("==> Verifying IBRL client still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Debug("--> IBRL client still working")

	// === FULL DISCONNECT PHASE ===
	log.Debug("==> FULL DISCONNECT PHASE")

	// Disconnect IBRL client - don't fail test if ledger is unavailable (infrastructure flakiness)
	_, disconnectErr := ibrlClient.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + ibrlClient.CYOANetworkIP})
	if disconnectErr != nil {
		log.Debug("--> Warning: IBRL disconnect failed (ledger may be unavailable)", "error", disconnectErr)
		log.Debug("--> Skipping tunnel removal verification due to disconnect failure")
	} else {
		// Only verify tunnel removal if disconnect succeeded
		log.Debug("==> Verifying tunnels removed")
		verifyTunnelRemoved(t, ibrlClient, "doublezero0")
		verifyTunnelRemoved(t, mcastClient, "doublezero1")
	}

	log.Debug("--> Test completed successfully")
}

// verifyIBRLClient verifies the IBRL client is working correctly.
func verifyIBRLClient(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client, allocatedAddr bool) {
	log.Debug("==> Verifying IBRL client")

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
		log.Debug("--> Verified allocated IP differs from public IP", "publicIP", client.CYOANetworkIP, "dzIP", dzIP)
	}

	log.Debug("--> IBRL client verified")
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
			log.Debug("--> BGP session Established", "peer", expectedLinkLocalAddr)
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("BGP session not established within timeout")
}

// checkIBRLClientReady checks if the IBRL client is ready (non-fatal version for polling).
func checkIBRLClientReady(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client, allocatedAddr bool) bool {
	// Check doublezero0 interface exists
	links, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"})
	if err != nil || len(links) != 1 {
		return false
	}

	// Check BGP session is Established
	neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd("vrf1"))
	if err != nil {
		return false
	}

	peer, ok := neighbors.VRFs["vrf1"].Peers[expectedLinkLocalAddr]
	if !ok || peer.PeerState != "Established" {
		return false
	}

	return true
}

// checkMulticastSubscriberPIMAdjacency checks if PIM adjacency is formed (non-fatal version for polling).
func checkMulticastSubscriberPIMAdjacency(t *testing.T, log *slog.Logger, device *devnet.Device) bool {
	pim, err := devnet.DeviceExecAristaCliJSON[*arista.ShowPIMNeighbors](t.Context(), device, arista.ShowPIMNeighborsCmd())
	if err != nil {
		return false
	}

	for _, vrf := range pim.VRFs {
		for intfName, intf := range vrf.Interfaces {
			for addr := range intf.Neighbors {
				if strings.HasPrefix(addr, "169.254.0.") {
					if len(intfName) >= 6 && intfName[:6] == "Tunnel" {
						return true
					}
				}
			}
		}
	}
	return false
}

// verifyMulticastSubscriberPIMAdjacency verifies PIM adjacency is formed on the device for subscriber.
// This function looks for any PIM neighbor on a Tunnel interface in the 169.254.0.x range.
// In coexistence tests with multiple clients, the multicast client may not get 169.254.0.1
// (e.g., if IBRL client connected first and got 169.254.0.1, multicast client gets 169.254.0.3).
func verifyMulticastSubscriberPIMAdjacency(t *testing.T, log *slog.Logger, device *devnet.Device) {
	log.Debug("==> Verifying multicast subscriber PIM adjacency")

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pim, err := devnet.DeviceExecAristaCliJSON[*arista.ShowPIMNeighbors](t.Context(), device, arista.ShowPIMNeighborsCmd())
		require.NoError(t, err, "error fetching pim neighbors from doublezero device")

		// Look for any PIM neighbor in the 169.254.0.x range on a Tunnel interface
		// The JSON structure is: vrfs -> vrf_name -> interfaces -> interface_name -> neighbors -> address -> details
		for _, vrf := range pim.VRFs {
			for intfName, intf := range vrf.Interfaces {
				for addr, neighbor := range intf.Neighbors {
					if strings.HasPrefix(addr, "169.254.0.") {
						if len(intfName) >= 6 && intfName[:6] == "Tunnel" {
							log.Info("--> PIM adjacency verified", "interface", intfName, "address", addr, "neighbor", neighbor)
							return
						}
					}
				}
			}
		}

		log.Debug("PIM neighbor not found yet in 169.254.0.x range", "vrfs", pim.VRFs)
		time.Sleep(1 * time.Second)
	}

	t.Fatalf("PIM neighbor not established on Tunnel interface within timeout")
}

// verifyConcurrentMulticastPIMAdjacency verifies PIM adjacency for concurrent IBRL+multicast tunnels.
// For single-client scenarios where the same user has both IBRL and multicast, both tunnels
// use sequential addresses in the 169.254.0.x range (e.g., IBRL on .0/.1, multicast on .2/.3).
func verifyConcurrentMulticastPIMAdjacency(t *testing.T, log *slog.Logger, device *devnet.Device) {
	tunnelOut, _ := devnet.DeviceExecAristaCliJSON[any](t.Context(), device, "show interfaces Tunnel1-100")
	log.Debug("Device tunnel interfaces", "output", tunnelOut)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pim, err := devnet.DeviceExecAristaCliJSON[*arista.ShowPIMNeighbors](t.Context(), device, arista.ShowPIMNeighborsCmd())
		require.NoError(t, err, "error fetching pim neighbors from doublezero device")

		// Look for any PIM neighbor in the 169.254.x.x range on a Tunnel interface
		// The JSON structure is: vrfs -> vrf_name -> interfaces -> interface_name -> neighbors -> address -> details
		for _, vrf := range pim.VRFs {
			for intfName, intf := range vrf.Interfaces {
				for addr := range intf.Neighbors {
					if strings.HasPrefix(addr, "169.254.") {
						if len(intfName) >= 6 && intfName[:6] == "Tunnel" {
							log.Info("--> Concurrent mcast PIM adjacency verified", "interface", intfName, "address", addr)
							return
						}
					}
				}
			}
		}

		log.Debug("Concurrent mcast PIM neighbor not found yet, checking all VRFs", "vrfs", pim.VRFs)
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Concurrent multicast PIM neighbor not established on Tunnel interface within timeout")
}

// verifyConcurrentMulticastPublisherMrouteState verifies mroute state for concurrent IBRL+multicast publisher.
func verifyConcurrentMulticastPublisherMrouteState(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client) {
	log.Info("==> Verifying concurrent multicast publisher mroute state")

	// Get the actual allocated IP from the client's tunnel status
	// This is more reliable than calculating it, especially when the client
	// has both IBRL and multicast tunnels with allocated IPs
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tunnelStatus, "client should have at least one tunnel")

	// Find the multicast tunnel's DoubleZeroIP
	var expectedAllocatedIP string
	for _, ts := range tunnelStatus {
		if ts.UserType == devnet.ClientUserTypeMulticast {
			expectedAllocatedIP = ts.DoubleZeroIP.String()
			break
		}
	}
	require.NotEmpty(t, expectedAllocatedIP, "could not find multicast tunnel's DoubleZeroIP")
	log.Info("==> Using client's actual allocated IP", "expectedAllocatedIP", expectedAllocatedIP)

	// Diagnostic: Check tunnel and route state
	log.Info("==> Diagnostic: Checking tunnel interfaces")
	tunnelOut, _ := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show type gre 2>/dev/null || ip link show type gre"}, docker.NoPrintOnError())
	log.Info("Tunnel interfaces", "output", string(tunnelOut))

	log.Info("==> Diagnostic: Checking routes to multicast group")
	routeOut, _ := client.Exec(t.Context(), []string{"bash", "-c", "ip route get 233.84.178.0 2>&1"}, docker.NoPrintOnError())
	log.Info("Route to multicast group", "output", string(routeOut))

	log.Info("==> Diagnostic: Checking addresses on doublezero1")
	addrOut, _ := client.Exec(t.Context(), []string{"bash", "-c", "ip addr show dev doublezero1 2>&1"}, docker.NoPrintOnError())
	log.Info("Addresses on doublezero1", "output", string(addrOut))

	// Verify mroute state on device - poll for 60 seconds (longer for concurrent case).
	// The heartbeat sender creates (S,G) state by sending periodic UDP heartbeat
	// packets to the multicast group, so no explicit ping is needed.
	mGroup := "233.84.178.0"
	deadline := time.Now().Add(60 * time.Second)
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
			log.Info("--> Concurrent mcast mroute state verified", "group", mGroup, "source", expectedAllocatedIP)
			return
		}

		log.Debug("Source not found in multicast group", "expectedIP", expectedAllocatedIP, "sources", groups.GroupSources)
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("Concurrent mcast mroute state not created within timeout for group %s with source %s", mGroup, expectedAllocatedIP)
}

// verifyMulticastPublisherMrouteState verifies mroute state is created on the device for publisher.
func verifyMulticastPublisherMrouteState(t *testing.T, log *slog.Logger, device *devnet.Device, client *devnet.Client) {
	log.Debug("==> Verifying multicast publisher mroute state")

	// Get the actual allocated IP from the client's tunnel status
	// This is more reliable than calculating it, especially when multiple clients
	// have allocated IPs from the same prefix
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, tunnelStatus, "client should have at least one tunnel")

	// Find the multicast tunnel's DoubleZeroIP
	var expectedAllocatedIP string
	for _, ts := range tunnelStatus {
		if ts.UserType == devnet.ClientUserTypeMulticast {
			expectedAllocatedIP = ts.DoubleZeroIP.String()
			break
		}
	}
	require.NotEmpty(t, expectedAllocatedIP, "could not find multicast tunnel's DoubleZeroIP")
	log.Info("==> Using client's actual allocated IP", "expectedAllocatedIP", expectedAllocatedIP)

	// Verify mroute state on device - poll for 30 seconds.
	// The heartbeat sender creates (S,G) state by sending periodic UDP heartbeat
	// packets to the multicast group, so no explicit ping is needed.
	mGroup := "233.84.178.0"
	deadline := time.Now().Add(60 * time.Second)
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
			log.Debug("--> Mroute state verified", "group", mGroup, "source", expectedAllocatedIP)
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
	var lastConfigSnippet string
	require.Eventually(t, func() bool {
		config, err := dn.Controller.GetAgentConfig(t.Context(), device.ID)
		if err != nil {
			log.Debug("Error getting agent config", "error", err, "deviceID", device.ID, "deviceCode", device.Spec.Code)
			return false
		}

		// Check if the config contains the client IP (indicating the tunnel is configured)
		if strings.Contains(config.Config, client.CYOANetworkIP) {
			log.Info("--> Agent config includes client", "clientIP", client.CYOANetworkIP, "deviceCode", device.Spec.Code)
			return true
		}

		// Log a snippet of the config to help debug what's being returned
		configLen := len(config.Config)
		snippet := config.Config
		if configLen > 500 {
			snippet = config.Config[:500] + "..."
		}
		if snippet != lastConfigSnippet {
			log.Info("Agent config does not yet include client",
				"clientIP", client.CYOANetworkIP,
				"deviceCode", device.Spec.Code,
				"deviceID", device.ID,
				"configLen", configLen,
				"configSnippet", snippet)
			lastConfigSnippet = snippet
		} else {
			log.Debug("Agent config does not yet include client", "clientIP", client.CYOANetworkIP)
		}
		return false
	}, 60*time.Second, 1*time.Second, "agent config should include client %s for device %s (%s)", client.CYOANetworkIP, device.Spec.Code, device.ID)
}
