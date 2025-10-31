//go:build e2e

package e2e_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_MultiClient(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

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

	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ewr1-dz01")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)

	// Add la2-dz01 device in xlax exchange.
	deviceCode1 := "la2-dz01"
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
	devicePK1 := device1.ID
	log.Info("--> Device1 added", "deviceCode", deviceCode1, "devicePK", devicePK1)

	// Add ewr1-dz01 device in xewr exchange.
	deviceCode2 := "ewr1-dz01"
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
	devicePK2 := device2.ID
	log.Info("--> Device2 added", "deviceCode", deviceCode2, "devicePK", devicePK2)

	// Wait for devices to exist onchain.
	log.Info("==> Waiting for devices to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 2
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Devices exist onchain", "deviceCode1", deviceCode1, "devicePK1", devicePK1, "deviceCode2", deviceCode2, "devicePK2", devicePK2)

	log.Info("==> Creating link onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero link create wan --code \"la2-dz01:ewr1-dz01\" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ewr1-dz01 --side-z-interface Ethernet2 --bandwidth \"10 Gbps\" --mtu 2048 --delay-ms 40 --jitter-ms 3"})
	require.NoError(t, err)
	log.Info("--> Link created onchain")

	// Add a client.
	log.Info("==> Adding client1")
	client1, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
		// Route probing disabled until we have a way to test connectivity between clients in the
		// containerized environment.
		// https://github.com/malbeclabs/doublezero/issues/2016
		RouteProbingEnable: false,
	})
	require.NoError(t, err)
	log.Info("--> Client1 added", "client1Pubkey", client1.Pubkey, "client1IP", client1.CYOANetworkIP)

	// Add another client.
	log.Info("==> Adding client2")
	client2, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
		// Route probing disabled until we have a way to test connectivity between clients in the
		// containerized environment.
		// https://github.com/malbeclabs/doublezero/issues/2016
		RouteProbingEnable: false,
	})
	require.NoError(t, err)
	log.Info("--> Client2 added", "client2Pubkey", client2.Pubkey, "client2IP", client2.CYOANetworkIP)

	// Wait for client latency results.
	log.Info("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Finished waiting for client latency results")

	log.Info("==> Add clients to user Access Pass")
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	log.Info("--> Clients added to user Access Pass")

	// Run IBRL workflow test.
	if !t.Run("ibrl", func(t *testing.T) {
		runMultiClientIBRLWorkflowTest(t, log, dn, client1, client2, deviceCode1, deviceCode2)
	}) {
		t.Fail()
	}

	// Run IBRL with allocated IP workflow test.
	if !t.Run("ibrl_with_allocated_ip", func(t *testing.T) {
		runMultiClientIBRLWithAllocatedIPWorkflowTest(t, log, dn, client1, client2, deviceCode1, deviceCode2)
	}) {
		t.Fail()
	}
}

func runMultiClientIBRLWorkflowTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, client1 *devnet.Client, client2 *devnet.Client, deviceCode1 string, deviceCode2 string) {
	// Check that the clients are disconnected and do not have a DZ IP allocated.
	log.Info("==> Checking that the clients are disconnected and do not have a DZ IP allocated")
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
	log.Info("--> Confirmed clients are disconnected and do not have a DZ IP allocated")

	// Connect client1 in IBRL mode to device1 (xlax exchange).
	log.Info("==> Connecting client1 in IBRL mode to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode to device1")

	// Connect client2 in IBRL mode to device2 (xewr exchange).
	log.Info("==> Connecting client2 in IBRL mode to device2")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client2 connected in IBRL mode to device2")

	// Check that the clients have a DZ IP equal to their client IP when not configured to use an allocated IP.
	log.Info("==> Checking that the clients have a DZ IP as public IP when not configured to use an allocated IP")
	status, err = client1.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client1DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client1.CYOANetworkIP, client1DZIP)
	status, err = client2.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client2DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client2.CYOANetworkIP, client2DZIP)
	log.Info("--> Clients have a DZ IP as public IP when not configured to use an allocated IP")

	// Check that the clients have routes to each other.
	log.Info("==> Checking that the clients have routes to each other")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 120*time.Second, 5*time.Second, "client1 should have route to client2")
	require.Eventually(t, func() bool {
		output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 120*time.Second, 5*time.Second, "client2 should have route to client1")
	log.Info("--> Clients have routes to each other")

	// Check that the clients can reach each other via their DZ IPs, via ping.
	log.Info("==> Checking that the clients can reach each other via their DZ IPs")
	_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client1.Exec(t.Context(), []string{"ping", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client2.Exec(t.Context(), []string{"ping", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	log.Info("--> Clients can reach each other via their DZ IPs")

	// Disconnect client1.
	log.Info("==> Disconnecting client1 from IBRL")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Client1 disconnected from IBRL")

	// Disconnect client2.
	log.Info("==> Disconnecting client2 from IBRL")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client2.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Client2 disconnected from IBRL")

	// Wait for users to be deleted onchain.
	log.Info("==> Waiting for users to be deleted onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Users) == 0
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Users deleted onchain")

	// Check that the clients are eventually disconnected and do not have a DZ IP allocated.
	log.Info("==> Checking that the clients are eventually disconnected and do not have a DZ IP allocated")
	err = client1.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = client2.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	status, err = client1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	status, err = client2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	log.Info("--> Confirmed clients are disconnected and do not have a DZ IP allocated")
}

func runMultiClientIBRLWithAllocatedIPWorkflowTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, client1 *devnet.Client, client2 *devnet.Client, deviceCode1 string, deviceCode2 string) {
	// Check that the clients are disconnected and do not have a DZ IP allocated.
	log.Info("==> Checking that the clients are disconnected and do not have a DZ IP allocated")
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
	log.Info("--> Confirmed clients are disconnected and do not have a DZ IP allocated")

	// Connect client1 in IBRL mode to device1 (xlax exchange) with allocated IP.
	log.Info("==> Connecting client1 in IBRL mode with allocated IP to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--allocate-addr", "--device", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode with allocated IP to device1")

	// Connect client2 in IBRL mode to device2 (xewr exchange) with allocated IP.
	log.Info("==> Connecting client2 in IBRL mode with allocated IP to device2")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--allocate-addr", "--device", deviceCode2})
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client2 connected in IBRL mode with allocated IP to device2")

	// Check that the clients have a DZ IP equal to their client IP when not configured to use an allocated IP.
	log.Info("==> Checking that the clients have a DZ IP different from their client IP when configured to use an allocated IP")
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
	log.Info("--> Clients have a DZ IP different from their client IP when configured to use an allocated IP")

	// Check that the clients have routes to each other.
	log.Info("==> Checking that the clients have routes to each other")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 120*time.Second, 5*time.Second, "client1 should have route to client2")
	require.Eventually(t, func() bool {
		output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 120*time.Second, 5*time.Second, "client2 should have route to client1")
	log.Info("--> Clients have routes to each other")

	// Disconnect client1.
	log.Info("==> Disconnecting client1 from IBRL with allocated IP")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Client1 disconnected from IBRL with allocated IP")

	// Disconnect client2.
	log.Info("==> Disconnecting client2 from IBRL with allocated IP")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client2.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Client2 disconnected from IBRL with allocated IP")

	// Wait for users to be deleted onchain.
	log.Info("==> Waiting for users to be deleted onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Users) == 0
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Users deleted onchain")

	// Check that the clients are eventually disconnected and do not have a DZ IP allocated.
	log.Info("==> Checking that the clients are eventually disconnected and do not have a DZ IP allocated")
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
	log.Info("--> Confirmed clients are disconnected and do not have a DZ IP allocated")
}
