//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_MultiTunnel_FallbackToSecondDevice tests that when a client already has
// a tunnel to a device, creating a second tunnel (different type) will use a different
// device since the first device's tunnel endpoint is already in use.
//
// This verifies the client CLI's device selection logic correctly excludes devices
// where all tunnel endpoints are exhausted and falls back to the next best device.
func TestE2E_MultiTunnel_FallbackToSecondDevice(t *testing.T) {
	t.Parallel()

	dn, device1, device2, client := setupMultiTunnelDevnet(t)
	log := logger.With("test", t.Name())

	t.Run("ibrl_then_multicast_different_devices", func(t *testing.T) {
		runMultiTunnelFallbackTest(t, log, dn, device1, device2, client, false)
	})
}

// TestE2E_MultiTunnel_FallbackToSecondDevice_AllocatedAddr tests the same fallback
// behavior but with IBRL using allocated address mode.
func TestE2E_MultiTunnel_FallbackToSecondDevice_AllocatedAddr(t *testing.T) {
	t.Parallel()

	dn, device1, device2, client := setupMultiTunnelDevnet(t)
	log := logger.With("test", t.Name())

	t.Run("ibrl_allocated_then_multicast_different_devices", func(t *testing.T) {
		runMultiTunnelFallbackTest(t, log, dn, device1, device2, client, true)
	})
}

// setupMultiTunnelDevnet sets up a devnet with two devices and a single client.
// This allows testing the fallback behavior when the first device's endpoint is used.
func setupMultiTunnelDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Device, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Setting up multi-tunnel test devnet with two devices")

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

	// Add first device
	log.Info("==> Adding device ny5-dz01")
	device1, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "ny5-dz01",
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Info("--> Device 1 added", "deviceID", device1.ID, "deviceIP", device1.CYOANetworkIP)

	// Add second device for fallback
	log.Info("==> Adding device pit-dz01")
	device2, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "pit-dz01",
		Location:                     "pit",
		Exchange:                     "xpit",
		CYOANetworkIPHostID:          16,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	log.Info("--> Device 2 added", "deviceID", device2.ID, "deviceIP", device2.CYOANetworkIP)

	// Create device interfaces
	log.Info("==> Creating device interfaces")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create device interfaces for ny5-dz01"
		doublezero device interface create ny5-dz01 "Ethernet2" --bandwidth 10G -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 --bandwidth 10G -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 --bandwidth 10G -w

		echo "==> Create device interfaces for pit-dz01"
		doublezero device interface create pit-dz01 "Ethernet2" --bandwidth 10G -w
		doublezero device interface create pit-dz01 "Loopback255" --loopback-type vpnv4 --bandwidth 10G -w
		doublezero device interface create pit-dz01 "Loopback256" --loopback-type ipv4 --bandwidth 10G -w

		echo "--> Device interfaces created"
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

	// Wait for latency results from both devices
	log.Info("==> Waiting for latency results from both devices")
	err = client.WaitForLatencyResults(t.Context(), device1.ID, 75*time.Second)
	require.NoError(t, err)
	err = client.WaitForLatencyResults(t.Context(), device2.ID, 75*time.Second)
	require.NoError(t, err)
	log.Info("--> Latency results received from both devices")

	log.Info("--> Multi-tunnel test devnet setup complete")

	return dn, device1, device2, client
}

