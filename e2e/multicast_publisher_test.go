//go:build e2e

package e2e_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/fixtures"
	"github.com/stretchr/testify/require"
)

func TestE2E_Multicast_Publisher(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())
	devnet, device, client := startSingleDeviceSingleClientDevnet(t, log)

	if !t.Run("connect", func(t *testing.T) {
		createMulticastGroupOnchain(t, log, devnet, client, "mg01")

		connectMulticastPublisher(t, log, client, "mg01")

		waitForClientTunnelUp(t, log, client)

		checkMulticastPublisherPostConnect(t, log, devnet, device, client)
	}) {
		t.Fail()
		return
	}

	if !t.Run("disconnect", func(t *testing.T) {
		disconnectMulticastPublisher(t, log, client)

		checkMulticastPublisherPostDisconnect(t, log, devnet, device, client)
	}) {
		t.Fail()
	}
}

func connectMulticastPublisher(t *testing.T, log *slog.Logger, client *devnet.Client, multicastGroupCode string) {
	log.Info("==> Connecting multicast publisher", "clientIP", client.IP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher " + multicastGroupCode + " --client-ip " + client.IP})
	require.NoError(t, err, "failed to connect multicast publisher")

	log.Info("--> Multicast publisher connected")
}

// DisconnectMulticastPublisher disconnects a multicast publisher from a multicast group.
func disconnectMulticastPublisher(t *testing.T, log *slog.Logger, client *devnet.Client) {
	log.Info("==> Disconnecting multicast publisher", "clientIP", client.IP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.IP})
	require.NoError(t, err, "failed to disconnect multicast publisher")

	log.Info("--> Multicast publisher disconnected")
}

// checkMulticastPublisherPostConnect checks requirements after connecting a multicast publisher.
func checkMulticastPublisherPostConnect(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_connect", func(t *testing.T) {
		log.Info("==> Checking multicast publisher post-connect requirements")

		expectedAllocatedClientIP := getNextAllocatedClientIP(device.InternalCYOAIP)

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_publisher/doublezero_agent_config_user_added.tmpl", map[string]string{
				"ClientIP":                  client.IP,
				"DeviceIP":                  device.InternalCYOAIP,
				"ExpectedAllocatedClientIP": expectedAllocatedClientIP,
			})
			require.NoError(t, err, "error reading agent configuration fixture")
			err = waitForAgentConfigMatchViaController(t, dn, device.AgentPubkey, string(config))
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
				fixturePath: "fixtures/multicast_publisher/doublezero_multicast_group_list.tmpl",
				data: map[string]string{
					"ManagerPubkey": dn.ManagerPubkey,
				},
				cmd: []string{"doublezero", "multicast", "group", "list"},
			},
			{
				name:        "doublezero_status",
				fixturePath: "fixtures/multicast_publisher/doublezero_status_connected.tmpl",
				data: map[string]string{
					"ClientIP":                  client.IP,
					"DeviceIP":                  device.InternalCYOAIP,
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
				"address":           client.IP,
				"link_pointtopoint": true,
				"broadcast":         device.InternalCYOAIP,
			}, links[0])
		}) {
			t.Fail()
		}

		if !t.Run("check_multicast_static_routes", func(t *testing.T) {
			t.Parallel()

			// Verify static multicast routes are installed for publisher
			routes, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "ip -j route show table main"})
			require.NoError(t, err)

			// Look for multicast route (233.84.178.0/32 for example)
			foundMcastRoute := false
			for _, route := range routes {
				dst, _ := route["dst"].(string)
				if dst == "233.84.178.0" {
					foundMcastRoute = true
					break
				}
			}
			if !foundMcastRoute {
				t.Fatalf("multicast route 233.84.178.0/32 not found for publisher: %+v", routes)
			}
		}) {
			t.Fail()
		}

		if !t.Run("check_s_comma_g_is_created", func(t *testing.T) {
			t.Parallel()

			// Send single ping to simulate multicast traffic
			// We ignore the expected error from this because it's happening just to build the mroute
			// state on the switch, so we can check the mroute state later.
			_, _ = client.Exec(t.Context(), []string{"bash", "-c", "ping -c 1 -w 1 233.84.178.0"})

			mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
			require.NoError(t, err, "error fetching mroutes from doublezero device")

			mGroup := "233.84.178.0"
			groups, ok := mroutes.Groups[mGroup]
			require.True(t, ok, "multicast group %s not found in mroutes", mGroup)

			_, ok = groups.GroupSources[expectedAllocatedClientIP]
			require.True(t, ok, "source %s not found in multicast group %s", expectedAllocatedClientIP, mGroup)
		}) {
			t.Fail()
		}

		log.Info("--> Multicast publisher post-connect requirements checked")
	})
}

// checkMulticastPublisherPostDisconnect checks requirements after disconnecting a multicast publisher.
func checkMulticastPublisherPostDisconnect(t *testing.T, log *slog.Logger, dn *devnet.Devnet, device *devnet.Device, client *devnet.Client) {
	// NOTE: Since we have inner parallel tests in this method, we need to wrap them in a
	// non-parallel test to ensure methods that follow this one wait for the inner tests to
	// complete.
	t.Run("check_post_disconnect", func(t *testing.T) {
		log.Info("==> Checking multicast publisher post-disconnect requirements")

		if !t.Run("wait_for_agent_config_from_controller", func(t *testing.T) {
			config, err := fixtures.Render("fixtures/multicast_publisher/doublezero_agent_config_user_removed.tmpl", map[string]string{
				"DeviceIP": device.InternalCYOAIP,
			})
			require.NoError(t, err, "error reading agent configuration fixture")
			err = waitForAgentConfigMatchViaController(t, dn, device.AgentPubkey, string(config))
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
				fixturePath: "fixtures/multicast_publisher/doublezero_status_disconnected.txt",
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

			got, err := client.Exec(t.Context(), []string{"bash", "-c", "ip -j link show dev doublezero1"})
			require.Error(t, err)
			require.Equal(t, `Device "doublezero1" does not exist.`, strings.TrimSpace(string(got)))
		}) {
			t.Fail()
		}

		if !t.Run("check_multicast_routes_removed", func(t *testing.T) {
			t.Parallel()

			routes, err := client.ExecReturnJSONList(t.Context(), []string{
				"bash", "-c", "ip -j route show table main",
			})
			require.NoError(t, err)

			// Verify multicast routes are removed
			for _, route := range routes {
				if dst, ok := route["dst"].(string); ok && dst == "233.84.178.0" {
					t.Fatalf("multicast route 233.84.178.0/32 should be removed after disconnect: found in %+v", routes)
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

		log.Info("--> Multicast publisher post-disconnect requirements checked")
	})
}
