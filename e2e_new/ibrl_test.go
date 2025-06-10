//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	e2e "github.com/malbeclabs/doublezero/e2e_new"
	"github.com/malbeclabs/doublezero/e2e_new/internal/arista"
	"github.com/malbeclabs/doublezero/e2e_new/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e_new/internal/fixtures"
	"github.com/malbeclabs/doublezero/e2e_new/internal/netlink"
	"github.com/stretchr/testify/require"
)

func TestE2E_IBRL(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	devnet, device, client := startSingleDeviceSingleClientDevnet(t, log)

	if !t.Run("connect", func(t *testing.T) {
		connectIBRLUserTunnel(t, log, client)

		waitForClientTunnelUp(t, log, client)

		checkIBRLPostConnect(t, log, devnet, device, client)
	}) {
		t.Fail()
	}

	if !t.Run("disconnect", func(t *testing.T) {
		disconnectUserTunnel(t, log, client)

		checkIBRLPostDisconnect(t, log, devnet, device, client)
	}) {
		t.Fail()
	}
}

func connectIBRLUserTunnel(t *testing.T, log *slog.Logger, client *devnet.Client) {
	log.Info("==> Connecting IBRL user tunnel")

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.IP})
	require.NoError(t, err)

	log.Info("--> IBRL user tunnel connected")
}

// checkIBRLPostConnect checks requirements after connecting a user tunnel.
func checkIBRLPostConnect(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device, client *devnet.Client) {
	log.Info("==> Checking IBRL post-connect requirements")

	if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
		config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_user_added.tmpl", map[string]string{
			"ClientIP": client.IP,
			"DeviceIP": device.InternalCYOAIP,
		})
		require.NoError(t, err, "error reading agent configuration fixture")
		err = waitForAgentConfigMatchViaController(t, dn, string(config))
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
			name:        "doublezero_user_list",
			fixturePath: "fixtures/ibrl/doublezero_user_list_user_added.tmpl",
			data: map[string]string{
				"ClientIP":            client.IP,
				"ClientPubkeyAddress": client.PubkeyAddress,
			},
			cmd: []string{"doublezero", "user", "list"},
		},
		{
			name:        "doublezero_status",
			fixturePath: "fixtures/ibrl/doublezero_status_connected.tmpl",
			data: map[string]string{
				"ClientIP": client.IP,
				"DeviceIP": device.InternalCYOAIP,
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
		require.Equal(t, map[string]any{
			"ifindex": float64(13),
			"link":    nil,
			"ifname":  "doublezero0",
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
			"address":           client.IP,
			"link_pointtopoint": true,
			"broadcast":         device.InternalCYOAIP,
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

		routes, err := client.ExecReturnJSONList(t.Context(), []string{
			"bash", "-c", "ip -j route show table main",
		})
		require.NoError(t, err)

		var matchingRoute map[string]any
		for _, route := range routes {
			if dst, ok := route["dst"].(string); ok && dst == "8.8.8.8" {
				matchingRoute = route
				break
			}
		}
		require.NotNil(t, matchingRoute, "no route to 8.8.8.8 found")

		require.Equal(t, "8.8.8.8", matchingRoute["dst"])
		require.Equal(t, "169.254.0.0", matchingRoute["gateway"])
		require.Equal(t, "doublezero0", matchingRoute["dev"])
		require.Equal(t, client.IP, matchingRoute["prefsrc"])
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

		clientRoute := client.IP + "/32"
		_, ok = routes.VRFs["vrf1"].Routes[clientRoute]
		require.True(t, ok, "expected client route of %s installed; got none\n", clientRoute)
	}) {
		t.Fail()
	}

	log.Info("--> IBRL post-connect requirements checked")
}

// checkIBRLPostDisconnect checks requirements after disconnecting a user tunnel.
func checkIBRLPostDisconnect(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device, client *devnet.Client) {
	log.Info("==> Checking IBRL post-disconnect requirements")

	if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
		config, err := fixtures.Render("fixtures/ibrl/doublezero_agent_config_user_removed.tmpl", map[string]string{
			"DeviceIP": device.InternalCYOAIP,
		})
		require.NoError(t, err, "error reading agent configuration fixture")
		err = waitForAgentConfigMatchViaController(t, dn, string(config))
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
			name:        "doublezero_user_list",
			fixturePath: "fixtures/ibrl/doublezero_user_list_user_removed.txt",
			data:        map[string]string{},
			cmd:         []string{"doublezero", "user", "list"},
		},
		{
			name:        "doublezero_status",
			fixturePath: "fixtures/ibrl/doublezero_status_disconnected.txt",
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

		got, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero0"})
		require.Error(t, err)
		require.Equal(t, `Device "doublezero0" does not exist.`, strings.TrimSpace(string(got)))
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

	log.Info("--> IBRL post-disconnect requirements checked")
}