// runMultiTunnelFallbackTest tests that when creating a second tunnel type,
// the client CLI correctly falls back to a different device since the first
// device's tunnel endpoint is already in use.
func runMultiTunnelFallbackTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet,
	device1 *devnet.Device, device2 *devnet.Device, client *devnet.Client, useAllocatedAddr bool,
) {
	mode := "standard"
	if useAllocatedAddr {
		mode = "allocated_addr"
	}
	log = log.With("mode", mode)

	// === PHASE 1: Connect IBRL to device1 ===
	log.Info("==> PHASE 1: Connecting IBRL to device1", "device", device1.Spec.Code)

	// Set access pass for the client
	log.Info("==> Setting access pass")
	_, err := dn.Manager.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
	})
	require.NoError(t, err)

	// Connect IBRL client to device1 using --device flag
	log.Info("==> Connecting client with IBRL to device1", "device", device1.Spec.Code)
	ibrlCmd := fmt.Sprintf("doublezero connect ibrl --client-ip %s --device %s",
		client.CYOANetworkIP, device1.Spec.Code)
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

	// Verify IBRL tunnel destination is device1
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel after IBRL connect")
	ibrlTunnelDst := tunnelStatus[0].TunnelDst.String()
	require.Equal(t, device1.CYOANetworkIP, ibrlTunnelDst,
		"IBRL tunnel should be connected to device1")
	log.Info("==> IBRL tunnel established to device1", "tunnelDst", ibrlTunnelDst)

	// Verify BGP is established on device1
	log.Info("==> Verifying IBRL BGP session on device1")
	verifyIBRLClientBGPEstablished(t, log, device1)
	log.Info("--> IBRL BGP session verified on device1")

	// === PHASE 2: Connect Multicast (should go to device2 since device1's endpoint is used) ===
	log.Info("==> PHASE 2: Connecting Multicast (should fall back to device2)")

	// Connect multicast without specifying device - it should automatically pick device2
	// because device1's tunnel endpoint is already in use by the IBRL tunnel
	mcastCmd := fmt.Sprintf("doublezero connect multicast subscriber mg01 --client-ip %s 2>&1",
		client.CYOANetworkIP)
	mcastOutput, err := client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	// Wait for agent config to be pushed to device2
	log.Info("==> Waiting for agent config to be pushed to device2")
	waitForAgentConfigWithClient(t, log, dn, device2, client)
	log.Info("--> Agent config pushed to device2")

	// Wait for BOTH tunnels to be up
	log.Info("==> Waiting for both tunnels (IBRL and Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "Both tunnels should be up")
	log.Info("--> Both tunnels are up")

	// Verify tunnel destinations
	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 2, "expected exactly two tunnels")

	// Find which tunnel goes where
	var ibrlTunnel, mcastTunnel *devnet.ClientStatusResponse
	for i := range tunnelStatus {
		switch tunnelStatus[i].UserType {
		case devnet.ClientUserTypeIBRL, devnet.ClientUserTypeIBRLWithAllocated:
			ibrlTunnel = &tunnelStatus[i]
		case devnet.ClientUserTypeMulticast:
			mcastTunnel = &tunnelStatus[i]
		}
	}
	require.NotNil(t, ibrlTunnel, "should have IBRL tunnel")
	require.NotNil(t, mcastTunnel, "should have Multicast tunnel")

	log.Info("==> Tunnel destinations",
		"ibrl_dst", ibrlTunnel.TunnelDst.String(),
		"mcast_dst", mcastTunnel.TunnelDst.String())

	// CRITICAL VERIFICATION: The tunnels should be on DIFFERENT devices
	// because device1's endpoint is already in use by IBRL
	require.Equal(t, device1.CYOANetworkIP, ibrlTunnel.TunnelDst.String(),
		"IBRL tunnel should still be on device1")
	require.Equal(t, device2.CYOANetworkIP, mcastTunnel.TunnelDst.String(),
		"Multicast tunnel should fall back to device2")
	log.Info("--> Verified: tunnels are on different devices (fallback worked)")

	// === PHASE 3: Verify both tunnels work ===
	log.Info("==> PHASE 3: Verifying both tunnels work")

	// Verify IBRL BGP on device1
	log.Info("==> Verifying IBRL BGP on device1")
	verifyIBRLClientBGPEstablished(t, log, device1)
	log.Info("--> IBRL BGP verified on device1")

	// Verify multicast PIM adjacency on device2
	log.Info("==> Verifying multicast PIM adjacency on device2")
	verifyMulticastSubscriberPIMAdjacency(t, log, device2)
	log.Info("--> Multicast PIM adjacency verified on device2")

	// === PHASE 4: Disconnect and verify ===
	log.Info("==> PHASE 4: Disconnecting")

	// Disconnect multicast first
	log.Info("==> Disconnecting multicast")
	_, err = client.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero disconnect multicast --client-ip " + client.CYOANetworkIP,
	})
	if err != nil {
		log.Info("--> Warning: multicast disconnect returned error", "error", err)
	}

	// Wait a moment for disconnect to propagate
	time.Sleep(5 * time.Second)

	// Verify IBRL still works after multicast disconnect
	log.Info("==> Verifying IBRL still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device1)
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

	log.Info("--> Multi-tunnel fallback test completed successfully")
}

