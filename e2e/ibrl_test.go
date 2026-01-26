//go:build e2e

package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	controllerconfig "github.com/malbeclabs/doublezero/controlplane/controller/config"
	e2e "github.com/malbeclabs/doublezero/e2e"
	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e/internal/netlink"
	"github.com/stretchr/testify/require"
)

func TestE2E_IBRL(t *testing.T) {
	t.Parallel()

	dn, device, client := NewSingleDeviceSingleClientTestDevnet(t)

	if !t.Run("connect", func(t *testing.T) {
		dn.ConnectIBRLUserTunnel(t, client)
		err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		checkIBRLPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectUserTunnel(t, client)

		checkIBRLPostDisconnect(t, dn, device, client)
	}) {
		t.Fail()
	}

	if !t.Run("remove_ibgp_msdp_peer", func(t *testing.T) {
		dn.DeleteDeviceLoopbackInterface(t.Context(), "pit-dzd01", "Loopback255")
		time.Sleep(30 * time.Second) // Wait for the device to process the change
		checkIbgpMsdpPeerRemoved(t, dn, device)
	}) {
		t.Fail()
	}

	if !t.Run("drain_device", func(t *testing.T) {
		checkDeviceDrain(t, dn, device)
	}) {
		t.Fail()
	}

	if !t.Run("undrain_device", func(t *testing.T) {
		checkDeviceUndrain(t, dn, device)
	}) {
		t.Fail()
	}
}

func checkIbgpMsdpPeerRemoved(t *testing.T, dn *TestDevnet, device *devnet.Device) {
	dn.log.Info("==> Checking that iBGP/MSDP peers have been removed after peer's Loopback255 interface was removed")

	if !t.Run("wait_for_agent_config_after_peer_removal", func(t *testing.T) {
		// We need a new fixture that shows the config after pit-dzd01's Loopback255 is removed
		// This fixture should have pit-dzd01's BGP and MSDP peer configurations removed
		config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_peer_removed.tmpl", map[string]any{
			"DeviceIP":    device.CYOANetworkIP,
			"StartTunnel": controllerconfig.StartUserTunnelNum,
			"EndTunnel":   controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
		})
		require.NoError(t, err, "error reading agent configuration fixture for peer removal")
		err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
		require.NoError(t, err, "error waiting for agent config to match after peer removal")
	}) {
		t.Fail()
	}

	dn.log.Info("--> IBRL iBGP/MSDP peer removal requirements checked")
}

func checkDeviceDrain(t *testing.T, dn *TestDevnet, device *devnet.Device) {
	dn.log.Info("==> Checking that device is drained")

	if !t.Run("set_device_status_to_drained", func(t *testing.T) {
		_, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "device", "update", "--pubkey", device.Spec.Code, "--desired-status", "drained"})
		require.NoError(t, err)
	}) {
		t.Fail()
		return
	}

	if !t.Run("wait_for_drained_config", func(t *testing.T) {
		config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_drained.tmpl", map[string]any{
			"DeviceIP":    device.CYOANetworkIP,
			"StartTunnel": controllerconfig.StartUserTunnelNum,
			"EndTunnel":   controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
		})
		require.NoError(t, err, "error reading drained config fixture")
		err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
		require.NoError(t, err, "error waiting for drained config")
	}) {
		t.Fail()
	}

	dn.log.Info("--> Device drain requirements checked")
}

func checkDeviceUndrain(t *testing.T, dn *TestDevnet, device *devnet.Device) {
	dn.log.Info("==> Checking that device is undrained (returned to activated)")

	if !t.Run("set_device_status_to_activated", func(t *testing.T) {
		_, err := dn.Manager.Exec(t.Context(), []string{"doublezero", "device", "update", "--pubkey", device.Spec.Code, "--desired-status", "activated"})
		require.NoError(t, err)
	}) {
		t.Fail()
		return
	}

	if !t.Run("wait_for_undrained_config", func(t *testing.T) {
		// After undrain, the config should return to the state before draining
		// which is after peer removal (pit-dzd01 removed), with no shutdown commands
		config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_peer_removed.tmpl", map[string]any{
			"DeviceIP":    device.CYOANetworkIP,
			"StartTunnel": controllerconfig.StartUserTunnelNum,
			"EndTunnel":   controllerconfig.StartUserTunnelNum + controllerconfig.MaxUserTunnelSlots - 1,
		})
		require.NoError(t, err, "error reading undrained config fixture")
		err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
		require.NoError(t, err, "error waiting for undrained config")
	}) {
		t.Fail()
	}

	dn.log.Info("--> Device undrain requirements checked")
}

