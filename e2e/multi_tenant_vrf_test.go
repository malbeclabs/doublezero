//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_MultiTenantVRF(t *testing.T) {
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

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Create a link network between the two devices.
	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ewr1-dz01")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)

	// Phase 1: Add 2 devices in parallel.
	var wg sync.WaitGroup
	deviceCode1 := "la2-dz01"
	deviceCode2 := "ewr1-dz01"
	var devicePK1, devicePK2 string

	wg.Add(1)
	go func() {
		defer wg.Done()

		device1, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:                         deviceCode1,
			Location:                     "lax",
			Exchange:                     "xlax",
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces:                   map[string]string{"Ethernet2": "physical"},
			LoopbackInterfaces:           map[string]string{"Loopback255": "vpnv4", "Loopback256": "ipv4"},
		})
		require.NoError(t, err)
		devicePK1 = device1.ID
		log.Debug("--> Device1 added", "deviceCode", deviceCode1, "devicePK", devicePK1)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		device2, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:                         deviceCode2,
			Location:                     "ewr",
			Exchange:                     "xewr",
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces:                   map[string]string{"Ethernet2": "physical"},
			LoopbackInterfaces:           map[string]string{"Loopback255": "vpnv4", "Loopback256": "ipv4"},
		})
		require.NoError(t, err)
		devicePK2 = device2.ID
		log.Debug("--> Device2 added", "deviceCode", deviceCode2, "devicePK", devicePK2)
	}()

	wg.Wait()

	// Wait for devices to exist onchain.
	log.Debug("==> Waiting for devices to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 2
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Devices exist onchain", "devicePK1", devicePK1, "devicePK2", devicePK2)

	// Create WAN link between the two devices.
	log.Debug("==> Creating link onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero link create wan --code \"la2-dz01:ewr1-dz01\" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ewr1-dz01 --side-z-interface Ethernet2 --bandwidth \"10 Gbps\" --mtu 2048 --delay-ms 40 --jitter-ms 3 --desired-status activated"})
	require.NoError(t, err)
	log.Debug("--> Link created onchain")

	// Phase 2: Create tenants.
	log.Debug("==> Creating tenants")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-alpha"})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "create", "--code", "tenant-bravo"})
	require.NoError(t, err)
	log.Debug("--> Tenants created")

	// Discover auto-allocated VRF IDs via CLI.
	log.Debug("==> Discovering tenant VRF IDs")
	vrfAlpha := getTenantVrfID(t, dn, "tenant-alpha")
	vrfBravo := getTenantVrfID(t, dn, "tenant-bravo")
	require.NotZero(t, vrfAlpha)
	require.NotZero(t, vrfBravo)
	require.NotEqual(t, vrfAlpha, vrfBravo, "VRF IDs must be different")
	log.Debug("--> Tenant VRF IDs discovered", "vrfAlpha", vrfAlpha, "vrfBravo", vrfBravo)

	// Phase 3: Add 4 clients.
	log.Debug("==> Adding clients")
	clientA1, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 100})
	require.NoError(t, err)
	log.Debug("--> clientA1 added", "pubkey", clientA1.Pubkey, "ip", clientA1.CYOANetworkIP)

	clientA2, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 110})
	require.NoError(t, err)
	log.Debug("--> clientA2 added", "pubkey", clientA2.Pubkey, "ip", clientA2.CYOANetworkIP)

	clientB1, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 120})
	require.NoError(t, err)
	log.Debug("--> clientB1 added", "pubkey", clientB1.Pubkey, "ip", clientB1.CYOANetworkIP)

	clientB2, err := dn.AddClient(t.Context(), devnet.ClientSpec{CYOANetworkIPHostID: 130})
	require.NoError(t, err)
	log.Debug("--> clientB2 added", "pubkey", clientB2.Pubkey, "ip", clientB2.CYOANetworkIP)

	// Wait for client latency results against the respective devices.
	log.Debug("==> Waiting for client latency results")
	err = clientA1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = clientA2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	err = clientB1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = clientB2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client latency results received")

	// Set access passes for all 4 clients.
	log.Debug("==> Setting access passes")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + clientA1.CYOANetworkIP + " --user-payer " + clientA1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + clientA2.CYOANetworkIP + " --user-payer " + clientA2.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + clientB1.CYOANetworkIP + " --user-payer " + clientB1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + clientB2.CYOANetworkIP + " --user-payer " + clientB2.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Access passes set")

	runMultiTenantVRFWorkflowTest(t, log, dn, clientA1, clientA2, clientB1, clientB2, deviceCode1, deviceCode2, devicePK1, devicePK2, vrfAlpha, vrfBravo)
}

