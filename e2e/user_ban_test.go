//go:build e2e

package e2e_test

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

func TestE2E_UserBan(t *testing.T) {
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
	log.Info("--> Clients added to user Access Pass")

	// Run IBRL workflow test.
	if !t.Run("user-ban-ibrl", func(t *testing.T) {
		runUserBanIBRLWorkflowTest(t, log, client1, client2, dn, deviceCode1, deviceCode2)
	}) {
		t.Fail()
	}
}

func runUserBanIBRLWorkflowTest(t *testing.T, log *slog.Logger, client1 *devnet.Client, client2 *devnet.Client, dn *devnet.Devnet, deviceCode1 string, deviceCode2 string) {
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
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device-code", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode to device1")

	// Connect client2 in IBRL mode to device2 (xewr exchange).
	log.Info("==> Connecting client2 in IBRL mode to device2")
	_, err = client2.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client2.CYOANetworkIP, "--device-code", deviceCode2})
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

	// Check that the clients can reach each other via their DZ IPs, via ping.
	log.Info("==> Checking that the clients can reach each other via their DZ IPs")
	_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"})
	require.NoError(t, err)
	_, err = client2.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client1DZIP, "-W", "1"})
	require.NoError(t, err)
	log.Info("--> Clients can reach each other via their DZ IPs")

	// Ban client1.
	user_pk := getUserAccountPk(t, log, client1, dn)
	log.Info("==> Banning client1")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "request-ban", "--pubkey", user_pk})
	require.NoError(t, err)
	log.Info("--> Client1 banned")

	waitForUserBanned(t, log, dn, user_pk, 60*time.Second)

	log.Info("==> Checking that the clients cannot reach each other via their DZ IPs")
	require.Eventually(t, func() bool {
		_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"})
		if err == nil {
			log.Info("Ping of client2 successful")
		}
		return err != nil
	}, 120*time.Second, 5*time.Second, "timeout waiting for client1 to be banned by the controller/agent")
	log.Info("--> Client1 cannot reach client2 via DZ IP after being banned")

	// Unban by deleting the user account.
	log.Info("==> Unbanning client1")
	_, err = dn.Manager.Exec(t.Context(), []string{"doublezero", "user", "delete", "--pubkey", user_pk})
	require.NoError(t, err)

	waitForUserDeleted(t, log, dn, user_pk, 60*time.Second)

	// Disconnect client1.
	log.Info("==> Disconnecting client1 from IBRL")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client1.CYOANetworkIP})
	require.NoError(t, err)
	log.Info("--> Client1 disconnected from IBRL")

	// Connect client1 in IBRL mode to device1 (xlax exchange).
	log.Info("==> Connecting client1 in IBRL mode to device1")
	_, err = client1.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client1.CYOANetworkIP, "--device-code", deviceCode1})
	require.NoError(t, err)
	err = client1.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Info("--> Client1 connected in IBRL mode to device1")

	// Check that the clients have a DZ IP equal to their client IP when not configured to use an allocated IP.
	log.Info("==> Checking that the client1 has a DZ IP as public IP when not configured to use an allocated IP")
	status, err = client1.GetTunnelStatus(t.Context())
	require.Len(t, status, 1)
	client1DZIP = status[0].DoubleZeroIP.String()
	require.NoError(t, err)
	require.Equal(t, client1.CYOANetworkIP, client1DZIP)
	log.Info("--> Client1 has a DZ IP as public IP when not configured to use an allocated IP")

	// Check that the clients can reach each other again via their DZ IPs, via ping.
	log.Info("==> Checking that the client1 can reach client2 via their DZ IPs")
	require.Eventually(t, func() bool {
		_, err = client1.Exec(t.Context(), []string{"ping", "-I", "doublezero0", "-c", "3", client2DZIP, "-W", "1"})
		if err == nil {
			return true
		}
		return false
	}, 120*time.Second, 5*time.Second, "timeout waiting for client1 to ping client2 after unbanning")
	log.Info("--> Client1 can reach client2 via their DZ IPs")

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

func getUserAccountPk(t *testing.T, log *slog.Logger, client *devnet.Client, dn *devnet.Devnet) string {
	log.Info("==> Getting user account pk for client", "clientIP", client.CYOANetworkIP)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	data, err := serviceabilityClient.GetProgramData(t.Context())
	require.NoError(t, err)
	for pubkey, user := range data.Users {
		if bytesIPToString(user.ClientIp) == client.CYOANetworkIP {
			log.Info("Found user account", "userAccountPk", pubkey)
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
	log.Info("==> Waiting for user to be banned", "user_pk", user_pk)
	userPkBytes, err := base58.Decode(user_pk)
	require.NoError(t, err)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		for _, user := range data.Users {
			if bytes.Equal(user.PubKey[:], userPkBytes) {
				log.Info("Checking user status", slog.Any("user.Status", user.Status), slog.Any("expected_banned_value", serviceability.UserStatusBanned))
				return user.Status == serviceability.UserStatusBanned
			}
		}
		log.Error("User not found in program data", "user_pk", user_pk, "users", data.Users)
		return false
	}, timeout, 5*time.Second, "timeout waiting for user banned")
}

func waitForUserDeleted(t *testing.T, log *slog.Logger, dn *devnet.Devnet, user_pk string, timeout time.Duration) {
	log.Info("==> Waiting for user to be deleted", "user_pk", user_pk)
	userPkBytes, err := base58.Decode(user_pk)
	require.NoError(t, err)
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		for _, user := range data.Users {
			if bytes.Equal(user.PubKey[:], userPkBytes) {
				return false
			}
		}
		return true
	}, timeout, 5*time.Second, "timeout waiting for user deleted")
}
