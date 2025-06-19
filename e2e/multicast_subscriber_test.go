//go:build e2e

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

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
		dn.CreateMulticastGroupOnchain(t, client, "mg01")

		dn.ConnectMulticastSubscriber(t, client, "mg01")

		dn.WaitForClientTunnelUp(t, client)

		checkMulticastSubscriberPostConnect(t, dn, device, client)

		dn.CreateMulticastGroupOnchain(t, client, "mg02")

		output, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber mg02 --client-ip " + client.CYOANetworkIP})
		require.Error(t, err)
		require.Contains(t, string(output), "Multicast supports only one subscription at this time")
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
			config, err := fixtures.Render("fixtures/multicast_subscriber/doublezero_agent_config_user_added.tmpl", map[string]string{
				"ClientIP": client.CYOANetworkIP,
				"DeviceIP": device.CYOANetworkIP,
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
			data        map[string]string
			cmd         []string
		}{
			{
				name:        "doublezero_multicast_group_list",
				fixturePath: "fixtures/multicast_subscriber/doublezero_multicast_group_list.tmpl",
				data: map[string]string{
					"ManagerPubkey": dn.Manager.Pubkey,
				},
				cmd: []string{"doublezero", "multicast", "group", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_subscriber/doublezero_status_connected.tmpl",
				data: map[string]string{
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

			found := false
			for _, route := range routes {
				if dst, ok := route["dst"].(string); ok && dst == "233.84.178.0" {
					found = true
					break
				}
			}

			if !found {
				t.Fatalf("multicast group address 233.84.178.0 not found on tunnel for subscriber: %+v", routes)
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

		if !t.Run("check_pim_join_received", func(t *testing.T) {
			t.Parallel()

			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
				require.NoError(t, err, "error fetching mroutes from doublezero device")

				groups, ok := mroutes.Groups["233.84.178.0"]
				if !ok {
					return
				}
				groupDetails, ok := groups.GroupSources["0.0.0.0"]
				if !ok {
					return
				}
				if slices.Contains(groupDetails.OIFList, "Tunnel500") {
					return
				}
				time.Sleep(1 * time.Second)
			}
			t.Fatalf("PIM join not received for 233.84.178.0")
		}) {
			t.Fail()
		}

		if !t.Run("only_one_tunnel_allowed", func(t *testing.T) {
			_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
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
			config, err := fixtures.Render("fixtures/multicast_subscriber/doublezero_agent_config_user_removed.tmpl", map[string]string{
				"DeviceIP": device.CYOANetworkIP,
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
			data        map[string]string
			cmd         []string
		}{
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_subscriber/doublezero_status_disconnected.txt",
				data:        map[string]string{},
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

		dn.log.Info("--> Multicast subscriber post-disconnect requirements checked")
	})
}