// TestE2E_MultiTunnel_SimultaneousToSingleDevice tests that a client can establish
// two simultaneous tunnels (IBRL + multicast) to the SAME device using different
// tunnel endpoints. The device has a user-tunnel-endpoint loopback interface with a
// separate IP. The client selects the best endpoint based on latency for the first
// tunnel, and the second tunnel gets the remaining endpoint (since the first is
// excluded). The two tunnels end up on different endpoint IPs on the same device.
func TestE2E_MultiTunnel_SimultaneousToSingleDevice(t *testing.T) {
	t.Parallel()

	dn, device, client := setupSimultaneousTunnelDevnet(t)
	log := logger.With("test", t.Name())

	t.Run("ibrl_then_multicast_same_device", func(t *testing.T) {
		runSimultaneousTunnelTest(t, log, dn, device, client)
	})
}

// setupSimultaneousTunnelDevnet sets up a devnet with a single device that has a
// user-tunnel-endpoint loopback, allowing two simultaneous tunnels from one client.
func setupSimultaneousTunnelDevnet(t *testing.T) (*devnet.Devnet, *devnet.Device, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Setting up simultaneous tunnel test devnet with one device + UTE loopback")

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

	// Add device with a UTE loopback interface.
	// Device CYOA IP: hostID=8, UTE loopback IP: hostID=9 (offset=1).
	log.Info("==> Adding device ewr-dz01 with UTE loopback")
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "ewr-dz01",
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
		UserTunnelEndpoints: []devnet.UserTunnelEndpointSpec{
			{InterfaceName: "Loopback100", IPHostIDOffset: 1},
		},
	})
	require.NoError(t, err)
	log.Info("--> Device added",
		"deviceID", device.ID,
		"deviceIP", device.CYOANetworkIP,
		"uteIPs", device.UserTunnelEndpointIPs)

	// Create standard device interfaces.
	log.Info("==> Creating device interfaces")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create device interfaces for ewr-dz01"
		doublezero device interface create ewr-dz01 "Ethernet2" --bandwidth 10G -w
		doublezero device interface create ewr-dz01 "Loopback255" --loopback-type vpnv4 --bandwidth 10G -w
		doublezero device interface create ewr-dz01 "Loopback256" --loopback-type ipv4 --bandwidth 10G -w

		echo "--> Device interfaces created"
	`})
	require.NoError(t, err)

	// Add client with UTE probing enabled so the latency system also probes
	// the UTE loopback IP. This lets us verify reachability before connecting.
	log.Info("==> Adding client")
	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID:         100,
		LatencyProbeTunnelEndpoints: true,
	})
	require.NoError(t, err)
	log.Info("--> Client added", "clientIP", client.CYOANetworkIP, "pubkey", client.Pubkey)

	// Create multicast group and add client to allowlists.
	log.Info("==> Creating multicast group onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Create multicast group"
		doublezero multicast group create --code mg01 --max-bandwidth 10Gbps --owner me -w

		echo "--> Multicast group created:"
		doublezero multicast group list
	`})
	require.NoError(t, err)

	// Add client to multicast allowlists.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code mg01 --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	// Add a static route on the client to reach the UTE loopback IP via the
	// device's CYOA IP. The UTE IP is in a different /24 (third octet + 1) to avoid
	// EOS IP overlap with the CYOA subnet on Ethernet1.
	uteIP := device.UserTunnelEndpointIPs["Loopback100"]
	log.Info("==> Adding static route on client for UTE IP", "uteIP", uteIP, "via", device.CYOANetworkIP)
	_, err = client.Exec(t.Context(), []string{
		"ip", "route", "add", uteIP + "/32", "via", device.CYOANetworkIP,
	})
	require.NoError(t, err)

	// Wait for latency results from the device's public IP.
	log.Info("==> Waiting for latency results from device")
	err = client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)
	log.Info("--> Latency results received")

	// Wait for the UTE loopback IP to be reachable from the client. This ensures the
	// controller has pushed the Loopback100 config to the device before the test starts
	// creating GRE tunnels that target the UTE IP.
	log.Info("==> Waiting for UTE IP to be reachable from client", "uteIP", uteIP)
	err = client.WaitForPing(t.Context(), uteIP, 90*time.Second)
	require.NoError(t, err, "UTE IP should be reachable from client")
	log.Info("--> UTE IP is reachable")

	log.Info("--> Simultaneous tunnel test devnet setup complete")

	return dn, device, client
}

