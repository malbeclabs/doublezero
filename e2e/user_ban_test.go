//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

func TestE2E_UserBan(t *testing.T) {
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

	linkNetwork := devnet.NewMiscNetwork(dn, log, "la2-dz01:ny5-dz01")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)

	// Add both devices in parallel (ME1 optimization).
	deviceCode1 := "la2-dz01"
	deviceCode2 := "ny5-dz01"
	var devicePK1, devicePK2 string

	g := new(errgroup.Group)
	g.Go(func() error {
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
		if err != nil {
			return fmt.Errorf("add device1 (%s): %w", deviceCode1, err)
		}
		devicePK1 = device1.ID
		log.Debug("--> Device1 added", "deviceCode", deviceCode1, "devicePK", devicePK1)
		return nil
	})

	g.Go(func() error {
		// Add ny5-dz01 device in xewr exchange.
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
		if err != nil {
			return fmt.Errorf("add device2 (%s): %w", deviceCode2, err)
		}
		devicePK2 = device2.ID
		log.Debug("--> Device2 added", "deviceCode", deviceCode2, "devicePK", devicePK2)
		return nil
	})

	// Wait for both devices to be added.
	require.NoError(t, g.Wait())

	// Wait for devices to exist onchain.
	log.Debug("==> Waiting for devices to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			log.Error("failed to get program data", "error", err)
			return false
		}
		return len(data.Devices) == 2
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Devices exist onchain", "deviceCode1", deviceCode1, "devicePK1", devicePK1, "deviceCode2", deviceCode2, "devicePK2", devicePK2)

	// Create WAN link between devices.
	log.Debug("==> Creating link onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero link create wan --code \"la2-dz01:ny5-dz01\" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ny5-dz01 --side-z-interface Ethernet2 --bandwidth \"10 Gbps\" --mtu 2048 --delay-ms 40 --jitter-ms 3 --desired-status activated"})
	require.NoError(t, err)
	log.Debug("--> Link created onchain")

	// Add a client.
	log.Debug("==> Adding client1")
	client1, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Debug("--> Client1 added", "client1Pubkey", client1.Pubkey, "client1IP", client1.CYOANetworkIP)

	// Add another client.
	log.Debug("==> Adding client2")
	client2, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
	})
	require.NoError(t, err)
	log.Debug("--> Client2 added", "client2Pubkey", client2.Pubkey, "client2IP", client2.CYOANetworkIP)

	// Add third client to test intra-exchange routing policy.
	log.Debug("==> Adding client3")
	client3, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 120,
	})
	require.NoError(t, err)
	log.Debug("--> Client3 added", "client3Pubkey", client3.Pubkey, "client3IP", client3.CYOANetworkIP)

	// Wait for client latency results in parallel (QW1 optimization).
	log.Debug("==> Waiting for client latency results (parallel)")
	g = new(errgroup.Group)
	g.Go(func() error {
		return client1.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	})
	g.Go(func() error {
		return client2.WaitForLatencyResults(t.Context(), devicePK2, 90*time.Second)
	})
	g.Go(func() error {
		return client3.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	})
	require.NoError(t, g.Wait())
	log.Debug("--> Finished waiting for client latency results")

	// Add clients to user Access Pass so they can open user connections.
	log.Debug("==> Adding clients to Access Pass")
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client1.CYOANetworkIP + " --user-payer " + client1.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client2.CYOANetworkIP + " --user-payer " + client2.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client3.CYOANetworkIP + " --user-payer " + client3.Pubkey})
	require.NoError(t, err)
	log.Debug("--> Clients added to user Access Pass")

	// Run IBRL workflow test.
	if !t.Run("user-ban-ibrl", func(t *testing.T) {
		runUserBanIBRLWorkflowTest(t, log, client1, client2, client3, dn, deviceCode1, deviceCode2)
	}) {
		t.Fail()
	}
}

