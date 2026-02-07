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
	"github.com/malbeclabs/doublezero/e2e/internal/netlink"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/stretchr/testify/require"
)

// ibrlUserTunnelInfo holds tunnel assignment info for an IBRL user, parsed from user list output.
type ibrlUserTunnelInfo struct {
	ClientIP  string
	TunnelID  string
	TunnelNet string
}

// parseIBRLTunnelInfo parses user list output and returns tunnel info for IBRL and IBRLWithAllocatedIP users.
func parseIBRLTunnelInfo(output []byte) (ibrlUsers []ibrlUserTunnelInfo, allocUserTunnelID, allocUserTunnelNet string) {
	rows := fixtures.ParseCLITable(output)
	for _, row := range rows {
		switch row["user_type"] {
		case "IBRL":
			ibrlUsers = append(ibrlUsers, ibrlUserTunnelInfo{
				ClientIP:  row["client_ip"],
				TunnelID:  row["tunnel_id"],
				TunnelNet: row["tunnel_net"],
			})
		case "IBRLWithAllocatedIP":
			allocUserTunnelID = row["tunnel_id"]
			allocUserTunnelNet = row["tunnel_net"]
		}
	}
	return
}

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
	dn.log.Debug("==> Creating multiple IBRL users on a single device with allocated IP addresses")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip 1.2.3.4 --user-payer " + client.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip 2.3.4.5 --user-payer " + client.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip 3.4.5.6 --user-payer " + client.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip 4.5.6.7 --user-payer " + client.Pubkey})
	require.NoError(t, err)
	// Set access pass for the client.
	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip 5.6.7.8 --user-payer " + client.Pubkey})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", `
		doublezero user create --device la2-dz01 --client-ip 1.2.3.4
		doublezero user create --device la2-dz01 --client-ip 2.3.4.5
		doublezero user create --device la2-dz01 --client-ip 3.4.5.6
		doublezero user create --device la2-dz01 --client-ip 4.5.6.7
		doublezero user create --device la2-dz01 --client-ip 5.6.7.8
	`})
	require.NoError(t, err)

	dn.log.Debug("--> Multiple IBRL users on a single device with allocated IP addresses created")
}

// checkIBRLWithAllocatedIPPostConnect checks requirements after connecting a user tunnel with an allocated IP.
func checkIBRLWithAllocatedIPPostConnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		dn.log.Debug("==> Checking IBRL with allocated IP post-connect requirements")

		// Parse the dz_prefix to get the base IP and prefix length
		// User IPs are allocated from the dz_prefix, not the public IP
		dzPrefixIP, dzPrefixNet, err := netutil.ParseCIDR(device.DZPrefix)
		require.NoError(t, err)
		ones, _ := dzPrefixNet.Mask.Size()
		allocatableBits := 32 - ones // number of host bits

		// First IP is reserved for device tunnel endpoint (Loopback100 interface)
		expectedAllocatedClientIP, err := nextAllocatableIP(dzPrefixIP, allocatableBits, map[string]bool{dzPrefixIP: true})
		require.NoError(t, err)

		dn.log.Debug("--> Expected allocated client IP", "expectedAllocatedClientIP", expectedAllocatedClientIP, "deviceCYOAIP", device.CYOANetworkIP, "dzPrefix", device.DZPrefix)

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_added.tmpl", map[string]any{
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

		if !t.Run("wait_for_user_activation", func(t *testing.T) {
			err := dn.WaitForUserActivation(t)
			require.NoError(t, err, "error waiting for user activation")
		}) {
			t.Fail()
		}

		// Query actual tunnel assignments since tunnel IDs may not be allocated sequentially.
		userListOutput, err := client.Exec(t.Context(), []string{"doublezero", "user", "list"})
		require.NoError(t, err, "error querying user list for tunnel info")
		ibrlUsers, allocatedUserTunnelID, allocatedUserTunnelNet := parseIBRLTunnelInfo(userListOutput)

		tests := []struct {
			name        string
			fixturePath string
			data        map[string]any
			cmd         []string
		}{
			{
				name:        "doublezero_user_list",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_added.tmpl",
				data: map[string]any{
					"ClientIP":                  client.CYOANetworkIP,
					"ClientPubkeyAddress":       client.Pubkey,
					"DeviceIP":                  device.CYOANetworkIP,
					"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
					"IBRLUsers":                 ibrlUsers,
					"AllocatedUserTunnelID":     allocatedUserTunnelID,
					"AllocatedUserTunnelNet":    allocatedUserTunnelNet,
				},
				cmd: []string{"doublezero", "user", "list"},
			},
			{
				name:        "doublezero_device_list",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_device_list.tmpl",
				data: map[string]any{
					"DeviceIP":       device.CYOANetworkIP,
					"ManagerPubkey":  dn.Manager.Pubkey,
					"DeviceDZPrefix": device.DZPrefix,
				},
				cmd: []string{"doublezero", "device", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_status_connected.tmpl",
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

		// User ban verified in the `doublezero_user_list_user_removed.tmpl` fixture.
		if !t.Run("ban_user", func(t *testing.T) {
			t.Parallel()

			// Find IBRL user with client_ip 3.4.5.6
			output, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero user list | grep 3.4.5.6"})
			require.NoError(t, err)
			require.NotEmpty(t, strings.TrimSpace(string(output)), "no user found with client_ip 3.4.5.6")
			userID := strings.TrimSpace(strings.Split(string(output), "|")[0])

			// TODO: This is brittle, come up with a better solution.
			_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user request-ban --pubkey " + userID})
			require.NoError(t, err)
		}) {
			t.Fail()
		}

		dn.log.Debug("--> IBRL with allocated IP post-connect requirements checked")
	})
}

// checkIBRLWithAllocatedIPPostDisconnect checks requirements after disconnecting a user tunnel with an allocated IP.
func checkIBRLWithAllocatedIPPostDisconnect(t *testing.T, dn *TestDevnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		dn.log.Debug("==> Checking IBRL with allocated IP post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_removed.tmpl", map[string]any{
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
				fixturePath: "fixtures/ibrl_with_allocated_addr/doublezero_status_disconnected.txt",
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

			// Parse actual tunnel assignments since tunnel IDs may not be allocated sequentially.
			ibrlUsers, _, _ := parseIBRLTunnelInfo(got)

			want, err := fixtures.Render("fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_removed.tmpl", map[string]any{
				"ClientPubkeyAddress": client.Pubkey,
				"IBRLUsers":           ibrlUsers,
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

			deadline := time.Now().Add(60 * time.Second)
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

		dn.log.Debug("--> IBRL with allocated IP post-disconnect requirements checked")
	})
}
