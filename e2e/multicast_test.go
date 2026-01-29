//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	controllerconfig "github.com/malbeclabs/doublezero/controlplane/controller/config"
	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_Multicast(t *testing.T) {
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

	tdn := &TestDevnet{
		Devnet: dn,
		log:    log,
	}

	log.Info("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Info("--> Devnet started")

	// Create a dummy device first to maintain same ordering of devices as before.
	err = dn.CreateDeviceOnchain(t.Context(), "la2-dz01", "lax", "xlax", "207.45.216.134", []string{"207.45.216.136/30", "200.12.12.12/29"}, "mgmt")
	require.NoError(t, err)

	// Add a device to the devnet and onchain.
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:     "ny5-dz01",
		Location: "ewr",
		Exchange: "xewr",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)

	// Add other devices and links onchain.
	log.Info("==> Creating other devices and links onchain")
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Populate device information onchain"
		doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.80/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.96/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.112/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.120/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29" --mgmt-vrf mgmt --desired-status activated

		echo "--> Device information onchain:"
		doublezero device list

		echo "==> Populate device interface information onchain"
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Vlan4001" -w
		doublezero device interface create ny5-dz01 "Ethernet4" -w
		doublezero device interface create ny5-dz01 "Ethernet5" -w
		doublezero device interface create ny5-dz01 "Ethernet6" -w
		doublezero device interface create la2-dz01 "Ethernet2" -w
		doublezero device interface create la2-dz01 "Ethernet3" -w
		doublezero device interface create la2-dz01 "Ethernet4" -w
		doublezero device interface create la2-dz01 "Ethernet5" -w
		doublezero device interface create la2-dz01 "Ethernet6" -w
		doublezero device interface create ld4-dz01 "Vlan4001" -w
		doublezero device interface create ld4-dz01 "Ethernet3" -w
		doublezero device interface create ld4-dz01 "Ethernet4" -w
		doublezero device interface create frk-dz01 "Ethernet2" -w
		doublezero device interface create frk-dz01 "Ethernet3" -w
		doublezero device interface create sg1-dz01 "Ethernet2" -w
		doublezero device interface create sg1-dz01 "Ethernet3" -w
		doublezero device interface create ty2-dz01 "Ethernet2" -w
		doublezero device interface create ty2-dz01 "Ethernet3" -w
		doublezero device interface create pit-dzd01 "Ethernet2" -w
		doublezero device interface create pit-dzd01 "Ethernet3" -w
		doublezero device interface create ams-dz001 "Ethernet2" -w
		doublezero device interface create ams-dz001 "Ethernet3" -w

		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create la2-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ld4-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create frk-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create sg1-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ty2-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create pit-dzd01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ams-dz001 "Loopback255" --loopback-type vpnv4 -w

		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create la2-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ld4-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create frk-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create sg1-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ty2-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create pit-dzd01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ams-dz001 "Loopback256" --loopback-type ipv4 -w

		doublezero device update --pubkey ld4-dz01 --max-users 128
		doublezero device update --pubkey frk-dz01 --max-users 128
		doublezero device update --pubkey sg1-dz01 --max-users 128
		doublezero device update --pubkey ty2-dz01 --max-users 128
		doublezero device update --pubkey pit-dzd01 --max-users 128
		doublezero device update --pubkey ams-dz001 --max-users 128

		echo "==> Populate link information onchain"
		doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ny5-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Vlan4001 --side-z ld4-dz01 --side-z-interface Vlan4001 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet3 --side-z frk-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10 --desired-status activated -w
		doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet4 --side-z sg1-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9 --desired-status activated -w
		doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Ethernet3 --side-z ty2-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7 --desired-status activated -w
		doublezero link create wan --code "ty2-dz01:la2-dz01" --contributor co01 --side-a ty2-dz01 --side-a-interface Ethernet3 --side-z la2-dz01 --side-z-interface Ethernet3 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10 --desired-status activated -w

		doublezero link create wan --code "ny5-dz01:la2-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet4 --side-z la2-dz01 --side-z-interface Ethernet4 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ny5-dz01_e5:la2-dz01_e5" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet5 --side-z la2-dz01 --side-z-interface Ethernet5 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ny5-dz01_e6:la2-dz01_e6" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet6 --side-z la2-dz01 --side-z-interface Ethernet6 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 8 --jitter-ms 3 --desired-status activated -w

		echo "===> Set delay override for ny5-dz01:la2-dz01 link"
		doublezero link update --pubkey "ny5-dz01:la2-dz01" --delay-override-ms 500

		echo "===> Set link status=soft-drained for ny5-dz01_e5:la2-dz01_e5 link"
		doublezero link update --pubkey "ny5-dz01_e5:la2-dz01_e5" --status=soft-drained

		echo "===> Set link status=hard-drained for ny5-dz01_e6:la2-dz01_e6 link"
		doublezero link update --pubkey "ny5-dz01_e6:la2-dz01_e6" --status=hard-drained

		echo "--> Tunnel information onchain:"
		doublezero link list
	`})
	require.NoError(t, err)

	// Add publisher client.
	publisherClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Add subscriber client.
	subscriberClient, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 110,
	})
	require.NoError(t, err)

	// Set up latency routes and wait for results for both clients.
	for _, client := range []*devnet.Client{publisherClient, subscriberClient} {
		_, err = client.Exec(t.Context(), []string{"bash", "-c", `
			echo "==> Adding null routes to test latency selection to ny5-dz01."
			ip rule add priority 1 from ` + client.CYOANetworkIP + `/` + strconv.Itoa(dn.Spec.CYOANetwork.CIDRPrefix) + ` to all table main
			ip route add 207.45.216.134/32 dev lo proto static scope host
			ip route add 195.219.120.72/32 dev lo proto static scope host
			ip route add 195.219.220.88/32 dev lo proto static scope host
			ip route add 180.87.102.104/32 dev lo proto static scope host
			ip route add 180.87.154.112/32 dev lo proto static scope host
			ip route add 204.16.241.243/32 dev lo proto static scope host
			ip route add 195.219.138.50/32 dev lo proto static scope host
		`})
		require.NoError(t, err)
	}

	for _, client := range []*devnet.Client{publisherClient, subscriberClient} {
		err = client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
		require.NoError(t, err)
	}

	// Set access passes for both clients.
	for _, client := range []*devnet.Client{publisherClient, subscriberClient} {
		_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
		require.NoError(t, err)
	}

	// Create both multicast groups.
	createMulticastGroupForBothClients(t, tdn, publisherClient, subscriberClient, "mg01")
	createMulticastGroupForBothClients(t, tdn, publisherClient, subscriberClient, "mg02")

	if !t.Run("connect", func(t *testing.T) {
		// Connect publisher.
		tdn.ConnectMulticastPublisherSkipAccessPass(t, publisherClient, "mg01", "mg02")
		err = publisherClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Connect subscriber.
		tdn.ConnectMulticastSubscriberSkipAccessPass(t, subscriberClient, "mg01", "mg02")
		err = subscriberClient.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		// Check agent config with both users.
		checkMulticastBothUsersAgentConfig(t, tdn, device, publisherClient, subscriberClient)

		// Check publisher post-connect.
		checkMulticastPostConnect(t, log, "publisher", tdn, device, publisherClient)

		// Check subscriber post-connect.
		checkMulticastPostConnect(t, log, "subscriber", tdn, device, subscriberClient)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		tdn.DisconnectMulticastPublisher(t, publisherClient)
		tdn.DisconnectMulticastSubscriber(t, subscriberClient)

		// Check agent config with both users removed.
		checkMulticastBothUsersRemovedAgentConfig(t, tdn, device)

		// Check publisher post-disconnect.
		checkMulticastPostDisconnect(t, log, "publisher", tdn, device, publisherClient)

		// Check subscriber post-disconnect.
		checkMulticastPostDisconnect(t, log, "subscriber", tdn, device, subscriberClient)
	}) {
		t.Fail()
	}
}

// createMulticastGroupForBothClients creates a multicast group and adds both publisher and
// subscriber clients to the allowlists.
func createMulticastGroupForBothClients(t *testing.T, dn *TestDevnet, publisherClient, subscriberClient *devnet.Client, groupCode string) {
	dn.log.Info("==> Creating multicast group onchain", "group", groupCode)

	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -e

		echo "==> Populate multicast group information onchain"
		doublezero multicast group create --code ` + groupCode + ` --max-bandwidth 10Gbps --owner me -w

		echo "--> Multicast group information onchain:"
		doublezero multicast group list

		echo "==> Add manager to multicast group allowlist"
		doublezero multicast group allowlist publisher add --code ` + groupCode + ` --user-payer me --client-ip ` + publisherClient.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code ` + groupCode + ` --user-payer me --client-ip ` + subscriberClient.CYOANetworkIP + `

		echo "==> Add publisher client pubkey to multicast group allowlist"
		doublezero multicast group allowlist publisher add --code ` + groupCode + ` --user-payer ` + publisherClient.Pubkey + ` --client-ip ` + publisherClient.CYOANetworkIP + `

		echo "==> Add subscriber client pubkey to multicast group allowlist"
		doublezero multicast group allowlist subscriber add --code ` + groupCode + ` --user-payer ` + subscriberClient.Pubkey + ` --client-ip ` + subscriberClient.CYOANetworkIP + `
	`})
	require.NoError(t, err)
}

// checkMulticastBothUsersAgentConfig checks that the device agent config has both publisher
// and subscriber tunnels configured.
func checkMulticastBothUsersAgentConfig(t *testing.T, dn *TestDevnet, device *devnet.Device, publisherClient, subscriberClient *devnet.Client) {
	t.Run("wait_for_agent_config_both_users", func(t *testing.T) {
		dzPrefixIP, dzPrefixNet, err := netutil.ParseCIDR(device.DZPrefix)
		require.NoError(t, err)
		ones, _ := dzPrefixNet.Mask.Size()
		allocatableBits := 32 - ones

		// With onchain allocation, the network address itself is not allocatable.
		expectedAllocatedPublisherIP, err := nextAllocatableIP(dzPrefixIP, allocatableBits, map[string]bool{dzPrefixIP: true})
		require.NoError(t, err)

		// The manager consumes tunnel slots when added to allowlists (publisher slot 500,
		// subscriber slot 502), so the actual client tunnels are at 501 and 503.
		pubTunnel := controllerconfig.StartUserTunnelNum + 1
		subTunnel := controllerconfig.StartUserTunnelNum + 3
		pubTunnelDeviceIP := fmt.Sprintf("169.254.0.%d/31", 2*(pubTunnel-controllerconfig.StartUserTunnelNum))
		subTunnelDeviceIP := fmt.Sprintf("169.254.0.%d/31", 2*(subTunnel-controllerconfig.StartUserTunnelNum))
		pubTunnelBGPNeighbor := fmt.Sprintf("169.254.0.%d", 2*(pubTunnel-controllerconfig.StartUserTunnelNum)+1)
		subTunnelBGPNeighbor := fmt.Sprintf("169.254.0.%d", 2*(subTunnel-controllerconfig.StartUserTunnelNum)+1)

		config, err := fixtures.Render("fixtures/multicast/doublezero_agent_config_both_users_added.tmpl", map[string]any{
			"PublisherClientIP":            publisherClient.CYOANetworkIP,
			"SubscriberClientIP":           subscriberClient.CYOANetworkIP,
			"DeviceIP":                     device.CYOANetworkIP,
			"ExpectedAllocatedPublisherIP": expectedAllocatedPublisherIP,
			"PublisherTunnelNum":           pubTunnel,
			"SubscriberTunnelNum":          subTunnel,
			"PublisherTunnelDeviceIP":      pubTunnelDeviceIP,
			"SubscriberTunnelDeviceIP":     subTunnelDeviceIP,
			"PublisherTunnelBGPNeighbor":   pubTunnelBGPNeighbor,
			"SubscriberTunnelBGPNeighbor":  subTunnelBGPNeighbor,
			"StartTunnel":                  controllerconfig.StartUserTunnelNum,
			"EndTunnel":                    controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
		})
		require.NoError(t, err, "error reading agent configuration fixture")
		err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
		require.NoError(t, err, "error waiting for agent config to match")
	})
}

// checkMulticastBothUsersRemovedAgentConfig checks that the device agent config has both
// publisher and subscriber tunnels removed.
func checkMulticastBothUsersRemovedAgentConfig(t *testing.T, dn *TestDevnet, device *devnet.Device) {
	t.Run("wait_for_agent_config_both_users_removed", func(t *testing.T) {
		config, err := fixtures.Render("fixtures/multicast/doublezero_agent_config_both_users_removed.tmpl", map[string]any{
			"DeviceIP":    device.CYOANetworkIP,
			"StartTunnel": controllerconfig.StartUserTunnelNum,
			"EndTunnel":   controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
		})
		require.NoError(t, err, "error reading agent configuration fixture")
		err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
		require.NoError(t, err, "error waiting for agent config to match")
	})
}

// checkMulticastPostConnect checks requirements after connecting a multicast client.
// mode should be "publisher" or "subscriber".
func checkMulticastPostConnect(t *testing.T, log *slog.Logger, mode string, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	t.Run("check_post_connect_"+mode, func(t *testing.T) {
		log.Info("==> Checking multicast post-connect requirements", "mode", mode)

		var expectedAllocatedClientIP string
		if mode == "publisher" {
			dzPrefixIP, dzPrefixNet, err := netutil.ParseCIDR(device.DZPrefix)
			require.NoError(t, err)
			ones, _ := dzPrefixNet.Mask.Size()
			allocatableBits := 32 - ones

			// With onchain allocation, the network address itself is not allocatable.
			expectedAllocatedClientIP, err = nextAllocatableIP(dzPrefixIP, allocatableBits, map[string]bool{dzPrefixIP: true})
			require.NoError(t, err)
		}

		tests := []struct {
			name        string
			fixturePath string
			data        map[string]any
			cmd         []string
		}{
			{
				name:        "doublezero_multicast_group_list",
				fixturePath: "fixtures/multicast/doublezero_multicast_group_list_" + mode + ".tmpl",
				data: map[string]any{
					"ManagerPubkey": dn.Manager.Pubkey,
				},
				cmd: []string{"doublezero", "multicast", "group", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast/doublezero_status_connected_" + mode + ".tmpl",
				data: map[string]any{
					"ClientIP":                  client.CYOANetworkIP,
					"DeviceIP":                  device.CYOANetworkIP,
					"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
				},
				cmd: []string{"doublezero", "status"},
			},
		}

		for _, test := range tests {
			if !t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				got, err := client.Exec(t.Context(), test.cmd)
				require.NoError(t, err, "error executing command on client")

				want, err := fixtures.Render(test.fixturePath, test.data)
				require.NoError(t, err, "error reading fixture")

				diff := fixtures.DiffCLITable(got, []byte(want))
				if diff != "" {
					fmt.Println(string(got))
					t.Fatalf("output mismatch: -(want), +(got):%s", diff)
				}
			}) {
				t.Fail()
			}
		}

		if !t.Run("check_tunnel_interface_is_configured", func(t *testing.T) {
			t.Parallel()

			links, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero1"})
			require.NoError(t, err)

			require.Len(t, links, 1)
			delete(links[0], "ifindex")
			require.Equal(t, map[string]any{
				"link":   nil,
				"ifname": "doublezero1",
				"flags": []any{
					"POINTOPOINT",
					"NOARP",
					"UP",
					"LOWER_UP",
				},
				"mtu":               float64(1476),
				"qdisc":             "noqueue",
				"operstate":         "UNKNOWN",
				"linkmode":          "DEFAULT",
				"group":             "default",
				"link_type":         "gre",
				"address":           client.CYOANetworkIP,
				"link_pointtopoint": true,
				"broadcast":         device.CYOANetworkIP,
			}, links[0])
		}) {
			t.Fail()
		}

		if !t.Run("check_multicast_static_routes", func(t *testing.T) {
			t.Parallel()

			routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
			require.NoError(t, err)

			expectedAddrs := []string{"233.84.178.0", "233.84.178.1"}
			for _, expectedAddr := range expectedAddrs {
				found := false
				for _, route := range routes {
					if dst, ok := route["dst"].(string); ok && dst == expectedAddr {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("multicast route %s/32 not found for %s: %+v", expectedAddr, mode, routes)
				}
			}
		}) {
			t.Fail()
		}

		if !t.Run("check_device_tunnel_interface", func(t *testing.T) {
			t.Parallel()

			pubTunnelNum := controllerconfig.StartUserTunnelNum + 1
			subTunnelNum := controllerconfig.StartUserTunnelNum + 3
			tunnelName := fmt.Sprintf("Tunnel%d", pubTunnelNum)
			if mode == "subscriber" {
				tunnelName = fmt.Sprintf("Tunnel%d", subTunnelNum)
			}

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				ifaces, err := devnet.DeviceExecAristaCliJSON[*arista.ShowInterfaces](t.Context(), device, arista.ShowInterfacesCmd(tunnelName))
				if err != nil {
					time.Sleep(1 * time.Second)
					continue
				}

				iface, ok := ifaces.Interfaces[tunnelName]
				if ok && iface.LineProtocolStatus == "up" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("%s interface not up on device", tunnelName)
		}) {
			t.Fail()
		}

		// Mode-specific checks.
		if mode == "publisher" {
			if !t.Run("check_s_comma_g_is_created", func(t *testing.T) {
				t.Parallel()

				mGroups := []string{"233.84.178.0", "233.84.178.1"}

				for _, mGroup := range mGroups {
					_, _ = client.Exec(t.Context(), []string{"bash", "-c", "ping -c 1 -w 1 " + mGroup}, docker.NoPrintOnError())
				}

				for _, mGroup := range mGroups {
					require.Eventually(t, func() bool {
						mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
						if err != nil {
							dn.log.Debug("Error fetching mroutes from doublezero device", "error", err)
							return false
						}

						groups, ok := mroutes.Groups[mGroup]
						if !ok {
							dn.log.Debug("Waiting for multicast group to be created", "mGroup", mGroup, "mroutes", mroutes)
							return false
						}

						_, ok = groups.GroupSources[expectedAllocatedClientIP]
						if !ok {
							dn.log.Debug("Source not found in multicast group", "source", expectedAllocatedClientIP, "mGroup", mGroup)
							return false
						}

						return true
					}, 5*time.Second, 1*time.Second, "multicast group %s not found in mroutes", mGroup)
				}
			}) {
				t.Fail()
			}
		}

		if mode == "subscriber" {
			if !t.Run("check_pim_join_received", func(t *testing.T) {
				t.Parallel()

				mGroups := []string{"233.84.178.0", "233.84.178.1"}

				for _, mGroup := range mGroups {
					require.Eventually(t, func() bool {
						mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
						if err != nil {
							dn.log.Debug("Error fetching mroutes from doublezero device", "error", err)
							return false
						}

						groups, ok := mroutes.Groups[mGroup]
						if !ok {
							dn.log.Debug("Waiting for multicast group to appear in mroutes", "mGroup", mGroup)
							return false
						}

						groupDetails, ok := groups.GroupSources["0.0.0.0"]
						if !ok {
							dn.log.Debug("Waiting for (*, G) entry", "mGroup", mGroup)
							return false
						}

						subTunnelName := fmt.Sprintf("Tunnel%d", controllerconfig.StartUserTunnelNum+3)
						if !slices.Contains(groupDetails.OIFList, subTunnelName) {
							dn.log.Debug("Waiting for subscriber tunnel in OIFList", "tunnel", subTunnelName, "mGroup", mGroup, "oifList", groupDetails.OIFList)
							return false
						}

						return true
					}, 60*time.Second, 1*time.Second, "PIM join not received for %s", mGroup)
				}
			}) {
				t.Fail()
			}
		}

		if !t.Run("only_one_tunnel_allowed", func(t *testing.T) {
			_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
			require.NoError(t, err)

			_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
			require.Error(t, err, "User with different type already exists. Only one tunnel currently supported")
		}) {
			t.Fail()
		}

		log.Info("--> Multicast post-connect requirements checked", "mode", mode)
	})
}

// checkMulticastPostDisconnect checks requirements after disconnecting a multicast client.
// mode should be "publisher" or "subscriber".
func checkMulticastPostDisconnect(t *testing.T, log *slog.Logger, mode string, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	t.Run("check_post_disconnect_"+mode, func(t *testing.T) {
		log.Info("==> Checking multicast post-disconnect requirements", "mode", mode)

		tests := []struct {
			name        string
			fixturePath string
			data        map[string]any
			cmd         []string
		}{
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast/doublezero_status_disconnected.txt",
				data:        map[string]any{},
				cmd:         []string{"doublezero", "status"},
			},
		}

		for _, test := range tests {
			if !t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				got, err := client.Exec(t.Context(), test.cmd)
				require.NoError(t, err, "error executing command on client")

				want, err := fixtures.Render(test.fixturePath, test.data)
				require.NoError(t, err, "error reading fixture")

				diff := fixtures.DiffCLITable(got, []byte(want))
				if diff != "" {
					fmt.Println(string(got))
					t.Fatalf("output mismatch: -(want), +(got):%s", diff)
				}
			}) {
				t.Fail()
			}
		}

		if !t.Run("check_tunnel_interface_is_removed", func(t *testing.T) {
			t.Parallel()

			got, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero1"}, docker.NoPrintOnError())
			require.Error(t, err)
			require.Equal(t, `Device "doublezero1" does not exist.`, strings.TrimSpace(string(got)), err.Error())
		}) {
			t.Fail()
		}

		if !t.Run("check_multicast_routes_removed", func(t *testing.T) {
			t.Parallel()

			routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
			require.NoError(t, err)

			removedAddrs := []string{"233.84.178.0", "233.84.178.1"}
			for _, addr := range removedAddrs {
				for _, route := range routes {
					if dst, ok := route["dst"].(string); ok && dst == addr {
						t.Fatalf("multicast route %s/32 should be removed after disconnect: found in %+v", addr, routes)
					}
				}
			}
		}) {
			t.Fail()
		}

		if !t.Run("check_device_tunnel_interface_removed", func(t *testing.T) {
			t.Parallel()

			pubTunnelNum := controllerconfig.StartUserTunnelNum + 1
			subTunnelNum := controllerconfig.StartUserTunnelNum + 3
			tunnelName := fmt.Sprintf("Tunnel%d", pubTunnelNum)
			if mode == "subscriber" {
				tunnelName = fmt.Sprintf("Tunnel%d", subTunnelNum)
			}

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				ifaces, err := devnet.DeviceExecAristaCliJSON[*arista.ShowInterfaces](t.Context(), device, arista.ShowInterfacesCmd(tunnelName))
				if err != nil {
					return
				}

				iface, ok := ifaces.Interfaces[tunnelName]
				if !ok || iface.LineProtocolStatus != "up" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("%s interface still up on device after disconnect", tunnelName)
		}) {
			t.Fail()
		}

		if !t.Run("check_user_tunnel_is_removed_from_agent", func(t *testing.T) {
			t.Parallel()

			// Publisher tunnel is slot +1 (169.254.0.3), subscriber is slot +3 (169.254.0.7).
			pubBGPNeighbor := fmt.Sprintf("169.254.0.%d", 2*1+1)
			subBGPNeighbor := fmt.Sprintf("169.254.0.%d", 2*3+1)
			expectedAddr := pubBGPNeighbor
			if mode == "subscriber" {
				expectedAddr = subBGPNeighbor
			}

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd("vrf1"))
				require.NoError(t, err, "error fetching neighbors from doublezero device")

				_, ok := neighbors.VRFs["vrf1"].Peers[expectedAddr]
				if !ok {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("bgp neighbor %s has not been removed from doublezero device", expectedAddr)
		}) {
			t.Fail()
		}

		log.Info("--> Multicast post-disconnect requirements checked", "mode", mode)
	})
}
