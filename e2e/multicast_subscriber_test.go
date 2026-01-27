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
	"github.com/stretchr/testify/require"
)

func TestE2E_Multicast_Subscriber(t *testing.T) {
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
		dn.ConnectMulticastSubscriberSkipAccessPass(t, client, "mg01", "mg02")

		err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		checkMulticastSubscriberPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectMulticastSubscriber(t, client)

		checkMulticastSubscriberPostDisconnect(t, dn, device, client)
	}) {
		t.Fail()
	}
}

func checkMulticastSubscriberPostConnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		dn.log.Info("==> Checking multicast subscriber post-connect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_subscriber/doublezero_agent_config_user_added.tmpl", map[string]any{
				"ClientIP":    client.CYOANetworkIP,
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
				name:        "doublezero_multicast_group_list",
				fixturePath: "fixtures/multicast_subscriber/doublezero_multicast_group_list.tmpl",
				data: map[string]any{
					"ManagerPubkey": dn.Manager.Pubkey,
				},
				cmd: []string{"doublezero", "multicast", "group", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_subscriber/doublezero_status_connected.tmpl",
				data: map[string]any{
					"ClientIP": client.CYOANetworkIP,
					"DeviceIP": device.CYOANetworkIP,
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
					t.Fatalf("multicast group address %s not found on tunnel for subscriber: %+v", expectedAddr, routes)
				}
			}
		}) {
			t.Fail()
		}

		if !t.Run("check_bgp_neighbor_established", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd(""))
				require.NoError(t, err, "error fetching bgp summary from doublezero device")

				peer, ok := neighbors.VRFs["default"].Peers[expectedLinkLocalAddr]
				if ok && peer.PeerState == "Established" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("BGP neighbor %s not in Established state on device", expectedLinkLocalAddr)
		}) {
			t.Fail()
		}

		if !t.Run("check_device_tunnel_interface", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				ifaces, err := devnet.DeviceExecAristaCliJSON[*arista.ShowInterfaces](t.Context(), device, arista.ShowInterfacesCmd("Tunnel500"))
				if err != nil {
					time.Sleep(1 * time.Second)
					continue
				}

				iface, ok := ifaces.Interfaces["Tunnel500"]
				if ok && iface.LineProtocolStatus == "up" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("Tunnel500 interface not up on device")
		}) {
			t.Fail()
		}

		// NOTE: PIM join and mroute checks are skipped because PIM interfaces don't populate
		// in the single-device cEOS e2e environment (no PIM peer on the client side),
		// so mroutes are never created. This would need a two-device setup to test.

		if !t.Run("only_one_tunnel_allowed", func(t *testing.T) {
			// Set access pass for the client.
			_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
			require.NoError(t, err)

			_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
			require.Error(t, err, "User with different type already exists. Only one tunnel currently supported")
		}) {
			t.Fail()
		}

		dn.log.Info("--> Multicast subscriber post-connect requirements checked")
	})
}

func checkMulticastSubscriberPostDisconnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		dn.log.Info("==> Checking multicast subscriber post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_subscriber/doublezero_agent_config_user_removed.tmpl", map[string]any{
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
				fixturePath: "fixtures/multicast_subscriber/doublezero_status_disconnected.txt",
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

		if !t.Run("check_device_tunnel_interface_removed", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				ifaces, err := devnet.DeviceExecAristaCliJSON[*arista.ShowInterfaces](t.Context(), device, arista.ShowInterfacesCmd("Tunnel500"))
				if err != nil {
					return // interface doesn't exist, success
				}

				iface, ok := ifaces.Interfaces["Tunnel500"]
				if !ok || iface.LineProtocolStatus != "up" {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("Tunnel500 interface still up on device after disconnect")
		}) {
			t.Fail()
		}

		if !t.Run("check_user_tunnel_is_removed_from_agent", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](t.Context(), device, arista.ShowIPBGPSummaryCmd(""))
				require.NoError(t, err, "error fetching neighbors from doublezero device")

				_, ok := neighbors.VRFs["default"].Peers[expectedLinkLocalAddr]
				if !ok {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("bgp neighbor %s has not been removed from doublezero device", expectedLinkLocalAddr)
		}) {
			t.Fail()
		}

		dn.log.Info("--> Multicast subscriber post-disconnect requirements checked")
	})
}
