//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_MultiClientIBRLAllocatedIP(t *testing.T) {
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

	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ewr1-dz01")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)

	var wg sync.WaitGroup
	deviceCode1 := "la2-dz01"
	deviceCode2 := "ewr1-dz01"
	var devicePK1, devicePK2 string

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Add la2-dz01 device in xlax exchange.
		device1, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:     deviceCode1,
			Location: "lax",
			Exchange: "xlax",
			// .8/29 has network address .8, allocatable up to .14, and broadcast .15
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
			},
			LoopbackInterfaces: map[string]string{
				"Loopback255": "vpnv4",
				"Loopback256": "ipv4",
			},
		})
		require.NoError(t, err)
		devicePK1 = device1.ID
		log.Debug("--> Device1 added", "deviceCode", deviceCode1, "devicePK", devicePK1)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Add ewr1-dz01 device in xewr exchange.
		device2, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:     deviceCode2,
			Location: "ewr",
			Exchange: "xewr",
			// .16/29 has network address .16, allocatable up to .22, and broadcast .23
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
			AdditionalNetworks:           []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
			},
			LoopbackInterfaces: map[string]string{
				"Loopback255": "vpnv4",
				"Loopback256": "ipv4",
			},
		})
		require.NoError(t, err)
		devicePK2 = device2.ID
		log.Debug("--> Device2 added", "deviceCode", deviceCode2, "devicePK", devicePK2)
	}()

	// Wait for devices to be added.
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
	log.Debug("--> Devices exist onchain", "deviceCode1", deviceCode1, "devicePK1", devicePK1, "deviceCode2", deviceCode2, "devicePK2", devicePK2)

	log.Debug("==> Creating link onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero link create wan --code \"la2-dz01:ewr1-dz01\" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ewr1-dz01 --side-z-interface Ethernet2 --bandwidth \"10 Gbps\" --mtu 2048 --delay-ms 40 --jitter-ms 3 --desired-status activated"})
	require.NoError(t, err)
	log.Debug("--> Link created onchain")

	// Add client1.
	log.Debug("==> Adding client1")
	client1, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID:       100,
		RouteLivenessEnableActive: true,
	})
	require.NoError(t, err)
	log.Debug("--> Client1 added", "client1Pubkey", client1.Pubkey, "client1IP", client1.CYOANetworkIP)

	// Add client2.
	log.Debug("==> Adding client2")
	client2, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID:        110,
		RouteLivenessEnablePassive: true, // route liveness in passive mode for this client
	})
	require.NoError(t, err)
	log.Debug("--> Client2 added", "client2Pubkey", client2.Pubkey, "client2IP", client2.CYOANetworkIP)

	// Wait for client latency results.
	log.Debug("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Finished waiting for client latency results")

	log.Debug("==> Add clients to user Access Pass")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Clients added to user Access Pass")

	// Run IBRL with allocated IP workflow test.
	runMultiClientIBRLAllocatedIPWorkflowTest(t, log, dn, client1, client2, deviceCode1, deviceCode2)
}

func runMultiClientIBRLAllocatedIPWorkflowTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, client1 *devnet.Client, client2 *devnet.Client, deviceCode1 string, deviceCode2 string) {
	// Check that the clients are disconnected and do not have a DZ IP allocated.
	log.Debug("==> Checking that the clients are disconnected and do not have a DZ IP allocated")
	status, err := client1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	status, err = client2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")

	// Connect client1 in IBRL mode to device1 (xlax exchange) with allocated IP.
	log.Debug("==> Connecting client1 in IBRL mode with allocated IP to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--allocate-addr", "--device", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client1 connected in IBRL mode with allocated IP to device1")

	// Connect client2 in IBRL mode to device2 (xewr exchange) with allocated IP.
	log.Debug("==> Connecting client2 in IBRL mode with allocated IP to device2")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--allocate-addr", "--device", deviceCode2})
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client2 connected in IBRL mode with allocated IP to device2")

	// Check that the clients have a DZ IP different from their client IP when configured to use an allocated IP.
	log.Debug("==> Checking that the clients have a DZ IP different from their client IP when configured to use an allocated IP")
	status, err = client1.GetTunnelStatus(t.Context())
	require.Len(t, status, 1, status)
	client1DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.NotEqual(t, client1.CYOANetworkIP, client1DZIP)
	status, err = client2.GetTunnelStatus(t.Context())
	require.Len(t, status, 1, status)
	client2DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.NotEqual(t, client2.CYOANetworkIP, client2DZIP)
	log.Debug("--> Clients have a DZ IP different from their client IP when configured to use an allocated IP")

	// Wait for cross-exchange routes to propagate via iBGP between devices.
	log.Debug("==> Waiting for cross-exchange routes to propagate via iBGP")
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf("Cli -c \"show ip route vrf vrf1 %s/32\"", client2DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 90*time.Second, 1*time.Second, "device1 should have route to client2 via iBGP")
	require.Eventually(t, func() bool {
		output, err := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf("Cli -c \"show ip route vrf vrf1 %s/32\"", client1DZIP)})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 90*time.Second, 1*time.Second, "device2 should have route to client1 via iBGP")
	log.Debug("--> Cross-exchange routes have propagated via iBGP")

	// Check that the clients have routes to each other.
	log.Debug("==> Checking that the clients have routes to each other")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 60*time.Second, 1*time.Second, "client1 should have route to client2")
	require.Eventually(t, func() bool {
		output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 60*time.Second, 1*time.Second, "client2 should have route to client1")
	log.Debug("--> Clients have routes to each other")

	// Disconnect client1.
	log.Debug("==> Disconnecting client1 from IBRL with allocated IP")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client1 disconnected from IBRL with allocated IP")

	// Disconnect client2.
	log.Debug("==> Disconnecting client2 from IBRL with allocated IP")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client2.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client2 disconnected from IBRL with allocated IP")

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

	// Check that the clients are eventually disconnected and do not have a DZ IP allocated.
	log.Debug("==> Checking that the clients are eventually disconnected and do not have a DZ IP allocated")
	err = client1.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = client2.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	status, err = client1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	status, err = client2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")
}
