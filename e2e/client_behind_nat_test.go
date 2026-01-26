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

// TestE2E_ClientBehindNAT verifies that IBRL mode works for clients behind a NAT gateway.
// This proves that the tunnel can be established and BGP session comes up when the client's
// public IP differs from its private IP (i.e., the client is behind NAT).
//
// The test also verifies that IBRL with --allocate-addr works alongside the NAT client.
func TestE2E_ClientBehindNAT(t *testing.T) {
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

	// Add a single device.
	deviceCode := "ewr1-dz01"
	log.Info("==> Adding device", "deviceCode", deviceCode)
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         deviceCode,
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          16,
		CYOANetworkAllocatablePrefix: 29,
		Interfaces: map[string]string{
			"Ethernet2": "physical",
		},
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
	})
	require.NoError(t, err)
	devicePK := device.ID
	log.Info("--> Device added", "deviceCode", deviceCode, "devicePK", devicePK)

	// Wait for device to exist onchain.
	log.Info("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		return len(data.Devices) == 1
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Device exists onchain")

	// Create NAT infrastructure.
	log.Info("==> Creating NAT infrastructure")
	behindNATNetwork := devnet.NewBehindNATNetwork(dn, log, "nat1")
	_, err = behindNATNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)
	log.Info("--> Behind-NAT network created", "name", behindNATNetwork.Name, "subnet", behindNATNetwork.SubnetCIDR)

	// Create and start NAT gateway.
	natGateway := &devnet.NATGateway{
		Spec: &devnet.NATGatewaySpec{
			Code:                     "gw1",
			BehindNATNetworkIPHostID: 2,
			CYOANetworkIPHostID:      130,
		},
		BehindNATNetwork: behindNATNetwork,
	}
	natGateway.SetDevnet(dn, log)
	_, err = natGateway.StartIfNotRunning(t.Context())
	require.NoError(t, err)
	log.Info("--> NAT gateway started", "behindNATIP", natGateway.BehindNATNetworkIP, "cyoaIP", natGateway.CYOANetworkIP)

	// Add an IBRL with --allocate-addr behind NAT.
	log.Info("==> Adding allocate-addr client")
	allocateAddrNatClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Info("--> Allocate-addr client added", "pubkey", allocateAddrNatClient.Pubkey, "ip", allocateAddrNatClient.CYOANetworkIP)

	// Add an IBRL client behind NAT.
	log.Info("==> Adding client behind NAT")
	ibrlNatClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		BehindNATGateway:         natGateway,
		BehindNATNetworkIPHostID: 10,
	})
	require.NoError(t, err)
	log.Info("--> NAT client added", "pubkey", ibrlNatClient.Pubkey, "privateIP", ibrlNatClient.PrivateIP, "publicIP", ibrlNatClient.CYOANetworkIP)

	// Verify NAT client's public IP is the NAT gateway's CYOA IP.
	require.Equal(t, natGateway.CYOANetworkIP, ibrlNatClient.CYOANetworkIP)

	// Configure NAT rules for the client.
	log.Info("==> Configuring NAT for client")
	err = natGateway.ConfigureNATForClient(t.Context(), ibrlNatClient.PrivateIP)
	require.NoError(t, err)
	log.Info("--> NAT configured for client")

	// Wait for latency results.
	log.Info("==> Waiting for client latency results")
	err = allocateAddrNatClient.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	err = ibrlNatClient.WaitForLatencyResults(t.Context(), devicePK, 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Latency results received")

	// Add clients to Access Pass.
	log.Info("==> Adding clients to Access Pass")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + allocateAddrNatClient.CYOANetworkIP + " --user-payer " + allocateAddrNatClient.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + ibrlNatClient.CYOANetworkIP + " --user-payer " + ibrlNatClient.Pubkey})
	require.NoError(t, err)
	log.Info("--> Clients added to Access Pass")

	// Run connect subtest.
	if !t.Run("connect", func(t *testing.T) {
		log.Info("==> Connecting allocate-addr client in IBRL mode with --allocate-addr")
		_, err = allocateAddrNatClient.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", allocateAddrNatClient.CYOANetworkIP, "--device", deviceCode, "--allocate-addr"})
		require.NoError(t, err)
		log.Info("--> Allocate-addr client connected")

		log.Info("==> Connecting NAT client in IBRL mode")
		_, err = ibrlNatClient.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", ibrlNatClient.CYOANetworkIP, "--device", deviceCode})
		require.NoError(t, err)
		log.Info("--> NAT client connected")

		log.Info("==> Waiting for tunnels to come up")
		err = allocateAddrNatClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)
		log.Info("--> Allocate-addr client tunnel up")

		err = ibrlNatClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)
		log.Info("--> NAT client tunnel up (BGP session established)")

		log.Info("==> Verifying tunnel status")

		allocateAddrStatus, err := allocateAddrNatClient.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, allocateAddrStatus, 1)
		allocateAddrDZIP := allocateAddrStatus[0].DoubleZeroIP.String()
		log.Info("--> Allocate-addr client DZ IP", "ip", allocateAddrDZIP)
		// Allocate-addr client should get an IP from device's allocatable range (not its CYOA IP).
		require.NotEqual(t, allocateAddrNatClient.CYOANetworkIP, allocateAddrDZIP, "allocate-addr client should get allocated IP, not CYOA IP")

		natStatus, err := ibrlNatClient.GetTunnelStatus(t.Context())
		require.NoError(t, err)
		require.Len(t, natStatus, 1)
		natDZIP := natStatus[0].DoubleZeroIP.String()
		log.Info("--> NAT client DZ IP", "ip", natDZIP)
		require.Equal(t, ibrlNatClient.CYOANetworkIP, natDZIP, "NAT client's DZ IP should be NAT gateway's CYOA IP")

		log.Info("--> Verified: allocate-addr client got allocated IP, NAT client uses gateway's public IP")

		log.Info("==> Testing connectivity between clients")

		// Allocate-addr client pings NAT client.
		_, err = allocateAddrNatClient.Exec(t.Context(), []string{"ping", "-c", "3", natDZIP, "-W", "1"})
		require.NoError(t, err)
		log.Info("--> Allocate-addr client can ping NAT client", "src", allocateAddrDZIP, "dst", natDZIP)

		// NAT client pings allocate-addr client.
		_, err = ibrlNatClient.Exec(t.Context(), []string{"ping", "-c", "3", allocateAddrDZIP, "-W", "1"})
		require.NoError(t, err)
		log.Info("--> NAT client can ping allocate-addr client", "src", natDZIP, "dst", allocateAddrDZIP)

		log.Info("--> Connectivity verified: bidirectional ping works with ibrl and ibrl -a clients behind NAT")
	}) {
		t.Fail()
		return
	}

	// Run disconnect subtest.
	if !t.Run("disconnect", func(t *testing.T) {
		log.Info("==> Disconnecting clients")
		_, err = allocateAddrNatClient.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", allocateAddrNatClient.CYOANetworkIP})
		require.NoError(t, err)
		_, err = ibrlNatClient.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", ibrlNatClient.CYOANetworkIP})
		require.NoError(t, err)
		log.Info("--> Clients disconnected")

		log.Info("==> Waiting for tunnels to disconnect")
		err = allocateAddrNatClient.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
		require.NoError(t, err)
		err = ibrlNatClient.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
		require.NoError(t, err)
		log.Info("--> Tunnels disconnected")
	}) {
		t.Fail()
	}

	log.Info("==> Test completed successfully - IBRL mode works via NAT and with --allocate-addr")
}
