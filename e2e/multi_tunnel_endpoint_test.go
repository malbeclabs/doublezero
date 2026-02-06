//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_MultiTunnelEndpoint_SameDevice tests that a single client can have
// multiple tunnels (IBRL + Multicast) to the SAME device when that device has
// multiple UserTunnelEndpoint interfaces configured.
//
// This is a key feature that allows multiple GRE tunnels from the same client IP
// to the same device by using different tunnel endpoint IPs on the device side.
func TestE2E_MultiTunnelEndpoint_SameDevice(t *testing.T) {
	t.Parallel()

	dn, device, client := setupMultiTunnelEndpointDevnet(t)
	log := logger.With("test", t.Name())

	t.Run("ibrl_and_multicast_same_device", func(t *testing.T) {
		runMultiTunnelSameDeviceTest(t, log, dn, device, client, false)
	})
}

// TestE2E_MultiTunnelEndpoint_SameDevice_AllocatedAddr tests the same scenario
// but with IBRL using allocated address mode.
func TestE2E_MultiTunnelEndpoint_SameDevice_AllocatedAddr(t *testing.T) {
	t.Parallel()

	dn, device, client := setupMultiTunnelEndpointDevnet(t)
	log := logger.With("test", t.Name())

	t.Run("ibrl_allocated_and_multicast_same_device", func(t *testing.T) {
		runMultiTunnelSameDeviceTest(t, log, dn, device, client, true)
	})
}

