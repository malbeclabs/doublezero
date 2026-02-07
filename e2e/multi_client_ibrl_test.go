//go:build e2e

package e2e_test

import (
	"errors"
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

const (
	routeLivenessPort = 44880
)

func TestE2E_MultiClientIBRL(t *testing.T) {
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

	// Add client3.
	log.Debug("==> Adding client3")
	client3, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID:       120,
		RouteLivenessEnableActive: true, //
	})
	require.NoError(t, err)
	log.Debug("--> Client3 added", "client3Pubkey", client3.Pubkey, "client3IP", client3.CYOANetworkIP)

	// Add client4.
	log.Debug("==> Adding client4")
	client4, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID:        130,
		RouteLivenessEnablePassive: false, // route liveness subsystem is disabled for this client
		RouteLivenessEnableActive:  false,
	})
	require.NoError(t, err)
	log.Debug("--> Client4 added", "client4Pubkey", client4.Pubkey, "client4IP", client4.CYOANetworkIP)

	// Wait for client latency results.
	log.Debug("==> Waiting for client latency results")
	err = client1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	err = client3.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	err = client4.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Finished waiting for client latency results")

	log.Debug("==> Add clients to user Access Pass")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client3.CYOANetworkIP + " --user-payer " + client3.Pubkey})
	require.NoError(t, err)
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client4.CYOANetworkIP + " --user-payer " + client4.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Clients added to user Access Pass")

	// Run IBRL workflow test.
	runMultiClientIBRLWorkflowTest(t, log, dn, client1, client2, client3, client4, deviceCode1, deviceCode2)
}

func runMultiClientIBRLWorkflowTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, client1 *devnet.Client, client2 *devnet.Client, client3 *devnet.Client, client4 *devnet.Client, deviceCode1 string, deviceCode2 string) {
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
	status, err = client3.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	status, err = client4.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")

	// Connect client1 in IBRL mode to device1 (xlax exchange).
	log.Debug("==> Connecting client1 in IBRL mode to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device", deviceCode1})
	require.NoError(t, err)
	log.Debug("--> Client1 connected in IBRL mode to device1")

	// Connect client2 in IBRL mode to device2 (xewr exchange).
	log.Debug("==> Connecting client2 in IBRL mode to device2")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	log.Debug("--> Client2 connected in IBRL mode to device2")

	// Connect client3 in IBRL mode to device2 (xewr exchange).
	log.Debug("==> Connecting client3 in IBRL mode to device2")
	_, err = client3.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client3.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	log.Debug("--> Client3 connected in IBRL mode to device2")

	// Connect client4 in IBRL mode to device2 (xewr exchange).
	log.Debug("==> Connecting client4 in IBRL mode to device2")
	_, err = client4.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client4.CYOANetworkIP, "--device", deviceCode2})
	require.NoError(t, err)
	log.Debug("--> Client4 connected in IBRL mode to device2")

	// Wait for all clients to be connected.
	log.Debug("==> Waiting for all clients to be connected")
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = client2.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = client3.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	err = client4.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> All clients connected")

	// Check that the clients have a DZ IP equal to their client IP when not configured to use an allocated IP.
	log.Debug("==> Checking that the clients have a DZ IP as public IP when not configured to use an allocated IP")
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
	status, err = client3.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client3DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client3.CYOANetworkIP, client3DZIP)
	status, err = client4.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client4DZIP := status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client4.CYOANetworkIP, client4DZIP)
	log.Debug("--> Clients have a DZ IP as public IP when not configured to use an allocated IP")

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

	// Client1 (on DZD1) should have routes to client2 (on DZD2) and client3 (on DZD2).
	log.Debug("--> Client1 (on DZD1) should have routes to client2 (on DZD2) and client3 (on DZD2)")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP) && strings.Contains(string(output), client3DZIP)
	}, 60*time.Second, 1*time.Second, "client1 should have route to client2")

	// Client2 (on DZD2) should have routes to client1 (on DZD1) only.
	log.Debug("--> Client2 (on DZD2) should have routes to client1 (on DZD1) only")
	require.Eventually(t, func() bool {
		output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 120*time.Second, 5*time.Second, "client2 should have route to client1")

	// Client3 (on DZD2) should have routes to client1 (on DZD1) only.
	log.Debug("--> Client3 (on DZD2) should have routes to client1 (on DZD1) only")
	require.Eventually(t, func() bool {
		output, err := client3.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 120*time.Second, 5*time.Second, "client3 should have route to client1")

	// Client2 (on DZD2) should not have routes to client3 (on DZD2).
	log.Debug("--> Client2 (on DZD2) should not have routes to client3 (on DZD2)")
	require.Never(t, func() bool {
		output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			t.Logf("error listing routes on client2: %v", err)
			return false
		}
		return strings.Contains(string(output), client3DZIP)
	}, 1*time.Second, 100*time.Millisecond, "client2 should not have route to client3")

	// Client3 (on DZD2) should not have routes to client2 (on DZD2).
	log.Debug("--> Client3 (on DZD2) should not have routes to client2 (on DZD2)")
	require.Never(t, func() bool {
		output, err := client3.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			t.Logf("error listing routes on client3: %v", err)
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 1*time.Second, 100*time.Millisecond, "client3 should not have route to client2")

	// Client4 (on DZD2) should have route to client1 (on DZD1).
	log.Debug("--> Client4 (on DZD2) should have route to client1 (on DZD1)")
	require.Eventually(t, func() bool {
		output, err := client4.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client1DZIP)
	}, 120*time.Second, 5*time.Second, "client4 should have routes to client1")

	log.Debug("--> Clients have routes to each other")

	// Check that the clients can reach each other via their DZ IPs, via ping.
	log.Debug("==> Checking that the clients can reach each other via their DZ IPs")

	// Client1 can reach client2 and client3 over doublezero0 interface.
	_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client3DZIP, "-W", "1"})
	require.NoError(t, err)

	// Client2 can reach client1 over doublezero0 interface.
	_, err = client2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	// Client2 cannot reach client3 over doublezero0 interface.
	_, err = client2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client3DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)

	// Client3 can reach client1 over doublezero0 interface.
	_, err = client3.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	// Client3 cannot reach client2 over doublezero0 interface.
	_, err = client3.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)

	// Client4 cannot reach client1 over doublezero0 interface, since client1 does not have a route to client4 and so replies over eth0/1.
	_, err = client4.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client1DZIP, "-W", "1"}, docker.NoPrintOnError())
	require.Error(t, err)

	// Client1 can reach client2 and client3 without specifying the interface.
	_, err = client1.Exec(t.Context(), []string{"ping", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client1.Exec(t.Context(), []string{"ping", "-c", "3", client3DZIP, "-W", "1"})
	require.NoError(t, err)

	// Client2 can reach client1 and client3 without specifying the interface.
	_, err = client2.Exec(t.Context(), []string{"ping", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client2.Exec(t.Context(), []string{"ping", "-c", "3", client3DZIP, "-W", "1"})
	require.NoError(t, err)

	// Client3 can reach client1 and client2 without specifying the interface.
	_, err = client3.Exec(t.Context(), []string{"ping", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client3.Exec(t.Context(), []string{"ping", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)

	// Client4 can reach client1, client2, and client3 without specifying the interface.
	_, err = client4.Exec(t.Context(), []string{"ping", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client4.Exec(t.Context(), []string{"ping", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client4.Exec(t.Context(), []string{"ping", "-c", "3", client3DZIP, "-W", "1"})
	require.NoError(t, err)

	log.Debug("--> Clients can reach each other via their DZ IPs")
	// --- Route liveness block matrix ---
	log.Debug("==> Route liveness: block each client independently and require expected route behavior")
	const wait = 120 * time.Second
	const tick = 5 * time.Second

	doRouteLivenessBaseline := func() {
		t.Helper()
		// Baseline should already be:
		//   - c1 has routes to c2,c3
		//   - c2 has route to c1, NOT to c3
		//   - c3 has route to c1, NOT to c2
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "baseline c1->c2")
		requireEventuallyRoute(t, client1, client3DZIP, true, wait, tick, "baseline c1->c3")
		requireEventuallyRoute(t, client2, client1DZIP, true, wait, tick, "baseline c2->c1")
		requireEventuallyRoute(t, client2, client3DZIP, false, wait, tick, "baseline c2->c3")
		requireEventuallyRoute(t, client3, client1DZIP, true, wait, tick, "baseline c3->c1")
		requireEventuallyRoute(t, client3, client2DZIP, false, wait, tick, "baseline c3->c2")

		// Baseline liveness packets (dz0 present where peers exist; never on eth0/eth1)
		requireUDPLivenessOnDZ0(t, client1, client2DZIP, "baseline c1 liveness packets -> c2 on dz0")
		requireUDPLivenessOnDZ0(t, client1, client3DZIP, "baseline c1 liveness packets -> c3 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "baseline c2 liveness packets -> c1 on dz0 (disabled = routing-agnostic)")
		requireNoUDPLivenessOnDZ0(t, client2, client3DZIP, "baseline c2 liveness packets -> c3 none")
		requireUDPLivenessOnDZ0(t, client3, client1DZIP, "baseline c3 liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnDZ0(t, client3, client2DZIP, "baseline c3 liveness packets -> c2 none")
		requireNoUDPLivenessOnEth01(t, client1, client2DZIP, "baseline no c1 liveness packets on eth0/1 -> c2")
		requireNoUDPLivenessOnEth01(t, client1, client3DZIP, "baseline no c1 liveness packets on eth0/1 -> c3")
		requireNoUDPLivenessOnEth01(t, client2, client1DZIP, "baseline no c2 liveness packets on eth0/1 -> c1")
		requireNoUDPLivenessOnEth01(t, client3, client1DZIP, "baseline no c3 liveness packets on eth0/1 -> c1")
	}

	doRouteLivenessCaseA := func(pass int) {
		t.Helper()
		log.Debug("==> Route liveness Case A (block client1)", "pass", pass)
		blockUDPLiveness(t, client1)

		// Routes
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "pass %d: block c1: c1->c2 remains")
		requireEventuallyRoute(t, client1, client3DZIP, false, wait, tick, "pass %d: block c1: c1->c3 removed")
		requireEventuallyRoute(t, client1, client4DZIP, false, wait, tick, "pass %d: block c1: c1->c4 removed")
		requireEventuallyRoute(t, client3, client1DZIP, false, wait, tick, "pass %d: block c1: c3->c1 removed")
		requireEventuallyRoute(t, client2, client1DZIP, true, wait, tick, "pass %d: block c1: c2->c1 remains")
		requireEventuallyRoute(t, client2, client3DZIP, false, wait, tick, "pass %d: block c1: c2->c3 remains absent")
		requireEventuallyRoute(t, client3, client2DZIP, false, wait, tick, "pass %d: block c1: c3->c2 remains absent")
		requireEventuallyRoute(t, client4, client1DZIP, true, wait, tick, "pass %d: block c1: c4->c1 remains")

		// Liveness packets on doublezero0, none on eth0/1
		requireUDPLivenessOnDZ0(t, client1, client2DZIP, "pass %d: block c1: no c1 liveness packets -> c2 on dz0")
		requireUDPLivenessOnDZ0(t, client1, client3DZIP, "pass %d: block c1: no c1 liveness packets -> c3 on dz0")
		requireUDPLivenessOnDZ0(t, client3, client1DZIP, "pass %d: block c1: no c3 liveness packets -> c1 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: block c1: c2 still shows liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client2DZIP, "pass %d: block c1: no c1 liveness packets on eth0/1 -> c2")
		requireNoUDPLivenessOnEth01(t, client1, client3DZIP, "pass %d: block c1: no c1 liveness packets on eth0/1 -> c3")
		requireNoUDPLivenessOnEth01(t, client3, client1DZIP, "pass %d: block c1: no c3 liveness packets on eth0/1 -> c1")
		requireNoUDPLivenessOnEth01(t, client2, client1DZIP, "pass %d: block c1: no c2 liveness packets on eth0/1 -> c1")

		unblockUDPLiveness(t, client1)

		// Routes restored
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "pass %d: unblock c1: c1->c2 remains")
		requireEventuallyRoute(t, client1, client3DZIP, true, wait, tick, "pass %d: unblock c1: c1->c3 restored")
		requireEventuallyRoute(t, client3, client1DZIP, true, wait, tick, "pass %d: unblock c1: c3->c1 restored")

		// Liveness packets on dz0; none on eth0/1
		requireUDPLivenessOnDZ0(t, client1, client2DZIP, "pass %d: unblock c1: c1 liveness packets -> c2 on dz0")
		requireUDPLivenessOnDZ0(t, client1, client3DZIP, "pass %d: unblock c1: c1 liveness packets -> c3 on dz0")
		requireUDPLivenessOnDZ0(t, client3, client1DZIP, "pass %d: unblock c1: c3 liveness packets -> c1 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: unblock c1: c2 liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client2DZIP, "pass %d: unblock c1: none on eth0/1 -> c2")
		requireNoUDPLivenessOnEth01(t, client1, client3DZIP, "pass %d: unblock c1: none on eth0/1 -> c3")
	}

	doRouteLivenessCaseB := func(pass int) {
		t.Helper()
		log.Debug("==> Route liveness Case B (block client2)", "pass", pass)
		blockUDPLiveness(t, client2)

		// Routes
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "pass %d: block c2: c1->c2 remains")
		requireEventuallyRoute(t, client2, client1DZIP, true, wait, tick, "pass %d: block c2: c2->c1 remains")
		requireEventuallyRoute(t, client2, client3DZIP, false, wait, tick, "pass %d: block c2: c2->c3 remains absent")
		requireEventuallyRoute(t, client3, client2DZIP, false, wait, tick, "pass %d: block c2: c3->c2 remains absent")
		requireEventuallyRoute(t, client1, client3DZIP, true, wait, tick, "pass %d: block c2: c1->c3 remains")
		requireEventuallyRoute(t, client3, client1DZIP, true, wait, tick, "pass %d: block c2: c3->c1 remains")

		// Liveness packets
		requireUDPLivenessOnDZ0(t, client1, client2DZIP, "pass %d: block c2: c1 liveness packets -> c2 on dz0 (route withdrawn)")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: block c2: c2 still shows liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client2DZIP, "pass %d: block c2: no c1 liveness packets on eth0/1 -> c2")
		requireNoUDPLivenessOnEth01(t, client2, client1DZIP, "pass %d: block c2: no c2 liveness packets on eth0/1 -> c1")

		unblockUDPLiveness(t, client2)

		// Routes restored
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "pass %d: unblock c2: c1->c2 remains")

		// Liveness packets on dz0; none on eth0/1
		requireUDPLivenessOnDZ0(t, client1, client2DZIP, "pass %d: unblock c2: c1 liveness packets -> c2 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: unblock c2: c2 liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client2DZIP, "pass %d: unblock c2: none on eth0/1 -> c2")
	}

	doRouteLivenessCaseC := func(pass int) {
		t.Helper()
		log.Debug("==> Route liveness Case C (block client3)", "pass", pass)
		blockUDPLiveness(t, client3)

		// Routes
		requireEventuallyRoute(t, client1, client3DZIP, false, wait, tick, "pass %d: block c3: c1->c3 removed")
		requireEventuallyRoute(t, client3, client1DZIP, false, wait, tick, "pass %d: block c3: c3->c1 removed")
		requireEventuallyRoute(t, client1, client2DZIP, true, wait, tick, "pass %d: block c3: c1->c2 remains")
		requireEventuallyRoute(t, client2, client1DZIP, true, wait, tick, "pass %d: block c3: c2->c1 remains")
		requireEventuallyRoute(t, client2, client3DZIP, false, wait, tick, "pass %d: block c3: c2->c3 remains absent")
		requireEventuallyRoute(t, client3, client2DZIP, false, wait, tick, "pass %d: block c3: c3->c2 remains absent")

		// Liveness packets
		requireUDPLivenessOnDZ0(t, client1, client3DZIP, "pass %d: block c3: c1 liveness packets -> c3 on dz0")
		requireUDPLivenessOnDZ0(t, client3, client1DZIP, "pass %d: block c3: c3 liveness packets -> c1 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: block c3: c2 still shows liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client3DZIP, "pass %d: block c3: no c1 liveness packets on eth0/1 -> c3")
		requireNoUDPLivenessOnEth01(t, client3, client1DZIP, "pass %d: block c3: no c3 liveness packets on eth0/1 -> c1")
		requireNoUDPLivenessOnEth01(t, client2, client1DZIP, "pass %d: block c3: no c2 liveness packets on eth0/1 -> c1")

		unblockUDPLiveness(t, client3)

		// Routes restored
		requireEventuallyRoute(t, client1, client3DZIP, true, wait, tick, "pass %d: unblock c3: c1->c3 restored")
		requireEventuallyRoute(t, client3, client1DZIP, true, wait, tick, "pass %d: unblock c3: c3->c1 restored")

		// Liveness packets on dz0; none on eth0/1
		requireUDPLivenessOnDZ0(t, client1, client3DZIP, "pass %d: unblock c3: c1 liveness packets -> c3 on dz0")
		requireUDPLivenessOnDZ0(t, client3, client1DZIP, "pass %d: unblock c3: c3 liveness packets -> c1 on dz0")
		requireUDPLivenessOnDZ0(t, client2, client1DZIP, "pass %d: unblock c3: c2 liveness packets -> c1 on dz0")
		requireNoUDPLivenessOnEth01(t, client1, client3DZIP, "pass %d: unblock c3: none on eth0/1 -> c3")
	}

	// Run the route liveness matrix.
	doRouteLivenessBaseline()
	doRouteLivenessCaseA(1)
	doRouteLivenessCaseB(1)
	doRouteLivenessCaseC(1)

	log.Debug("--> Route liveness block matrix complete")

	// Disconnect client1.
	log.Debug("==> Disconnecting client1 from IBRL")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client1 disconnected from IBRL")

	// Disconnect client2.
	log.Debug("==> Disconnecting client2 from IBRL")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client2.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client2 disconnected from IBRL")

	// Disconnect client3.
	log.Debug("==> Disconnecting client3 from IBRL")
	_, err = client3.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client3.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client3 disconnected from IBRL")

	// Disconnect client4.
	log.Debug("==> Disconnecting client4 from IBRL")
	_, err = client4.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client4.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client4 disconnected from IBRL")

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
	err = client3.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	err = client4.WaitForTunnelDisconnected(t.Context(), 60*time.Second)
	require.NoError(t, err)
	status, err = client1.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	status, err = client2.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	status, err = client3.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	status, err = client4.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, status, 1, status)
	require.Nil(t, status[0].DoubleZeroIP, status)
	require.Equal(t, devnet.ClientSessionStatusDisconnected, status[0].DoubleZeroStatus.SessionStatus)
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")
}

func blockUDPLiveness(t *testing.T, c *devnet.Client) {
	t.Helper()
	cmd := []string{"iptables", "-A", "INPUT", "-p", "udp", "--dport", strconv.Itoa(routeLivenessPort), "-j", "DROP"}
	_, err := c.Exec(t.Context(), cmd)
	require.NoError(t, err)
}

func unblockUDPLiveness(t *testing.T, c *devnet.Client) {
	t.Helper()
	cmd := []string{"iptables", "-D", "INPUT", "-p", "udp", "--dport", strconv.Itoa(routeLivenessPort), "-j", "DROP"}
	_, err := c.Exec(t.Context(), cmd)
	require.NoError(t, err)
}

func hasRoute(t *testing.T, from *devnet.Client, ip string) bool {
	t.Helper()
	out, err := from.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
	require.NoError(t, err)
	return strings.Contains(string(out), ip)
}

func requireEventuallyRoute(t *testing.T, from *devnet.Client, ip string, want bool, wait, tick time.Duration, msg string) {
	t.Helper()
	require.Eventually(t, func() bool { return hasRoute(t, from, ip) == want }, wait, tick, msg)
}

func requireUDPLivenessOnDZ0(t *testing.T, c *devnet.Client, host string, msg string) {
	t.Helper()
	n, err := udpLivenessCaptureCount(t, c, "doublezero0", host, 90*time.Second, 1)
	require.NoError(t, err)
	require.True(t, n > 0, msg)
}

func requireNoUDPLivenessOnDZ0(t *testing.T, c *devnet.Client, host string, msg string) {
	t.Helper()
	n, err := udpLivenessCaptureCount(t, c, "doublezero0", host, 1*time.Second, 1)
	require.NoError(t, err)
	require.Equal(t, 0, n, msg)
}

func requireNoUDPLivenessOnEth01(t *testing.T, c *devnet.Client, host string, msg string) {
	t.Helper()
	n, err := udpLivenessCaptureCount(t, c, "eth0", host, 1*time.Second, 1)
	require.NoError(t, err)
	require.Equal(t, 0, n, msg)
	n, err = udpLivenessCaptureCount(t, c, "eth1", host, 1*time.Second, 1)
	require.NoError(t, err)
	require.Equal(t, 0, n, msg)
}

func udpLivenessCaptureCount(t *testing.T, c *devnet.Client, iface string, host string, duration time.Duration, stopAtPacketCount int) (int, error) {
	t.Helper()
	var iargs []string
	// We should only specify one interface at a time, or else the filters will not apply to them all.
	iargs = append(iargs, "-i", iface)
	cmd := fmt.Sprintf(`tshark %s -a duration:%d -a packets:%d -f "udp port %d and host %s and not proto gre"`, strings.Join(iargs, " "), int(duration.Seconds()), stopAtPacketCount, routeLivenessPort, host)
	args := append([]string{"bash", "-lc"}, cmd)
	out, err := c.Exec(t.Context(), args)
	require.NoError(t, err)
	// Expect a line like: "9 packets captured" or "1 packet captured"
	s := string(out)
	for line := range strings.SplitSeq(s, "\n") {
		var idx int
		if strings.Contains(line, " packets captured") {
			idx = strings.LastIndex(line, " packets captured")
		} else if strings.Contains(line, " packet captured") {
			idx = strings.LastIndex(line, " packet captured")
		} else {
			continue
		}
		numStr := strings.TrimSpace(line[:idx])
		n, err := strconv.Atoi(numStr)
		require.NoError(t, err)
		return n, nil
	}
	return 0, errors.New("no capture count found in output")
}
