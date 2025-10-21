//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_Controller_NoIfacesAndPeers(t *testing.T) {
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
		Controller: devnet.ControllerSpec{
			NoEnableInterfacesAndPeers: true,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Info("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Info("--> Devnet started")

	// Add la2-dz01 device in xlax exchange.
	deviceCode1 := "la2-dz01"
	device1, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     deviceCode1,
		Location: "lax",
		Exchange: "xlax",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
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
	err = client1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Finished waiting for client latency results")

	// Add clients to user Access Pass so they can open user connections.
	log.Info("==> Adding clients to Access Pass")
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	log.Info("--> Finished adding clients to Access Pass")

	// Run IBRL with allocated IP workflow test.
	if !t.Run("ibrl_with_allocated_ip", func(t *testing.T) {
		runMultiClientIBRLWithAllocatedIPWorkflowTest(t, log, dn, client1, client2, deviceCode1, deviceCode2)
	}) {
		t.Fail()
	}
}