// setupMultiTunnelEndpointDevnet sets up a devnet with a single device that has
// multiple UserTunnelEndpoint interfaces, allowing multiple tunnels from the same client.
func setupMultiTunnelEndpointDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Setting up multi-tunnel endpoint test devnet")

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

	// Add the device
	log.Info("==> Adding device ny5-dz01")
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "ny5-dz01",
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Info("--> Device added", "deviceID", device.ID, "deviceIP", device.CYOANetworkIP)

	// Create device interfaces including multiple UserTunnelEndpoint interfaces
	// This is the key setup - we need at least 2 tunnel endpoints for multiple tunnels
	log.Info("==> Creating device interfaces with multiple UserTunnelEndpoint interfaces")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create standard device interfaces"
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w

		echo "==> Create additional UserTunnelEndpoint interface (Loopback100)"
		# This creates a second tunnel endpoint on the device
		# The first endpoint is the device's public_ip, this is the second
		doublezero device interface create ny5-dz01 "Loopback100" \
			--ip-net "203.0.113.10/32" \
			--user-tunnel-endpoint true \
			-w

		echo "--> Device interfaces created:"
		doublezero device interface list ny5-dz01
	`})
	require.NoError(t, err)

	// Add client
	log.Info("==> Adding client")
	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Info("--> Client added", "clientIP", client.CYOANetworkIP, "pubkey", client.Pubkey)

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

	// Add client to multicast allowlists
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Wait for latency results
	log.Info("==> Waiting for latency results")
	err = client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	log.Info("--> Latency results received")

	log.Info("--> Multi-tunnel endpoint test devnet setup complete")

	return dn, device, client
}

// runMultiTunnelSameDeviceTest tests that a single client can establish multiple tunnels
// to the same device using different tunnel endpoints.
func runMultiTunnelSameDeviceTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet,
	device *devnet.Device, client *devnet.Client, useAllocatedAddr bool,
) {
	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode, "device", device.Spec.Code)

	// === PHASE 1: Connect IBRL to the specific device ===
	log.Info("==> PHASE 1: Connecting IBRL to device", "device", device.Spec.Code)

	// Set access pass for the client
	log.Info("==> Setting access pass")
	_, err := dn.Manager.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
	})
	require.NoError(t, err)

	// Connect IBRL client to the specific device using --device flag
	log.Info("==> Connecting client with IBRL to specific device", "device", device.Spec.Code)
	ibrlCmd := fmt.Sprintf("doublezero connect ibrl --client-ip %s --device %s",
		client.CYOANetworkIP, device.Spec.Code)
	if useAllocatedAddr {
		ibrlCmd += " --allocate-addr"
	}
	_, err = client.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Wait for IBRL tunnel to come up
	log.Info("==> Waiting for IBRL tunnel to come up")
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	log.Info("--> IBRL tunnel is up")

	// Verify IBRL tunnel destination
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel after IBRL connect")
	ibrlTunnelEndpoint := tunnelStatus[0].TunnelDst.String()
	log.Info("==> IBRL tunnel established", "tunnelDst", ibrlTunnelEndpoint)

	// Verify BGP is established on the device
	log.Info("==> Verifying IBRL BGP session on device")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL BGP session verified")

	// === PHASE 2: Connect Multicast to the SAME device ===
	log.Info("==> PHASE 2: Connecting Multicast subscriber to SAME device", "device", device.Spec.Code)

	// Connect multicast to the same device using --device flag
	mcastCmd := fmt.Sprintf("doublezero connect multicast subscriber mg01 --client-ip %s --device %s 2>&1",
		client.CYOANetworkIP, device.Spec.Code)
	mcastOutput, err := client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	// Wait for agent config to be pushed to device
	log.Info("==> Waiting for agent config to be pushed to device")
	waitForAgentConfigWithClient(t, log, dn, device, client)
	log.Info("--> Agent config pushed")

	// Wait for BOTH tunnels to be up
	log.Info("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels should be up on the same device")
	log.Info("--> Both tunnels are up")

	// Get tunnel status and verify they're using different endpoints
	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 2, "expected exactly two tunnels")

	tunnel1Dst := tunnelStatus[0].TunnelDst.String()
	tunnel2Dst := tunnelStatus[1].TunnelDst.String()
	log.Info("==> Tunnel endpoints",
		"tunnel1_dst", tunnel1Dst,
		"tunnel2_dst", tunnel2Dst)

	// CRITICAL VERIFICATION: The two tunnels should use DIFFERENT endpoints on the same device
	require.NotEqual(t, tunnel1Dst, tunnel2Dst,
		"Two tunnels to the same device should use different tunnel endpoints")
	log.Info("--> Verified: tunnels use different endpoints on the same device")

	// Verify both tunnels are to the same device (both endpoints should belong to our device)
	// One should be the device's CYOA IP, another should be the UserTunnelEndpoint (203.0.113.10)
	deviceEndpoints := []string{device.CYOANetworkIP, "203.0.113.10"}
	require.Contains(t, deviceEndpoints, tunnel1Dst,
		"First tunnel should terminate at one of the device's endpoints")
	require.Contains(t, deviceEndpoints, tunnel2Dst,
		"Second tunnel should terminate at one of the device's endpoints")
	log.Info("--> Verified: both tunnels terminate at the same device")

	// Verify both BGP sessions are established on the device
	log.Info("==> Verifying both BGP sessions on device")
	verifyMultipleBGPSessionsOnDevice(t, log, device, 2)
	log.Info("--> Both BGP sessions verified on device")

	// === PHASE 3: Verify both tunnels work independently ===
	log.Info("==> PHASE 3: Verifying both tunnels work")

	// Verify IBRL still works
	log.Info("==> Verifying IBRL tunnel still works")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL tunnel working")

	// Verify multicast PIM adjacency
	log.Info("==> Verifying multicast PIM adjacency")
	verifyMulticastSubscriberPIMAdjacency(t, log, device)
	log.Info("--> Multicast PIM adjacency verified")

	// === PHASE 4: Disconnect and verify ===
	log.Info("==> PHASE 4: Disconnecting")

	// Disconnect multicast first
	log.Info("==> Disconnecting multicast")
	_, err = client.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero disconnect multicast subscriber mg01 --client-ip " + client.CYOANetworkIP,
	})
	if err != nil {
		log.Info("--> Warning: multicast disconnect returned error", "error", err)
	}

	// Wait a moment for disconnect to propagate
	time.Sleep(5 * time.Second)

	// Verify IBRL still works after multicast disconnect
	log.Info("==> Verifying IBRL still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL still working after multicast disconnect")

	// Disconnect IBRL
	log.Info("==> Disconnecting IBRL")
	_, err = client.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero disconnect --client-ip " + client.CYOANetworkIP,
	})
	if err != nil {
		log.Info("--> Warning: IBRL disconnect returned error", "error", err)
	}

	log.Info("--> Multi-tunnel same-device test completed successfully")
}

// verifyMultipleBGPSessionsOnDevice checks that the device has the expected number
// of BGP sessions established in vrf1.
func verifyMultipleBGPSessionsOnDevice(t *testing.T, log *slog.Logger, device *devnet.Device, expectedCount int) {
	log.Info("==> Checking BGP sessions on device", "device", device.Spec.Code, "expected", expectedCount)

	ctx := t.Context()
	var lastOutput string
	var establishedCount int

	// Retry a few times as BGP sessions may take time to establish
	for attempt := 0; attempt < 10; attempt++ {
		output, err := arista.ExecCli(ctx, device.ID, "show ip bgp summary vrf vrf1")
		if err != nil {
			log.Info("==> BGP summary query failed, retrying", "attempt", attempt, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		lastOutput = output

		// Count established sessions (lines with "Estab" state)
		lines := strings.Split(output, "\n")
		establishedCount = 0
		for _, line := range lines {
			if strings.Contains(line, "Estab") {
				establishedCount++
			}
		}

		if establishedCount >= expectedCount {
			log.Info("--> Found expected BGP sessions",
				"established", establishedCount, "expected", expectedCount)
			return
		}

		log.Info("==> Waiting for BGP sessions",
			"attempt", attempt,
			"established", establishedCount,
			"expected", expectedCount)
		time.Sleep(5 * time.Second)
	}

	t.Fatalf("Expected %d BGP sessions but found %d. Last output:\n%s",
		expectedCount, establishedCount, lastOutput)
}