// checkIBRLPostConnect checks requirements after connecting a user tunnel.
func checkIBRLPostConnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		dn.log.Info("==> Checking IBRL post-connect requirements")

		if !t.Run("wait_for_agent_config_from_controller_post_connect", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_user_added.tmpl", map[string]any{
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
				name:        "doublezero_user_list",
				fixturePath: "fixtures/ibrl/doublezero_user_list_user_added.tmpl",
				data: map[string]any{
					"ClientIP":            client.CYOANetworkIP,
					"ClientPubkeyAddress": client.Pubkey,
				},
				cmd: []string{"doublezero", "user", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/ibrl/doublezero_status_connected.tmpl",
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

			links, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"})
			require.NoError(t, err)

			require.Len(t, links, 1)
			delete(links[0], "ifindex")
			require.Equal(t, map[string]any{
				"link":   nil,
				"ifname": "doublezero0",
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

		if !t.Run("check_doublezero_address_is_configured", func(t *testing.T) {
			t.Parallel()

			ifaces, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j -4 addr show dev doublezero0"})
			require.NoError(t, err)

			require.True(t, netlink.HasAddr(ifaces, "doublezero0", expectedLinkLocalAddr), fmt.Sprintf("doublezero0 should have link-local address %s in: %+v", expectedLinkLocalAddr, ifaces))
		}) {
			t.Fail()
		}

		if !t.Run("check_learned_route_installed", func(t *testing.T) {
			t.Parallel()

			var matchingRoute map[string]any
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
				require.NoError(t, err)

				for _, route := range routes {
					if dst, ok := route["dst"].(string); ok && dst == "8.8.8.8" {
						matchingRoute = route
						break
					}
				}

				if matchingRoute != nil {
					break
				}

				dn.log.Info("no route to 8.8.8.8 found, retrying...", "routes", routes)

				time.Sleep(1 * time.Second)
			}
			require.NotNil(t, matchingRoute, "no route to 8.8.8.8 found")

			require.Equal(t, "8.8.8.8", matchingRoute["dst"])
			require.Equal(t, "169.254.0.0", matchingRoute["gateway"])
			require.Equal(t, "doublezero0", matchingRoute["dev"])
			require.Equal(t, client.CYOANetworkIP, matchingRoute["prefsrc"])
		}) {
			t.Fail()
		}

		if !t.Run("check_user_session_is_established", func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			neighbors, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPBGPSummary](ctx, device, arista.ShowIPBGPSummaryCmd("vrf1"))
			require.NoError(t, err, "error fetching neighbors from doublezero device")

			routes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIpRoute](ctx, device, arista.ShowIpRouteCmd("vrf1"))
			require.NoError(t, err, "error fetching routes from doublezero device")

			peer, ok := neighbors.VRFs["vrf1"].Peers[expectedLinkLocalAddr]
			require.True(t, ok, "client ip %s missing from doublezero device\n", expectedLinkLocalAddr)
			require.Equal(t, "65000", peer.ASN, "client asn should be 65000; got %s\n", peer.ASN)
			require.Equal(t, "Established", peer.PeerState, "client state should be established; got %s\n", peer.PeerState)

			clientRoute := client.CYOANetworkIP + "/32"
			_, ok = routes.VRFs["vrf1"].Routes[clientRoute]
			require.True(t, ok, "expected client route of %s installed; got none\n", clientRoute)
		}) {
			t.Fail()
		}

		dn.log.Info("--> IBRL post-connect requirements checked")
	})
}

// checkIBRLPostDisconnect checks requirements after disconnecting a user tunnel.
func checkIBRLPostDisconnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		dn.log.Info("==> Checking IBRL post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller_post_disconnect", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_user_removed.tmpl", map[string]any{
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
				name:        "doublezero_user_list",
				fixturePath: "fixtures/ibrl/doublezero_user_list_user_removed.txt",
				data:        map[string]any{},
				cmd:         []string{"doublezero", "user", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/ibrl/doublezero_status_disconnected.txt",
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

			got, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"}, docker.NoPrintOnError())
			require.Error(t, err)
			require.Equal(t, `Device "doublezero0" does not exist.`, strings.TrimSpace(string(got)), err.Error())
		}) {
			t.Fail()
		}

		if !t.Run("check_user_contract_is_removed", func(t *testing.T) {
			t.Parallel()

			got, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero user list"})
			require.NoError(t, err)

			want, err := e2e.FS.ReadFile("fixtures/ibrl/doublezero_user_list_user_removed.txt")
			require.NoError(t, err, "error reading user list fixture")

			diff := fixtures.DiffCLITable(got, want)
			if diff != "" {
				fmt.Println(string(got))
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
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

		dn.log.Info("--> IBRL post-disconnect requirements checked")
	})
}
