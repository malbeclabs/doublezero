//go:build e2e

package e2e_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e/internal/netlink"
	"github.com/stretchr/testify/require"
)

func TestE2E_IBRL_WithAllocatedIP(t *testing.T) {
	t.Parallel()

	dn, device, client := NewSingleDeviceSingleClientTestDevnet(t)

	if !t.Run("connect", func(t *testing.T) {
		dn.ConnectUserTunnelWithAllocatedIP(t, client)

		createMultipleIBRLUsersOnSameDeviceWithAllocatedIPs(t, dn, client)

		err := client.WaitForTunnelUp(t.Context(), 90*time.Second)
		require.NoError(t, err)

		checkIBRLWithAllocatedIPPostConnect(t, dn, device, client)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		dn.DisconnectUserTunnel(t, client)

		checkIBRLWithAllocatedIPPostDisconnect(t, dn, device, client)
	}) {
		t.Fail()
	}
}

func createMultipleIBRLUsersOnSameDeviceWithAllocatedIPs(t *testing.T, dn *TestDevnet, client *devnet.Client) {
	dn.log.Info("==> Creating multiple IBRL users on a single device with allocated IP addresses")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip 1.2.3.4 --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip 2.3.4.5 --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip 3.4.5.6 --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip 4.5.6.7 --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip 5.6.7.8 --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", `
		doublezero user create --device la2-dz01 --client-ip 1.2.3.4
		doublezero user create --device la2-dz01 --client-ip 2.3.4.5
		doublezero user create --device la2-dz01 --client-ip 3.4.5.6
		doublezero user create --device la2-dz01 --client-ip 4.5.6.7
		doublezero user create --device la2-dz01 --client-ip 5.6.7.8
	`})
	require.NoError(t, err)

	dn.log.Info("--> Multiple IBRL users on a single device with allocated IP addresses created")
}

// checkIBRLWithAllocatedIPPostConnect checks requirements after connecting a user tunnel with an allocated IP.
func checkIBRLWithAllocatedIPPostConnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		dn.log.Info("==> Checking IBRL with allocated IP post-connect requirements")

		expectedAllocatedClientIP, err := nextAllocatableIP(device.CYOANetworkIP, int(device.Spec.CYOANetworkAllocatablePrefix), map[string]bool{})
		require.NoError(t, err)

		dn.log.Info("--> Expected allocated client IP", "expectedAllocatedClientIP", expectedAllocatedClientIP, "deviceCYOAIP", device.CYOANetworkIP)

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_added.tmpl", map[string]string{
				"ClientIP":                  client.CYOANetworkIP,
				"DeviceIP":                  device.CYOANetworkIP,
				"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
			})
			require.NoError(t, err, "error reading agent configuration fixture")
			err = dn.WaitForAgentConfigMatchViaController(t, device.ID, string(config))
			require.NoError(t, err, "error waiting for agent config to match")
		}) {
			t.Fail()
		}

		if !t.Run("wait_for_user_activation", func(t *testing.T) {
			err := dn.WaitForUserActivation(t)
			require.NoError(t, err, "error waiting for user activation")
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
				name:        "doublezero_user_list",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_added.tmpl",
				data: map[string]string{
					"ClientIP":                  client.CYOANetworkIP,
					"ClientPubkeyAddress":       client.Pubkey,
					"DeviceIP":                  device.CYOANetworkIP,
					"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
				},
				cmd: []string{"doublezero", "user", "list"},
			},
			{
				name:        "doublezero_device_list",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_device_list.tmpl",
				data: map[string]string{
					"DeviceIP":                device.CYOANetworkIP,
					"ManagerPubkey":           dn.Manager.Pubkey,
					"DeviceAllocatablePrefix": strconv.Itoa(int(device.Spec.CYOANetworkAllocatablePrefix)),
				},
				cmd: []string{"doublezero", "device", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_status_connected.tmpl",
				data: map[string]string{
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
			require.True(t, netlink.HasAddr(ifaces, "doublezero0", expectedAllocatedClientIP), fmt.Sprintf("doublezero0 should have allocated client IP %s in: %+v", expectedAllocatedClientIP, ifaces))
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

			_, ok = routes.VRFs["vrf1"].Routes[expectedAllocatedClientIP+"/32"]
			if !ok {
				fmt.Println(routes)
				t.Fatalf("expected allocated client IP %s installed; got none\n", expectedAllocatedClientIP)
			}
		}) {
			t.Fail()
		}

		// User ban verified in the `doublezer_user_list_removed.txt` fixture.
		if !t.Run("ban_user", func(t *testing.T) {
			t.Parallel()

			// Find user with tunnel_net 169.254.0.6/31
			output, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero user list | grep 169.254.0.6/31"})
			require.NoError(t, err)
			require.NotEmpty(t, strings.TrimSpace(string(output)), "no user found with tunnel_net 169.254.0.6/31")
			userID := strings.TrimSpace(strings.Split(string(output), "|")[0])

			// TODO: This is brittle, come up with a better solution.
			_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user request-ban --pubkey " + userID})
			require.NoError(t, err)
		}) {
			t.Fail()
		}

		dn.log.Info("--> IBRL with allocated IP post-connect requirements checked")
	})
}

// checkIBRLWithAllocatedIPPostDisconnect checks requirements after disconnecting a user tunnel with an allocated IP.
func checkIBRLWithAllocatedIPPostDisconnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		dn.log.Info("==> Checking IBRL with allocated IP post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_removed.tmpl", map[string]string{
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
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_status_disconnected.txt",
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

			want, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_removed.tmpl", map[string]string{
				"ClientPubkeyAddress": client.Pubkey,
			})
			require.NoError(t, err, "error reading user list fixture")

			diff := fixtures.DiffCLITable(got, []byte(want))
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

		dn.log.Info("--> IBRL with allocated IP post-disconnect requirements checked")
	})
}