// runSimultaneousTunnelTest connects IBRL then multicast to the SAME device
// and verifies both tunnels coexist with different endpoint IPs.
func runSimultaneousTunnelTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet,
	device *devnet.Device, client *devnet.Client,
) {
	// Dump diagnostic info on test failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		log.Info("=== DIAGNOSTIC DUMP (test failed) ===")

		// Device running-config for loopbacks and tunnels.
		if out, err := device.Exec(ctx, []string{"Cli", "-p", "15", "-c", "show running-config section Loopback"}); err == nil {
			log.Info("Device Loopback config", "output", string(out))
		}
		if out, err := device.Exec(ctx, []string{"Cli", "-p", "15", "-c", "show running-config section Tunnel"}); err == nil {
			log.Info("Device Tunnel config", "output", string(out))
		}
		if out, err := device.Exec(ctx, []string{"Cli", "-c", "show interfaces Tunnel500"}); err == nil {
			log.Info("Device Tunnel500 status", "output", string(out))
		}
		if out, err := device.Exec(ctx, []string{"Cli", "-c", "show ip bgp summary"}); err == nil {
			log.Info("Device BGP summary", "output", string(out))
		}

		// Client tunnel status and routes.
		if out, err := client.Exec(ctx, []string{"doublezero", "status", "--output", "json"}); err == nil {
			log.Info("Client doublezero status", "output", string(out))
		}
		if out, err := client.Exec(ctx, []string{"ip", "route", "show"}); err == nil {
			log.Info("Client routes", "output", string(out))
		}
		if out, err := client.Exec(ctx, []string{"ip", "-d", "link", "show", "type", "gre"}); err == nil {
			log.Info("Client GRE tunnels", "output", string(out))
		}

		log.Info("=== END DIAGNOSTIC DUMP ===")
	})

	// === PHASE 1: Connect IBRL (first tunnel) ===
	log.Info("==> PHASE 1: Connecting IBRL to device", "device", device.Spec.Code)

	// Set access pass for the client.
	log.Info("==> Setting access pass")
	_, err := dn.Manager.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey,
	})
	require.NoError(t, err)

	// Connect IBRL. With only one device, the CLI auto-selects it.
	// The client picks the best tunnel endpoint based on latency probing.
	log.Info("==> Connecting client with IBRL")
	ibrlCmd := fmt.Sprintf("doublezero connect ibrl --client-ip %s", client.CYOANetworkIP)
	_, err = client.Exec(t.Context(), []string{"bash", "-c", ibrlCmd})
	require.NoError(t, err)

	// Wait for IBRL tunnel to come up.
	log.Info("==> Waiting for IBRL tunnel to come up")
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err, "IBRL tunnel failed to come up")
	log.Info("--> IBRL tunnel is up")

	// Verify IBRL tunnel destination is one of the device's endpoints (public IP or
	// UTE loopback). The client selects based on latency, so we don't assume which
	// endpoint is chosen first.
	tunnelStatus, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 1, "expected exactly one tunnel after IBRL connect")
	ibrlTunnelDst := tunnelStatus[0].TunnelDst.String()
	uteIP := device.UserTunnelEndpointIPs["Loopback100"]
	require.True(t, ibrlTunnelDst == uteIP || ibrlTunnelDst == device.CYOANetworkIP,
		"IBRL tunnel should use either the UTE loopback IP or device public IP, got %s", ibrlTunnelDst)
	log.Info("==> IBRL tunnel established", "tunnelDst", ibrlTunnelDst)

	// Verify BGP is established on device.
	log.Info("==> Verifying IBRL BGP session on device")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL BGP session verified")

	// === PHASE 2: Connect Multicast (second tunnel, same device) ===
	log.Info("==> PHASE 2: Connecting Multicast (same device, different endpoint)")

	// Connect multicast subscriber. The client's exclude_ips list contains the
	// first tunnel's endpoint, so it selects the remaining endpoint on this device.
	mcastCmd := fmt.Sprintf("doublezero connect multicast subscriber mg01 --client-ip %s 2>&1",
		client.CYOANetworkIP)
	mcastOutput, err := client.Exec(t.Context(), []string{"bash", "-c", mcastCmd})
	log.Info("==> Multicast connect output", "output", string(mcastOutput))
	require.NoError(t, err)

	// Wait for agent config to be pushed for the multicast tunnel.
	log.Info("==> Waiting for agent config to include client")
	waitForAgentConfigWithClient(t, log, dn, device, client)
	log.Info("--> Agent config pushed")

	// Wait for BOTH tunnels to be up.
	log.Info("==> Waiting for both tunnels (IBRL + Multicast) to be up")
	err = client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second)
	require.NoError(t, err, "both tunnels should be up")
	log.Info("--> Both tunnels are up")

	// Verify tunnel destinations.
	tunnelStatus, err = client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnelStatus, 2, "expected exactly two tunnels")

	// Find which tunnel goes where.
	var ibrlTunnel, mcastTunnel *devnet.ClientStatusResponse
	for i := range tunnelStatus {
		switch tunnelStatus[i].UserType {
		case devnet.ClientUserTypeIBRL, devnet.ClientUserTypeIBRLWithAllocated:
			ibrlTunnel = &tunnelStatus[i]
		case devnet.ClientUserTypeMulticast:
			mcastTunnel = &tunnelStatus[i]
		}
	}
	require.NotNil(t, ibrlTunnel, "should have IBRL tunnel")
	require.NotNil(t, mcastTunnel, "should have Multicast tunnel")

	log.Info("==> Tunnel destinations",
		"ibrl_dst", ibrlTunnel.TunnelDst.String(),
		"mcast_dst", mcastTunnel.TunnelDst.String())

	// CRITICAL VERIFICATION: Both tunnels on the SAME device but DIFFERENT endpoint IPs.
	// The client selects endpoints based on latency, so we don't assume which tunnel
	// gets which endpoint. We just verify they use different IPs and both are valid
	// device endpoints (one is the UTE loopback, the other is the device's public IP).
	endpoints := []string{ibrlTunnel.TunnelDst.String(), mcastTunnel.TunnelDst.String()}
	require.Contains(t, endpoints, uteIP,
		"one tunnel should use the UTE loopback IP")
	require.Contains(t, endpoints, device.CYOANetworkIP,
		"one tunnel should use the device public IP")
	require.NotEqual(t, ibrlTunnel.TunnelDst.String(), mcastTunnel.TunnelDst.String(),
		"the two tunnels must use different endpoint IPs")
	log.Info("--> Verified: both tunnels on same device with different endpoint IPs")

	// === PHASE 3: Verify both tunnels work ===
	log.Info("==> PHASE 3: Verifying both tunnels work")

	// Verify IBRL BGP on device.
	log.Info("==> Verifying IBRL BGP")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL BGP verified")

	// Verify multicast PIM adjacency on device.
	// Use the concurrent variant since both tunnels are on the same device.
	log.Info("==> Verifying multicast PIM adjacency")
	verifyConcurrentMulticastPIMAdjacency(t, log, device)
	log.Info("--> Multicast PIM adjacency verified")

	// === PHASE 4: Disconnect and verify ===
	log.Info("==> PHASE 4: Disconnecting")

	// Disconnect multicast first.
	log.Info("==> Disconnecting multicast")
	_, err = client.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero disconnect multicast --client-ip " + client.CYOANetworkIP,
	})
	if err != nil {
		log.Info("--> Warning: multicast disconnect returned error", "error", err)
	}

	// Wait for disconnect to propagate.
	time.Sleep(5 * time.Second)

	// Verify IBRL still works after multicast disconnect.
	log.Info("==> Verifying IBRL still works after multicast disconnect")
	verifyIBRLClientBGPEstablished(t, log, device)
	log.Info("--> IBRL still working after multicast disconnect")

	// Disconnect IBRL.
	log.Info("==> Disconnecting IBRL")
	_, err = client.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero disconnect --client-ip " + client.CYOANetworkIP,
	})
	if err != nil {
		log.Info("--> Warning: IBRL disconnect returned error", "error", err)
	}

	log.Info("--> Simultaneous tunnel test completed successfully")
}
