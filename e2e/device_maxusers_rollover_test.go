//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

// Test that if the client's nearest device is full, the client will connect to the next nearest device instead
func TestE2E_DeviceMaxusersRollover(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t).With("test", t.Name(), "deployID", deployID)

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

	// Add nearby-dzd1 device.
	deviceCode1 := "nearby-dzd1"
	device1, err1 := dn.AddDevice(t.Context(), devnet.DeviceSpec{
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
	require.NoError(t, err1)
	devicePK1 := device1.ID
	log.Debug("--> Device added", "deviceCode", deviceCode1, "devicePK", devicePK1)

	// Add faraway-dzd1 device.
	deviceCode2 := "faraway-dzd1"
	device2, err2 := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     deviceCode2,
		Location: "lax",
		Exchange: "xlax",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          16,
		CYOANetworkAllocatablePrefix: 29,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
	})
	require.NoError(t, err2)
	devicePK2 := device2.ID
	log.Debug("--> Device added", "deviceCode", deviceCode2, "devicePK", devicePK2)

	// Wait for devices to exist onchain.
	log.Debug("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 2
	}, 30*time.Second, 1*time.Second)
	log.Debug("--> Device exists onchain", "deviceCode1", deviceCode1, "devicePK1", devicePK1, "deviceCode2", deviceCode2, "devicePK2", devicePK2)

	// Add a client.
	log.Debug("==> Adding client")
	client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)
	log.Debug("--> Client added", "clientPubkey", client.Pubkey, "clientIP", client.CYOANetworkIP)

	// Add latency to client to make sure it prefers device 1
	// First, detect which interface has the CYOA network (9.x.x.x) since Docker may assign it to eth0 or eth1
	output, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -o addr show | grep -E 'inet 9\\.' | awk '{print $2}'"})
	require.NoError(t, err)
	cyoaInterface := strings.TrimSpace(string(output))
	require.NotEmpty(t, cyoaInterface, "could not find interface with CYOA network")
	log.Debug("Detected CYOA network interface", "interface", cyoaInterface)

	// Apply tc rules to the correct interface
	for _, command := range [][]string{
		{"bash", "-c", "tc qdisc add dev " + cyoaInterface + " root handle 1: prio bands 3"},
		{"bash", "-c", "tc qdisc add dev " + cyoaInterface + " parent 1:1 handle 10: netem delay 0ms"},
		{"bash", "-c", "tc qdisc add dev " + cyoaInterface + " parent 1:2 handle 20: netem delay 10ms"},
		{"bash", "-c", "tc qdisc add dev " + cyoaInterface + " parent 1:3 handle 30: sfq"},
		{"bash", "-c", "tc filter add dev " + cyoaInterface + " protocol ip parent 1:0 prio 1 u32 match ip dst " + device1.CYOANetworkIP + "/32 flowid 1:1"},
		{"bash", "-c", "tc filter add dev " + cyoaInterface + " protocol ip parent 1:0 prio 2 u32 match ip dst " + device2.CYOANetworkIP + "/32 flowid 1:2"},
		{"bash", "-c", "tc filter add dev " + cyoaInterface + " protocol ip parent 1:0 prio 3 u32 match ip dst 0.0.0.0/0 flowid 1:3"},
	} {
		_, err = client.Exec(t.Context(), command)
		require.NoError(t, err)
	}

	log.Debug("--> Client added to user allowlist")

	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Wait for client latency results.
	log.Debug("==> Waiting for client latency results")
	err = client.WaitForLatencyResults(t.Context(), devicePK1, 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Finished waiting for client latency results")

	// Connect client in IBRL mode.
	log.Debug("==> Connecting client in IBRL mode")
	_, err = client.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client.CYOANetworkIP})
	require.NoError(t, err)
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client connected in IBRL mode")

	// Verify that device has connected to the 1st device
	log.Debug("--> Verify that client has connected to nearby-dzd1")
	cmdOutput, err2 := dn.Manager.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero user list --json | jq -r '.[].device_name'",
	})
	require.NoError(t, err2)
	deviceName := strings.TrimSpace(string(cmdOutput))
	log.Debug("--> Client connected to device " + deviceName)
	if deviceName != "nearby-dzd1" {
		t.Fatalf("client should have connected to nearby-dzd1 but connected to %s instead", deviceName)
	}

	// Disconnect client.
	log.Debug("==> Disconnecting client from IBRL")
	_, err = client.Exec(t.Context(), []string{"doublezero", "disconnect", "--client-ip", client.CYOANetworkIP})
	require.NoError(t, err)
	log.Debug("--> Client disconnected from IBRL")

	// Mark first device full by setting max
	log.Debug("==> Marking first device as full by setting max-users to 0")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero device update --pubkey " + devicePK1 + " --max-users 0"})
	require.NoError(t, err)

	// Connect client in IBRL mode.
	log.Debug("==> Connecting client in IBRL mode")
	_, err = client.Exec(t.Context(), []string{"doublezero", "connect", "ibrl", "--client-ip", client.CYOANetworkIP})
	require.NoError(t, err)
	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)
	log.Debug("--> Client connected in IBRL mode")

	// Verify that the client has connected to the 2nd device
	log.Debug("--> Verify that client has connected to the faraway-dzd1")
	cmdOutput, err2 = dn.Manager.Exec(t.Context(), []string{
		"bash", "-c",
		"doublezero user list --json | jq -r '.[].device_name'",
	})
	require.NoError(t, err2)
	deviceName = strings.TrimSpace(string(cmdOutput))
	log.Debug("--> Client connected to device " + deviceName)
	if deviceName != "faraway-dzd1" {
		t.Fatalf("client should have connected to faraway-dzd1 but connected to %s instead", deviceName)
	}

}
