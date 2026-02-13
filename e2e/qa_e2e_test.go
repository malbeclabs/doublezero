//go:build e2e

package e2e_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// TestE2E_QAAgent_UnicastConnectivity validates the QA agent and QA client library
// against a local Docker devnet. It exercises the same code path as the real QA tests:
// connect via QA agent, wait for status, wait for routes, ping, disconnect.
func TestE2E_QAAgent_UnicastConnectivity(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)
	ctx := t.Context()

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
	err = dn.Start(ctx, nil)
	require.NoError(t, err)
	log.Debug("--> Devnet started")

	// Create a link network for the two devices.
	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ewr1-dz01")
	_, err = linkNetwork.CreateIfNotExists(ctx)
	require.NoError(t, err)

	// Add two devices in parallel, in different exchanges.
	var wg sync.WaitGroup
	deviceCode1 := "la2-dz01"
	deviceCode2 := "ewr1-dz01"
	var devicePK1, devicePK2 string

	wg.Add(1)
	go func() {
		defer wg.Done()
		device1, err := dn.AddDevice(ctx, devnet.DeviceSpec{
			Code:                         deviceCode1,
			Location:                     "lax",
			Exchange:                     "xlax",
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
		device2, err := dn.AddDevice(ctx, devnet.DeviceSpec{
			Code:                         deviceCode2,
			Location:                     "ewr",
			Exchange:                     "xewr",
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

	wg.Wait()

	// Wait for devices to exist onchain.
	log.Debug("==> Waiting for devices to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		return len(data.Devices) == 2
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Devices exist onchain")

	// Create a WAN link between the two devices.
	log.Debug("==> Creating link onchain")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `doublezero link create wan --code "la2-dz01:ewr1-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ewr1-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 2048 --delay-ms 40 --jitter-ms 3 --desired-status activated`})
	require.NoError(t, err)
	log.Debug("--> Link created onchain")

	// Add two clients with QA agent enabled.
	log.Debug("==> Adding client1 with QA agent")
	client1, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
		EnableQAAgent:       true,
	})
	require.NoError(t, err)
	require.NotZero(t, client1.QAAgentHostPort, "client1 QA agent host port should be mapped")
	log.Debug("--> Client1 added", "pubkey", client1.Pubkey, "cyoaIP", client1.CYOANetworkIP, "qaAgentHostPort", client1.QAAgentHostPort)

	log.Debug("==> Adding client2 with QA agent")
	client2, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
		EnableQAAgent:       true,
	})
	require.NoError(t, err)
	require.NotZero(t, client2.QAAgentHostPort, "client2 QA agent host port should be mapped")
	log.Debug("--> Client2 added", "pubkey", client2.Pubkey, "cyoaIP", client2.CYOANetworkIP, "qaAgentHostPort", client2.QAAgentHostPort)

	// Wait for client latency results from both devices.
	log.Debug("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(ctx, devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(ctx, devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Finished waiting for client latency results")

	// Set access passes for clients.
	log.Debug("==> Setting access passes for clients")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Access passes set")

	// Build network config for the QA client, pointing at the E2E test's ledger.
	networkConfig, err := config.NetworkConfigForEnv("localnet")
	require.NoError(t, err)
	// Override the ledger URL to point at the E2E test's ledger (not the default localhost:8899).
	networkConfig.LedgerPublicRPCURL = fmt.Sprintf("http://%s:%d", dn.ExternalHost, dn.Ledger.ExternalRPCPort)
	// Override the serviceability program ID to match the E2E test's deployed program.
	programID, err := solana.PublicKeyFromBase58(dn.Manager.ServiceabilityProgramID)
	require.NoError(t, err)
	networkConfig.ServiceabilityProgramID = programID

	// Build devices map from devnet device data for the QA client.
	devices := map[string]*qa.Device{
		deviceCode1: {
			PubKey:       devicePK1,
			Code:         deviceCode1,
			ExchangeCode: "xlax",
		},
		deviceCode2: {
			PubKey:       devicePK2,
			Code:         deviceCode2,
			ExchangeCode: "xewr",
		},
	}

	// Create qa.Client instances connected to the QA agent gRPC ports.
	log.Debug("==> Creating QA clients")
	qaClient1, err := qa.NewClient(ctx, log, dn.ExternalHost, client1.QAAgentHostPort, networkConfig, devices, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = qaClient1.Close() })
	// In E2E containers the auto-detected public IP is the default Docker network address,
	// not the CYOA network IP. Override both the public IP and connect-command client IP so
	// that route lookups, disconnect checks, and the connect command all use the CYOA IP.
	qaClient1.SetPublicIP(net.ParseIP(client1.CYOANetworkIP))
	qaClient1.ClientIP = client1.CYOANetworkIP

	qaClient2, err := qa.NewClient(ctx, log, dn.ExternalHost, client2.QAAgentHostPort, networkConfig, devices, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = qaClient2.Close() })
	qaClient2.SetPublicIP(net.ParseIP(client2.CYOANetworkIP))
	qaClient2.ClientIP = client2.CYOANetworkIP
	log.Debug("--> QA clients created")

	// Connect both clients via the QA agent.
	log.Debug("==> Connecting users via QA agent")
	err = qaClient1.ConnectUserUnicast_AnyDevice_NoWait(ctx)
	require.NoError(t, err)
	err = qaClient2.ConnectUserUnicast_AnyDevice_NoWait(ctx)
	require.NoError(t, err)
	log.Debug("--> Users connected via QA agent")

	// Wait for status up on both clients.
	log.Debug("==> Waiting for status up")
	err = qaClient1.WaitForStatusUp(ctx)
	require.NoError(t, err)
	err = qaClient2.WaitForStatusUp(ctx)
	require.NoError(t, err)
	log.Debug("--> Status is up on both clients")

	// Wait for cross-exchange routes. Clients on different exchanges should have routes
	// to each other. Clients on the same exchange do NOT have routes to each other.
	log.Debug("==> Waiting for cross-exchange routes")
	qaClients := []*qa.Client{qaClient1, qaClient2}
	for _, c := range qaClients {
		device, err := c.GetCurrentDevice(ctx)
		require.NoError(t, err)
		err = c.WaitForRoutes(ctx, qa.MapFilter(qaClients, func(other *qa.Client) (net.IP, bool) {
			// Skip self (by pointer identity, not hostname, since all E2E clients
			// share the same external host address).
			if other == c {
				return nil, false
			}
			otherDevice, err := other.GetCurrentDevice(ctx)
			if err != nil {
				return nil, false
			}
			if otherDevice.ExchangeCode == device.ExchangeCode {
				return nil, false
			}
			return other.PublicIP(), true
		}))
		require.NoError(t, err)
	}
	log.Debug("--> Cross-exchange routes installed")

	// Test ping connectivity between clients.
	log.Debug("==> Testing unicast connectivity")
	_, err = qaClient1.TestUnicastConnectivity(t, ctx, qaClient2, nil, nil)
	require.NoError(t, err)
	_, err = qaClient2.TestUnicastConnectivity(t, ctx, qaClient1, nil, nil)
	require.NoError(t, err)
	log.Debug("--> Unicast connectivity verified")

	// Disconnect both clients. We skip waiting for status and deletion since BGP
	// teardown can exceed the 90s timeout under QEMU emulation. The containers are
	// cleaned up by Ryuk regardless.
	log.Debug("==> Disconnecting users")
	err = qaClient1.DisconnectUser(ctx, false, false)
	require.NoError(t, err)
	err = qaClient2.DisconnectUser(ctx, false, false)
	require.NoError(t, err)
	log.Debug("--> Users disconnected")
}