func runUserBanIBRLWorkflowTest(t *testing.T, log *slog.Logger, client1 *devnet.Client, client2 *devnet.Client, client3 *devnet.Client, dn *devnet.Devnet, deviceCode1 string, deviceCode2 string) {
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
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")

	// Connect all three clients in IBRL mode in parallel (QW3 optimization).
	log.Debug("==> Connecting all clients in IBRL mode (parallel)")
	g := new(errgroup.Group)
	g.Go(func() error {
		// Connect client1 in IBRL mode to device1 (xlax exchange).
		_, err := client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device", deviceCode1})
		if err != nil {
			return fmt.Errorf("connect client1: %w", err)
		}
		if err := client1.WaitForTunnelUp(t.Context(), 90*time.Second); err != nil {
			return fmt.Errorf("wait for client1 tunnel up: %w", err)
		}
		log.Debug("--> Client1 connected in IBRL mode to device1")
		return nil
	})
	g.Go(func() error {
		// Connect client2 in IBRL mode to device2 (xewr exchange).
		_, err := client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--device", deviceCode2})
		if err != nil {
			return fmt.Errorf("connect client2: %w", err)
		}
		if err := client2.WaitForTunnelUp(t.Context(), 90*time.Second); err != nil {
			return fmt.Errorf("wait for client2 tunnel up: %w", err)
		}
		log.Debug("--> Client2 connected in IBRL mode to device2")
		return nil
	})
	g.Go(func() error {
		// Connect client3 in IBRL mode to device1 (xlax exchange, same as client1).
		_, err := client3.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client3.CYOANetworkIP, "--device", deviceCode1})
		if err != nil {
			return fmt.Errorf("connect client3: %w", err)
		}
		if err := client3.WaitForTunnelUp(t.Context(), 90*time.Second); err != nil {
			return fmt.Errorf("wait for client3 tunnel up: %w", err)
		}
		log.Debug("--> Client3 connected in IBRL mode to device1")
		return nil
	})
	require.NoError(t, g.Wait())
	log.Debug("--> All clients connected in IBRL mode")

	// Wait for cross-exchange routes to propagate via iBGP between devices (QW4 optimization: parallel).
	// Device1 (xlax) should have client2's route (from device2 via iBGP),
	// and device2 (xewr) should have client1's route (from device1 via iBGP).
	log.Debug("==> Waiting for cross-exchange routes to propagate via iBGP (parallel)")
	g = new(errgroup.Group)
	g.Go(func() error {
		return waitForCondition(t.Context(), 90*time.Second, 1*time.Second, func() (bool, error) {
			output, err := dn.Devices[deviceCode1].Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf("Cli -c \"show ip route vrf vrf1 %s/32\"", client2.CYOANetworkIP)})
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(output), client2.CYOANetworkIP), nil
		}, "device1 should have route to client2 via iBGP")
	})
	g.Go(func() error {
		return waitForCondition(t.Context(), 90*time.Second, 1*time.Second, func() (bool, error) {
			output, err := dn.Devices[deviceCode2].Exec(t.Context(), []string{"bash", "-c", fmt.Sprintf("Cli -c \"show ip route vrf vrf1 %s/32\"", client1.CYOANetworkIP)})
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(output), client1.CYOANetworkIP), nil
		}, "device2 should have route to client1 via iBGP")
	})
	require.NoError(t, g.Wait())
	log.Debug("--> Cross-exchange routes have propagated via iBGP")

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
	log.Debug("--> Clients have a DZ IP as public IP when not configured to use an allocated IP")

	// Check that client1 and client3 do not have routes to each other (same exchange - xlax) (QW5 optimization: parallel).
	log.Debug("==> Checking that client1 and client3 do not have routes to each other (intra-exchange routing policy, parallel)")
	g = new(errgroup.Group)
	g.Go(func() error {
		return waitForCondition(t.Context(), 30*time.Second, 1*time.Second, func() (bool, error) {
			output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(output), client3.CYOANetworkIP), nil
		}, "client1 should not have route to client3 (both in xlax exchange)")
	})
	g.Go(func() error {
		return waitForCondition(t.Context(), 30*time.Second, 1*time.Second, func() (bool, error) {
			output, err := client3.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(output), client1.CYOANetworkIP), nil
		}, "client3 should not have route to client1 (both in xlax exchange)")
	})
	require.NoError(t, g.Wait())
	log.Debug("--> Confirmed client1 and client3 do not have routes to each other (intra-exchange routing policy working)")

	// Check that client1 and client2 have routes to each other (cross-exchange) (QW5 optimization: parallel).
	// Even though iBGP propagation between devices is confirmed above, the route still
	// needs to propagate from the device to the client daemon and into the kernel routing
	// table. On resource-constrained CI this can take over 60s.
	log.Debug("==> Checking that client1 and client2 have routes to each other (cross-exchange, parallel)")
	g = new(errgroup.Group)
	g.Go(func() error {
		return waitForCondition(t.Context(), 120*time.Second, 5*time.Second, func() (bool, error) {
			output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(output), client2.CYOANetworkIP), nil
		}, "client1 should have route to client2 (different exchanges)")
	})
	g.Go(func() error {
		return waitForCondition(t.Context(), 120*time.Second, 5*time.Second, func() (bool, error) {
			output, err := client2.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(output), client1.CYOANetworkIP), nil
		}, "client2 should have route to client1 (different exchanges)")
	})
	require.NoError(t, g.Wait())
	log.Debug("--> Confirmed client1 and client2 have routes to each other (cross-exchange)")

	// Ban client1.
	user_pk := getUserAccountPk(t, log, client1, dn)
	log.Debug("==> Banning client1")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "request-ban", "--pubkey", user_pk})
	require.NoError(t, err)
	log.Debug("--> Client1 banned")

	waitForUserBanned(t, log, dn, user_pk, 60*time.Second)

	log.Debug("==> Checking that client1 does not have route to client2 after being banned")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return !strings.Contains(string(output), client2DZIP)
	}, 120*time.Second, 2*time.Second, "timeout waiting for client1's route to client2 to be withdrawn after banning")
	log.Debug("--> Client1 does not have route to client2 after being banned")

	// Unban by deleting the user account.
	log.Debug("==> Unbanning client1")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "delete", "--pubkey", user_pk})
	require.NoError(t, err)

	waitForUserDeleted(t, log, dn, user_pk, 60*time.Second)

	// Disconnect client1.
	log.Debug("==> Disconnecting client1 from IBRL")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client1 disconnected from IBRL")

	// Connect client1 in IBRL mode to device1 (xlax exchange).
	log.Debug("==> Connecting client1 in IBRL mode to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client1 connected in IBRL mode to device1")

	// Check that the clients have a DZ IP equal to their client IP when not configured to use an allocated IP.
	log.Debug("==> Checking that the client1 has a DZ IP as public IP when not configured to use an allocated IP")
	status, err = client1.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client1DZIP = status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client1.CYOANetworkIP, client1DZIP)
	log.Debug("--> Client1 has a DZ IP as public IP when not configured to use an allocated IP")

	// Check that client1 has route to client2 again after unbanning.
	log.Debug("==> Checking that client1 has route to client2 after unbanning")
	require.Eventually(t, func() bool {
		output, err := client1.Exec(t.Context(), []string{"ip", "r", "list", "dev", "doublezero0"})
		if err != nil {
			return false
		}
		return strings.Contains(string(output), client2DZIP)
	}, 120*time.Second, 2*time.Second, "timeout waiting for client1 to receive route to client2 after unbanning")
	log.Debug("--> Client1 has route to client2 after unbanning")

	// Disconnect all clients and wait for disconnection in parallel (QW6 optimization).
	log.Debug("==> Disconnecting all clients from IBRL and waiting for disconnection (parallel)")
	g = new(errgroup.Group)
	g.Go(func() error {
		if _, err := client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP}); err != nil {
			return fmt.Errorf("disconnect client1: %w", err)
		}
		log.Debug("--> Client1 disconnected from IBRL")
		if err := client1.WaitForTunnelDisconnected(t.Context(), 60*time.Second); err != nil {
			return fmt.Errorf("wait for client1 tunnel disconnected: %w", err)
		}
		status, err := client1.GetTunnelStatus(t.Context())
		if err != nil {
			return fmt.Errorf("get client1 tunnel status: %w", err)
		}
		if len(status) != 1 {
			return fmt.Errorf("expected 1 tunnel status for client1, got %d: %v", len(status), status)
		}
		if status[0].DoubleZeroIP != nil {
			return fmt.Errorf("expected nil DoubleZeroIP for client1, got %v", status[0].DoubleZeroIP)
		}
		return nil
	})
	g.Go(func() error {
		if _, err := client2.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client2.CYOANetworkIP}); err != nil {
			return fmt.Errorf("disconnect client2: %w", err)
		}
		log.Debug("--> Client2 disconnected from IBRL")
		if err := client2.WaitForTunnelDisconnected(t.Context(), 60*time.Second); err != nil {
			return fmt.Errorf("wait for client2 tunnel disconnected: %w", err)
		}
		status, err := client2.GetTunnelStatus(t.Context())
		if err != nil {
			return fmt.Errorf("get client2 tunnel status: %w", err)
		}
		if len(status) != 1 {
			return fmt.Errorf("expected 1 tunnel status for client2, got %d: %v", len(status), status)
		}
		if status[0].DoubleZeroIP != nil {
			return fmt.Errorf("expected nil DoubleZeroIP for client2, got %v", status[0].DoubleZeroIP)
		}
		return nil
	})
	g.Go(func() error {
		if _, err := client3.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client3.CYOANetworkIP}); err != nil {
			return fmt.Errorf("disconnect client3: %w", err)
		}
		log.Debug("--> Client3 disconnected from IBRL")
		if err := client3.WaitForTunnelDisconnected(t.Context(), 60*time.Second); err != nil {
			return fmt.Errorf("wait for client3 tunnel disconnected: %w", err)
		}
		status, err := client3.GetTunnelStatus(t.Context())
		if err != nil {
			return fmt.Errorf("get client3 tunnel status: %w", err)
		}
		if len(status) != 1 {
			return fmt.Errorf("expected 1 tunnel status for client3, got %d: %v", len(status), status)
		}
		if status[0].DoubleZeroIP != nil {
			return fmt.Errorf("expected nil DoubleZeroIP for client3, got %v", status[0].DoubleZeroIP)
		}
		return nil
	})
	require.NoError(t, g.Wait())
	log.Debug("--> Confirmed clients are disconnected and do not have a DZ IP allocated")
}

