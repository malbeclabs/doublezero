//go:build e2e

package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	controllerconfig "github.com/malbeclabs/doublezero/controlplane/controller/config"
	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/stretchr/testify/require"
)

func TestE2E_Multicast_Publisher(t *testing.T) {
	t.Parallel()

	dn, device, client := NewSingleDeviceSingleClientTestDevnet(t)

	if !t.Run("connect", func(t *testing.T) {
		// Set access pass first, before creating groups (so allowlist additions are preserved)
		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
		require.NoError(t, err)

		// Create both multicast groups before connecting
		dn.CreateMulticastGroupOnchain(t, client, "mg01")
		dn.CreateMulticastGroupOnchain(t, client, "mg02")

		// Connect to both groups at once (skip access-pass set since we did it above)
		dn.ConnectMulticastPublisherSkipAccessPass(t, client, "mg01", "mg02")

		err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		checkMulticastPublisherPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectMulticastPublisher(t, client)

		checkMulticastPublisherPostDisconnect(t, dn, device, client)
	}) {
		t.Fail()
	}
}

// checkMulticastPublisherPostConnect checks requirements after connecting a multicast publisher.
func checkMulticastPublisherPostConnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		dn.log.Info("==> Checking multicast publisher post-connect requirements")

		// Parse the dz_prefix to get the base IP and prefix length
		// User IPs are allocated from the dz_prefix, not the public IP
		dzPrefixIP, dzPrefixNet, err := netutil.ParseCIDR(device.DZPrefix)
		require.NoError(t, err)
		ones, _ := dzPrefixNet.Mask.Size()
		allocatableBits := 32 - ones // number of host bits

		expectedAllocatedClientIP, err := nextAllocatableIP(dzPrefixIP, allocatableBits, map[string]bool{})
		require.NoError(t, err)

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_publisher/doublezero_agent_config_user_added.tmpl", map[string]any{
				"ClientIP":                  client.CYOANetworkIP,
				"DeviceIP":                  device.CYOANetworkIP,
				"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
				"StartTunnel":               controllerconfig.StartUserTunnelNum,
				"EndTunnel":                 controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
			})
			require.NoError(t, err, "error reading agent configuration fixture")
			err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
			require.NoError(t, err, "error waiting for agent config to match")
		}) {
			t.Fail()
		}

		tests := []struct {
			name        string
			fixturePath string
			data        map[string]any
			cmd         []string
		}{
			{
				name:        "doublezero_multicast_group_list",
				fixturePath: "fixtures/multicast_publisher/doublezero_multicast_group_list.tmpl",
				data: map[string]any{
					"ManagerPubkey": dn.Manager.Pubkey,
				},
				cmd: []string{"doublezero", "multicast", "group", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_publisher/doublezero_status_connected.tmpl",
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

			// Verify static multicast routes are installed for publisher
			routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
			require.NoError(t, err)

			// Check for both multicast group addresses
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
					t.Fatalf("multicast route %s/32 not found for publisher: %+v", expectedAddr, routes)
				}
			}
		}) {
			t.Fail()
		}

		if !t.Run("check_pim_neighbor_formed", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				pim, err := devnet.DeviceExecAristaCliJSON[*arista.ShowPIMNeighbors](t.Context(), device, arista.ShowPIMNeighborsCmd())
				require.NoError(t, err, "error fetching pim neighbors from doublezero device")

				neighbor, ok := pim.Neighbors[expectedLinkLocalAddr]
				if !ok {
					return
				}
				if neighbor.Interface == "Tunnel500" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("PIM neighbor not established on Tunnel500")
		}) {
			t.Fail()
		}

		if !t.Run("check_s_comma_g_is_created", func(t *testing.T) {
			t.Parallel()

			// Send single ping to each multicast group to simulate multicast traffic
			// We ignore the expected errors because these are just to build the mroute
			// state on the switch, so we can check the mroute state later.
			_, _ = client.Exec(t.Context(), []string{"bash", "-c", "ping -c 1 -w 1 233.84.178.0"}, docker.NoPrintOnError())
			_, _ = client.Exec(t.Context(), []string{"bash", "-c", "ping -c 1 -w 1 233.84.178.1"}, docker.NoPrintOnError())

			expectedGroups := []string{"233.84.178.0", "233.84.178.1"}
			for _, mGroup := range expectedGroups {
				require.Eventually(t, func() bool {
					mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
					require.NoError(t, err, "error fetching mroutes from doublezero device")

					groups, ok := mroutes.Groups[mGroup]
					if !ok {
						dn.log.Debug("Waiting for multicast group to be created", "mGroup", mGroup, "mroutes", mroutes)
						return false
					}

					_, ok = groups.GroupSources[expectedAllocatedClientIP]
					require.True(t, ok, "source %s not found in multicast group %s", expectedAllocatedClientIP, mGroup)

					return true
				}, 5*time.Second, 1*time.Second, "multicast group %s not found in mroutes", mGroup)
			}
		}) {
			t.Fail()
		}

		if !t.Run("only_one_tunnel_allowed", func(t *testing.T) {
			// Set access pass for the client.
			_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
			require.NoError(t, err)

			_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
			require.Error(t, err, "User with different type already exists. Only one tunnel currently supported")
		}) {
			t.Fail()
		}

		dn.log.Info("--> Multicast publisher post-connect requirements checked")
	})
}

// checkMulticastPublisherPostDisconnect checks requirements after disconnecting a multicast publisher.
func checkMulticastPublisherPostDisconnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		dn.log.Info("==> Checking multicast publisher post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_publisher/doublezero_agent_config_user_removed.tmpl", map[string]any{
				"DeviceIP":    device.CYOANetworkIP,
				"StartTunnel": controllerconfig.StartUserTunnelNum,
				"EndTunnel":   controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
			})
			require.NoError(t, err, "error reading agent configuration fixture")
			err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
			require.NoError(t, err, "error waiting for agent config to match")
		}) {
			t.Fail()
		}

		tests := []struct {
			name        string
			fixturePath string
			data        map[string]any
			cmd         []string
		}{
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_publisher/doublezero_status_disconnected.txt",
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

			// Verify multicast routes are removed for both groups
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

		if !t.Run("check_user_tunnel_is_removed_from_agent", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd("vrf1"))
				require.NoError(t, err, "error fetching neighbors from doublezero device")

				_, ok := neighbors.VRFs["vrf1"].Peers[expectedLinkLocalAddr]
				if !ok {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("bgp neighbor %s has not been removed from doublezero device", expectedLinkLocalAddr)
		}) {
			t.Fail()
		}

		dn.log.Info("--> Multicast publisher post-disconnect requirements checked")
	})
}
