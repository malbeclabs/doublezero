//go:build e2e

package e2e_test

import (
	"log/slog"
	"os"
	"path/filepath"
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

	// Add la2-dz01 device.
	deviceCode := "la2-dz01"
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     deviceCode,
		Location: "lax",
		Exchange: "xlax",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)
	devicePK := device.ID
	log.Info("--> Device added", "deviceCode", deviceCode, "devicePK", devicePK)

	err = dn.CreateDeviceLoopbackInterface(t.Context(), deviceCode, "Loopback255", "vpnv4")
	require.NoError(t, err, "failed to create VPNv4 loopback interface for device %s: %w", deviceCode, err)

	err = dn.CreateDeviceLoopbackInterface(t.Context(), deviceCode, "Loopback256", "ipv4")
	require.NoError(t, err, "failed to create IPv4 loopback interface for device %s: %w", deviceCode, err)

	// Wait for device to exist onchain.
	log.Info("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 1
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Device exists onchain", "deviceCode", deviceCode, "devicePK", devicePK)

	// Add a client.
	log.Info("==> Adding client1")
	client1, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Info("--> Client1 added", "client1Pubkey", client1.Pubkey, "client1IP", client1.CYOANetworkIP)

	// Add another client.
	log.Info("==> Adding client2")
	client2, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
	})
	require.NoError(t, err)
	log.Info("--> Client2 added", "client2Pubkey", client2.Pubkey, "client2IP", client2.CYOANetworkIP)

	// Wait for client latency results.
	log.Info("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Finished waiting for client latency results")

	// Add clients to user allowlist so they can open user connections.
	log.Info("==> Adding clients to user allowlist")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "allowlist", "add", "--pubkey", client1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "allowlist", "add", "--pubkey", client2.Pubkey})
	require.NoError(t, err)
	log.Info("--> Clients added to user allowlist")

	// Run IBRL workflow test.
	if !t.Run("ibrl", func(t *testing.T) {
		runMultiClientIBRLWorkflowTest(t, log, client1, client2)
	}) {
		t.Fail()
	}

	// Run IBRL with allocated IP workflow test.
	if !t.Run("ibrl_with_allocated_ip", func(t *testing.T) {
		runMultiClientIBRLWithAllocatedIPWorkflowTest(t, log, client1, client2)
	}) {
		t.Fail()
	}
}

func runMultiClientIBRLWorkflowTest(t *testing.T, log *slog.Logger, client1 *devnet.Client, client2 *devnet.Client) {
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

	// Connect client1 in IBRL mode.
	log.Info("==> Connecting client1 in IBRL mode")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode")

	// Connect client2 in IBRL mode.
	log.Info("==> Connecting client2 in IBRL mode")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP})
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client2 connected in IBRL mode")

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

	// Check that the clients can reach each other via their DZ IPs, via ping.
	log.Info("==> Checking that the clients can reach each other via their DZ IPs")
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

func runMultiClientIBRLWithAllocatedIPWorkflowTest(t *testing.T, log *slog.Logger, client1 *devnet.Client, client2 *devnet.Client) {
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

	// Connect client1 in IBRL mode.
	log.Info("==> Connecting client1 in IBRL mode with allocated IP")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--allocate-addr"})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode with allocated IP")

	// Connect client2 in IBRL mode.
	log.Info("==> Connecting client2 in IBRL mode with allocated IP")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--allocate-addr"})
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client2 connected in IBRL mode with allocated IP")

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

	// Check that the clients can reach each other via their DZ IPs, via ping.
	log.Info("==> Checking that the clients can reach each other via their DZ IPs")
	_, err = client1.Exec(t.Context(), []string{"ping", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client2.Exec(t.Context(), []string{"ping", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	log.Info("--> Clients can reach each other via their DZ IPs")

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