func getUserAccountPk(t *testing.T, log *slog.Logger, client *devnet.Client, dn *devnet.Devnet) string {
	log.Debug("==> Getting user account pk for client", "clientIP", client.CYOANetworkIP)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	data, err := serviceabilityClient.GetProgramData(t.Context())
	require.NoError(t, err)
	for pubkey, user := range data.Users {
		if bytesIPToString(user.ClientIp) == client.CYOANetworkIP {
			log.Debug("Found user account", "userAccountPk", pubkey)
			return base58.Encode(user.PubKey[:])
		}
	}
	require.FailNow(t, "could not find user account")
	return ""
}

func bytesIPToString(b [4]byte) string {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	return ip.String()
}

func waitForUserBanned(t *testing.T, log *slog.Logger, dn *devnet.Devnet, user_pk string, timeout time.Duration) {
	log.Debug("==> Waiting for user to be banned", "user_pk", user_pk)
	userPkBytes, err := base58.Decode(user_pk)
	require.NoError(t, err)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			log.Error("failed to get program data while waiting for user ban", "error", err)
			return false
		}
		for _, user := range data.Users {
			if bytes.Equal(user.PubKey[:], userPkBytes) {
				log.Debug("Checking user status", slog.Any("user.Status", user.Status), slog.Any("expected_banned_value", serviceability.UserStatusBanned))
				return user.Status == serviceability.UserStatusBanned
			}
		}
		log.Error("User not found in program data", "user_pk", user_pk, "users", data.Users)
		return false
	}, timeout, 2*time.Second, "timeout waiting for user banned")
}

func waitForUserDeleted(t *testing.T, log *slog.Logger, dn *devnet.Devnet, user_pk string, timeout time.Duration) {
	log.Debug("==> Waiting for user to be deleted", "user_pk", user_pk)
	userPkBytes, err := base58.Decode(user_pk)
	require.NoError(t, err)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			log.Error("failed to get program data while waiting for user delete", "error", err)
			return false
		}
		for _, user := range data.Users {
			if bytes.Equal(user.PubKey[:], userPkBytes) {
				return false
			}
		}
		return true
	}, timeout, 2*time.Second, "timeout waiting for user deleted")
}

// waitForCondition polls a condition function at the given interval until it returns true
// or the timeout is reached. It returns an error if the timeout expires or if the condition
// returns a non-nil error. This is a goroutine-safe alternative to require.Eventually.
func waitForCondition(ctx context.Context, timeout time.Duration, interval time.Duration, condition func() (bool, error), msg string) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		ok, err := condition()
		if err != nil {
			return fmt.Errorf("%s: %w", msg, err)
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context cancelled: %w", msg, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("%s: timed out after %s", msg, timeout)
			}
		}
	}
}