func runMultiTenantVRFWorkflowTest(
	t *testing.T, log *slog.Logger, dn *devnet.Devnet,
	clientA1, clientA2, clientB1, clientB2 *devnet.Client,
	deviceCode1, deviceCode2 string,
	devicePK1, devicePK2 string,
	vrfAlpha, vrfBravo uint16,
) {
	// Verify all clients are initially disconnected.
	log.Debug("==> Checking that clients are disconnected")
	for _, c := range []*devnet.Client{clientA1, clientA2, clientB1, clientB2} {
		status, err := c.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1, status)
		require.Nil(t, status[0].DoubleZeroIP, status)
		require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	}
	log.Debug("--> Confirmed clients are disconnected")

	// Phase 4: Connect clients with tenant (positional arg to `ibrl` subcommand).
	// tenant-alpha: clientA1 → device1, clientA2 → device2
	log.Debug("==> Connecting tenant-alpha clients")
	_, err := clientA1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "tenant-alpha", "--client-ip", clientA1.CYOANetworkIP, "--device", deviceCode1})
	require.NoError(t, err)
	_, err = clientA2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "tenant-alpha", "--client-ip", clientA2.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	log.Debug("--> tenant-alpha clients connected")

	// tenant-bravo: clientB1 → device1, clientB2 → device2
	log.Debug("==> Connecting tenant-bravo clients")
	_, err = clientB1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "tenant-bravo", "--client-ip", clientB1.CYOANetworkIP, "--device", deviceCode1})
	require.NoError(t, err)
	_, err = clientB2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "tenant-bravo", "--client-ip", clientB2.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	log.Debug("--> tenant-bravo clients connected")

	// Wait for all tunnels to come up.
	log.Debug("==> Waiting for all tunnels to be up")
	err = clientA1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = clientA2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = clientB1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = clientB2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> All tunnels up")

	// Capture DZ IPs from tunnel status.
	log.Debug("==> Capturing DZ IPs")
	status, err := clientA1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1)
	clientA1DZIP := status[0].DoubleZeroIP.String()

	status, err = clientA2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1)
	clientA2DZIP := status[0].DoubleZeroIP.String()

	status, err = clientB1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1)
	clientB1DZIP := status[0].DoubleZeroIP.String()

	status, err = clientB2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1)
	clientB2DZIP := status[0].DoubleZeroIP.String()
	log.Debug("--> DZ IPs captured", "clientA1", clientA1DZIP, "clientA2", clientA2DZIP, "clientB1", clientB1DZIP, "clientB2", clientB2DZIP)

	// Phase 5: Verify controller config has both VRF instances.
	log.Debug("==> Verifying controller config contains VRF instances")
	for _, devicePK := range []string{devicePK1, devicePK2} {
		require.Eventually(t, func() bool {
			got, err := dn.Controller.GetAgentConfig(t.Context(), devicePK)
			if err != nil {
				return false
			}
			cfg := got.Config
			hasVrfAlpha := strings.Contains(cfg, fmt.Sprintf("vrf instance vrf%d", vrfAlpha))
			hasVrfBravo := strings.Contains(cfg, fmt.Sprintf("vrf instance vrf%d", vrfBravo))
			hasRdAlpha := strings.Contains(cfg, fmt.Sprintf("rd 65342:%d", vrfAlpha))
			hasRdBravo := strings.Contains(cfg, fmt.Sprintf("rd 65342:%d", vrfBravo))
			hasRtAlpha := strings.Contains(cfg, fmt.Sprintf("route-target import vpn-ipv4 65342:%d", vrfAlpha))
			hasRtBravo := strings.Contains(cfg, fmt.Sprintf("route-target import vpn-ipv4 65342:%d", vrfBravo))
			return hasVrfAlpha && hasVrfBravo && hasRdAlpha && hasRdBravo && hasRtAlpha && hasRtBravo
		}, 30*time.Second, 2*time.Second, "device %s config should contain both VRF instances", devicePK)
	}
	log.Debug("--> Controller config verified with both VRF instances")

	// Phase 6: Verify intra-VRF route propagation (positive).
	log.Debug("==> Waiting for intra-VRF cross-device routes to propagate via iBGP")

	// Device1 should have clientA2's route in VRF alpha (clientA2 is on device2).
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfAlpha, clientA2DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), clientA2DZIP)
	}, 90*time.Second, 2*time.Second, "device1 should have route to clientA2 in VRF alpha via iBGP")

	// Device1 should have clientB2's route in VRF bravo (clientB2 is on device2).
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfBravo, clientB2DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), clientB2DZIP)
	}, 90*time.Second, 2*time.Second, "device1 should have route to clientB2 in VRF bravo via iBGP")

	// Device2 should have clientA1's route in VRF alpha (clientA1 is on device1).
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfAlpha, clientA1DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), clientA1DZIP)
	}, 90*time.Second, 2*time.Second, "device2 should have route to clientA1 in VRF alpha via iBGP")

	// Device2 should have clientB1's route in VRF bravo (clientB1 is on device1).
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfBravo, clientB1DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), clientB1DZIP)
	}, 90*time.Second, 2*time.Second, "device2 should have route to clientB1 in VRF bravo via iBGP")
	log.Debug("--> Intra-VRF routes propagated")

	// Phase 7: Verify inter-VRF isolation (negative).
	log.Debug("==> Verifying inter-VRF route isolation")

	// clientA1's route (VRF alpha) must NOT appear in VRF bravo on device1.
	require.Never(t, func() bool {
		output, _ := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfBravo, clientA1DZIP)})
		return strings.Contains(string(output), clientA1DZIP)
	}, 10*time.Second, 1*time.Second, "clientA1 route must not appear in VRF bravo on device1")

	// clientB1's route (VRF bravo) must NOT appear in VRF alpha on device1.
	require.Never(t, func() bool {
		output, _ := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfAlpha, clientB1DZIP)})
		return strings.Contains(string(output), clientB1DZIP)
	}, 10*time.Second, 1*time.Second, "clientB1 route must not appear in VRF alpha on device1")

	// clientA2's route (VRF alpha) must NOT appear in VRF bravo on device2.
	require.Never(t, func() bool {
		output, _ := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfBravo, clientA2DZIP)})
		return strings.Contains(string(output), clientA2DZIP)
	}, 10*time.Second, 1*time.Second, "clientA2 route must not appear in VRF bravo on device2")

	// clientB2's route (VRF bravo) must NOT appear in VRF alpha on device2.
	require.Never(t, func() bool {
		output, _ := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c",
			fmt.Sprintf(`Cli -c "show ip route vrf vrf%d %s/32"`, vrfAlpha, clientB2DZIP)})
		return strings.Contains(string(output), clientB2DZIP)
	}, 10*time.Second, 1*time.Second, "clientB2 route must not appear in VRF alpha on device2")
	log.Debug("--> Inter-VRF route isolation verified")

	// Phase 8: Verify client connectivity.
	log.Debug("==> Verifying intra-VRF client connectivity")

	// clientA1 can ping clientA2 (same tenant, cross-device).
	_, err = clientA1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientA2DZIP, "-W", "1"})
	require.NoError(t, err)
	// clientA2 can ping clientA1 (same tenant, cross-device).
	_, err = clientA2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientA1DZIP, "-W", "1"})
	require.NoError(t, err)
	// clientB1 can ping clientB2 (same tenant, cross-device).
	_, err = clientB1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientB2DZIP, "-W", "1"})
	require.NoError(t, err)
	// clientB2 can ping clientB1 (same tenant, cross-device).
	_, err = clientB2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientB1DZIP, "-W", "1"})
	require.NoError(t, err)
	log.Debug("--> Intra-VRF connectivity verified")

	log.Debug("==> Verifying inter-VRF client isolation")

	// clientA1 cannot ping clientB1 (different tenant, same device).
	_, err = clientA1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientB1DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)
	// clientA1 cannot ping clientB2 (different tenant, cross-device).
	_, err = clientA1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientB2DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)
	// clientB1 cannot ping clientA1 (different tenant, same device).
	_, err = clientB1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientA1DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)
	// clientB1 cannot ping clientA2 (different tenant, cross-device).
	_, err = clientB1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", clientA2DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)
	log.Debug("--> Inter-VRF client isolation verified")

	// Phase 9: Disconnect + cleanup.
	log.Debug("==> Disconnecting all clients")
	_, err = clientA1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", clientA1.CYOANetworkIP})
	require.NoError(t, err)
	_, err = clientA2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", clientA2.CYOANetworkIP})
	require.NoError(t, err)
	_, err = clientB1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", clientB1.CYOANetworkIP})
	require.NoError(t, err)
	_, err = clientB2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", clientB2.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> All clients disconnected")

	// Wait for users to be deleted onchain.
	log.Debug("==> Waiting for users to be deleted onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Users) == 0
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Users deleted onchain")

	// Verify all tunnels are disconnected.
	log.Debug("==> Checking that clients are eventually disconnected")
	err = clientA1.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = clientA2.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = clientB1.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = clientB2.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)

	for _, c := range []*devnet.Client{clientA1, clientA2, clientB1, clientB2} {
		status, err := c.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, status, 1, status)
		require.Nil(t, status[0].DoubleZeroIP, status)
	}
	log.Debug("--> Confirmed all clients are disconnected and DZ IPs released")
}

// getTenantVrfID fetches the VRF ID for a tenant by parsing the output of
// `doublezero tenant get --code <code>`.
func getTenantVrfID(t *testing.T, dn *devnet.Devnet, tenantCode string) uint16 {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "tenant", "get", "--code", tenantCode})
	require.NoError(t, err)
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "vrf_id: ") {
			val, err := strconv.ParseUint(strings.TrimPrefix(line, "vrf_id: "), 10, 16)
			require.NoError(t, err)
			return uint16(val)
		}
	}
	t.Fatalf("vrf_id not found in tenant get output for %s: %s", tenantCode, string(output))
	return 0
}
