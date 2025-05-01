package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	rt "runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"

	nl "github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
)

type dummyPlugin struct{}

func (p *dummyPlugin) GetCapabilities(c corebgp.PeerConfig) []corebgp.Capability {
	caps := make([]corebgp.Capability, 0)
	return caps
}

func (p *dummyPlugin) OnOpenMessage(peer corebgp.PeerConfig, routerID netip.Addr, capabilities []corebgp.Capability) *corebgp.Notification {
	return nil
}

func (p *dummyPlugin) OnEstablished(peer corebgp.PeerConfig, writer corebgp.UpdateMessageWriter) corebgp.UpdateMessageHandler {
	origin := gobgp.NewPathAttributeOrigin(0)
	nexthop := gobgp.NewPathAttributeNextHop("2.2.2.2")
	param := gobgp.NewAs4PathParam(2, []uint32{65001})
	aspath := gobgp.NewPathAttributeAsPath([]gobgp.AsPathParamInterface{param})
	update := gobgp.NewBGPUpdateMessage(
		[]*gobgp.IPAddrPrefix{gobgp.NewIPAddrPrefix(32, "4.4.4.4")},
		[]gobgp.PathAttributeInterface{origin, nexthop, aspath},
		[]*gobgp.IPAddrPrefix{gobgp.NewIPAddrPrefix(32, "3.3.3.3")})
	buf, err := update.Body.Serialize()
	if err != nil {
		log.Printf("error serializing: %v", err)
	}
	if err := writer.WriteUpdate(buf); err != nil {
		log.Printf("error writing update: %v", err)
	}
	return p.handleUpdate
}

func (p *dummyPlugin) OnClose(peer corebgp.PeerConfig) {}

func (p *dummyPlugin) handleUpdate(peer corebgp.PeerConfig, u []byte) *corebgp.Notification {
	return nil
}

func TestEndToEnd_IBRL(t *testing.T) {
	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	defer os.RemoveAll(rootPath)

	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	// TODO: create network namespace
	cmd := exec.Command("ip", "netns", "add", "doublezero-peer")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error creating network namespace: %v", err)
	}

	// TODO: create veth pair
	cmd = exec.Command("ip", "link", "add", "veth0", "type", "veth", "peer", "name", "veth1")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error creating veth pair: %v", err)
	}
	// TODO: move veth to doublezero-peer-test namespace
	cmd = exec.Command("ip", "link", "set", "dev", "veth1", "netns", "doublezero-peer")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error moving veth to namespace: %v", err)
	}

	// configure source end of veth
	cmd = exec.Command("ip", "addr", "add", "10.0.0.0/31", "dev", "veth0")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error configuring source end of veth: %v", err)
	}
	cmd = exec.Command("ip", "link", "set", "dev", "veth0", "up")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error bringing up source end of veth: %v", err)
	}

	cmd = exec.Command("ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "10.0.0.1/31", "dev", "veth1")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error configuring source end of veth: %v", err)
	}
	cmd = exec.Command("ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "veth1", "up")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error bringing up source end of veth: %v", err)
	}

	cmd = exec.Command("ip", "netns", "exec", "doublezero-peer", "ip", "tunnel", "add", "doublezero0", "mode", "gre", "local", "10.0.0.1", "remote", "10.0.0.0", "ttl", "64")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error creating tunnel: %v", err)
	}

	cmd = exec.Command("ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "169.254.0.0/31", "dev", "doublezero0")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error configuring tunnel: %v", err)
	}

	cmd = exec.Command("ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "doublezero0", "up")
	if err = cmd.Run(); err != nil {
		t.Fatalf("error bringing up tunnel: %v", err)
	}

	cleanup := func() {
		cmd = exec.Command("ip", "link", "del", "veth0")
		if err = cmd.Run(); err != nil {
			t.Fatalf("error deleting veth: %v", err)
		}
		cmd = exec.Command("ip", "netns", "del", "doublezero-peer")
		if err = cmd.Run(); err != nil {
			t.Fatalf("error deleting network namespace: %v", err)
		}
	}
	defer cleanup()

	// TODO: start corebgp instance in network namespace
	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		peerNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			t.Logf("error creating namespace: %v", err)
			return
		}
		if err = netns.Set(peerNS); err != nil {
			t.Logf("error setting namespace: %v", err)
			return
		}

		// start bgp instance in network namespace
		d := &dummyPlugin{}
		err = srv.AddPeer(corebgp.PeerConfig{
			RemoteAddress: netip.MustParseAddr("169.254.0.1"),
			LocalAS:       65342,
			RemoteAS:      65000,
		}, d, corebgp.WithPassive())
		if err != nil {
			log.Fatalf("error creating dummy bgp server: %v", err)
		}
		dlc := &net.ListenConfig{}
		dlis, err := dlc.Listen(context.Background(), "tcp", ":179")
		if err != nil {
			log.Fatalf("error constructing listener: %v", err)
		}

		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}
		t.Log("here")
	}()

	tests := []struct {
		name                string
		userType            netlink.UserType
		provisioningRequest map[string]any
		goldenStateFile     string
	}{
		{
			name:     "test_ibrl",
			userType: netlink.UserTypeIBRL,
			provisioningRequest: map[string]any{

				"tunnel_src":     "10.0.0.0",
				"tunnel_dst":     "10.0.0.1",
				"tunnel_net":     "169.254.0.0/31",
				"doublezero_ip":  "1.1.1.1",
				"user_type":      "IBRL",
				"bgp_local_asn":  65000,
				"bgp_remote_asn": 65342,
			},
			goldenStateFile: "./fixtures/doublezerod.ibrl.json",
		},
		{
			name:     "test_ibrl_with_allocated_ip",
			userType: netlink.UserTypeIBRLWithAllocatedIP,
			provisioningRequest: map[string]any{

				"tunnel_src":     "10.0.0.0",
				"tunnel_dst":     "10.0.0.1",
				"tunnel_net":     "169.254.0.0/31",
				"doublezero_ip":  "3.3.3.3",
				"user_type":      "IBRLWithAllocatedIP",
				"bgp_local_asn":  65000,
				"bgp_remote_asn": 65342,
			},
			goldenStateFile: "./fixtures/doublezerod.ibrl.with.allocated.ip.json",
		},
	}
	for _, test := range tests {
		errChan := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		t.Run(test.name, func(t *testing.T) {
			sockFile := filepath.Join(rootPath, "doublezerod.sock")
			go func() {
				programId := ""
				err := runtime.Run(ctx, sockFile, false, programId, "")
				errChan <- err
			}()

			httpClient := http.Client{
				Transport: &http.Transport{
					DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", sockFile)
					},
				},
			}

			t.Run("start_runtime", func(t *testing.T) {
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error starting runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
				}
			})

			t.Run("send_provision_request", func(t *testing.T) {
				url, err := url.JoinPath("http://localhost/", "provision")
				if err != nil {
					t.Fatalf("error creating url: %v", err)
				}
				body, _ := json.Marshal(test.provisioningRequest)
				req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
				if err != nil {
					t.Fatalf("error creating request: %v", err)
				}
				resp, err := httpClient.Do(req)
				if err != nil {
					t.Fatalf("error during request: %v", err)
				}
				defer resp.Body.Close()

				got, _ := io.ReadAll(resp.Body)
				want := `{"status": "ok"}`
				if string(got) != want {
					t.Fatalf("wrong response: %s", string(got))
				}
			})

			t.Run("verify_tunnel_is_up", func(t *testing.T) {
				tun, err := nl.LinkByName("doublezero0")
				if err != nil {
					t.Fatalf("error fetching tunnel status: %v", err)
				}
				if tun.Attrs().Name != "doublezero0" {
					t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
				}
				if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
					t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
				}
				if tun.Attrs().MTU != 1476 {
					t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
				}
			})

			t.Run("verify_state_file_is_created", func(t *testing.T) {
				got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
				if err != nil {
					t.Fatalf("error reading state file: %v", err)
				}
				want, err := os.ReadFile(test.goldenStateFile)
				if err != nil {
					t.Fatalf("error reading state file: %v", err)
				}
				if diff := cmp.Diff(string(want), string(got)); diff != "" {
					t.Fatalf("State mismatch (-want +got): %s\n", diff)
				}
			})

			t.Run("verify_routes_flushed_on_session_down_event", func(t *testing.T) {
				if test.userType == netlink.UserTypeIBRLWithAllocatedIP {
					t.Skip("we don't flush routes in IBRLWithAllocatedIP mode")
				}

				t.Logf("peers: %+v\n", srv.ListPeers())
				if err := srv.DeletePeer(netip.AddrFrom4([4]byte{169, 254, 0, 1})); err != nil {
					t.Fatalf("error deleting peer: %v", err)
				}

				// should not have any routes tagged bgp
				routes, err := nl.RouteList(nil, nl.FAMILY_ALL)
				if err != nil {
					log.Fatal(err)
				}

				for _, route := range routes {
					if route.Protocol == 186 {
						slog.Error("Expected route to be withdrawn", "route", route)
						os.Exit(1)
					}
				}

				// re-add peer
				d := &dummyPlugin{}
				err = srv.AddPeer(corebgp.PeerConfig{
					RemoteAddress: netip.MustParseAddr("169.254.0.1"),
					LocalAS:       65342,
					RemoteAS:      65000,
				}, d, corebgp.WithPassive())
				if err != nil {
					log.Fatalf("error creating dummy bgp server: %v", err)
				}

				// ensure that 4.4.4.4,3.3.3.3 are added and tagged with bgp (186)
				routes, err = nl.RouteList(nil, nl.FAMILY_ALL)
				if err != nil {
					log.Fatal(err)
				}

				for _, route := range routes {
					fmt.Printf("route: %s\n", route.Protocol)
				}
			})

			t.Run("stop_runtime", func(t *testing.T) {
				cancel()
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error stopping runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
					log.Fatalf("timed out waiting for close")
				}
			})

			ctx, cancel = context.WithCancel(context.Background())
			go func() {
				programId := ""
				err := runtime.Run(ctx, sockFile, false, programId, "")
				errChan <- err
			}()

			<-time.After(5 * time.Second)

			t.Run("restart_runtime", func(t *testing.T) {
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error starting runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
				}
			})

			t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
				tun, err := nl.LinkByName("doublezero0")
				if err != nil {
					t.Fatalf("error fetching tunnel status: %v", err)
				}
				if tun.Attrs().Name != "doublezero0" {
					t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
				}
				if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
					t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
				}
				if tun.Attrs().MTU != 1476 {
					t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
				}
			})

			t.Run("send_remove_request", func(t *testing.T) {
				url, err := url.JoinPath("http://localhost/", "remove")
				if err != nil {
					t.Fatalf("error creating url: %v", err)
				}
				req, err := http.NewRequest(http.MethodPost, url, nil)
				if err != nil {
					t.Fatalf("error creating request: %v", err)
				}
				resp, err := httpClient.Do(req)
				if err != nil {
					t.Fatalf("error during request: %v", err)
				}
				defer resp.Body.Close()

				got, _ := io.ReadAll(resp.Body)
				want := `{"status": "ok"}`
				if string(got) != want {
					t.Fatalf("wrong response: %s", string(got))
				}
			})

			t.Run("verify_tunnel_is_removed", func(t *testing.T) {
				_, err := nl.LinkByName("doublezero0")
				if !errors.As(err, &nl.LinkNotFoundError{}) {
					t.Fatalf("expected LinkNotFoundError; got: %v", err)
				}
			})

			t.Run("state_removal_stop_runtime", func(t *testing.T) {
				cancel()
				select {
				case err := <-errChan:
					if err != nil {
						t.Fatalf("error stopping runtime: %v", err)
					}
				case <-time.After(5 * time.Second):
					log.Fatalf("timed out waiting for close")
				}
			})

			t.Run("state_removal_verify_state_file_removed", func(t *testing.T) {
				if _, err := os.Stat(filepath.Join(rootPath, "doublezerod", "doublezerod.json")); err == nil {
					t.Fatalf("state file still exists when should be removed")
				}
			})
		})
	}
}

// TestEndToEnd_EdgeFiltering exercises the entire client daemon end to end. It starts
// the runtime, makes a provisioning http call, verifies netlink state has
// been created as well as the statefile. The daemon is then restarted to verify
// successful recovery via the statefile.
// The test then tears down the state via the remove http endpoint and verifies
// the tunnel, ip rules, routes as well as the statefile have been successfully
// removed.
func TestEndToEnd_EdgeFiltering(t *testing.T) {
	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	defer os.RemoveAll(rootPath)

	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		programId := ""
		err := runtime.Run(ctx, sockFile, false, programId, "")
		errChan <- err
	}()

	httpClient := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockFile)
			},
		},
	}

	// case: clean start and provision tunnel
	t.Run("start_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
		}
	})

	t.Run("send_provision_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "provision")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		body := `{
					"tunnel_src": "1.1.1.1",
					"tunnel_dst": "2.2.2.2",
					"tunnel_net": "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": ["3.0.0.0/24"],
					"user_type": "EdgeFiltering"
				}`
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		defer resp.Body.Close()

		got, _ := io.ReadAll(resp.Body)
		want := `{"status": "ok"}`
		if string(got) != want {
			t.Fatalf("wrong response: %s", string(got))
		}
	})

	t.Run("verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero0" {
			t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("verify_ip_rules_created", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src != nil {
			t.Fatalf("rule 100 should be sourced from all; got %s", rules[0].Src)
		}
		if rules[0].Dst.String() != "3.0.0.0/24" {
			t.Fatalf("rule 100 should be destined to 3.0.0.0/24; got %s", rules[0].Dst)
		}
		if rules[0].Table != 100 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src.String() != "3.0.0.0/24" {
			t.Fatalf("rule 101 should be sourced from 3.0.0.0")
		}
		if rules[0].Dst != nil {
			t.Fatalf("rule 101 should be destined too all; got %s", rules[0].Dst)
		}
		if rules[0].Table != 101 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
	})

	t.Run("verify_default_route_created", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if !route[0].Src.Equal(net.IP{3, 3, 3, 3}) {
			t.Fatalf("route src hint should be 3.3.3.3; got %s", route[0].Src)
		}
		if route[0].Dst.String() != "0.0.0.0/0" {
			t.Fatalf("route dst should be 0.0.0.0/0; got %s", route[0].Dst)
		}
		if !route[0].Gw.Equal(net.IP{169, 254, 0, 0}) {
			t.Fatalf("route gw should be 169.254.0.0; got %s", route[0].Gw)
		}
	})

	// TODO: verify specific routes are created; this needs namespaces

	t.Run("verify_state_file_is_created", func(t *testing.T) {
		got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		want, err := os.ReadFile("./fixtures/doublezerod.edgefiltering.json")
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("State mismatch (-want +got): %s\n", diff)
		}
	})

	// case: restart and auto-recover state
	t.Run("stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Fatalf("timed out waiting for close")
		}
	})

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		programId := ""
		err := runtime.Run(ctx, sockFile, false, programId, "")
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
		}
	})

	t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero0" {
			t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("state_recovery_verify_ip_rules_created", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src != nil {
			t.Fatalf("rule 100 should be sourced from all; got %s", rules[0].Src)
		}
		if rules[0].Dst.String() != "3.0.0.0/24" {
			t.Fatalf("rule 100 should be destined to 3.0.0.0/24; got %s", rules[0].Dst)
		}
		if rules[0].Table != 100 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching ip rules: %v", err)
		}
		if rules[0].Src.String() != "3.0.0.0/24" {
			t.Fatalf("rule 101 should be sourced from 3.0.0.0")
		}
		if rules[0].Dst != nil {
			t.Fatalf("rule 101 should be destined too all; got %s", rules[0].Dst)
		}
		if rules[0].Table != 101 {
			t.Fatalf("rule 100 should be looked up in table 100; got %d", rules[0].Table)
		}
	})

	t.Run("state_recovery_verify_default_route_created", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if !route[0].Src.Equal(net.IP{3, 3, 3, 3}) {
			t.Fatalf("route src hint should be 3.3.3.3; got %s", route[0].Src)
		}
		if route[0].Dst.String() != "0.0.0.0/0" {
			t.Fatalf("route dst should be 0.0.0.0/0; got %s", route[0].Dst)
		}
		if !route[0].Gw.Equal(net.IP{169, 254, 0, 0}) {
			t.Fatalf("route gw should be 169.254.0.0; got %s", route[0].Gw)
		}
	})
	// TODO: verify specific routes are created; this needs namespaces

	// case: remove tunnel
	t.Run("send_remove_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "remove")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("error during request: %v", err)
		}
		defer resp.Body.Close()

		got, _ := io.ReadAll(resp.Body)
		want := `{"status": "ok"}`
		if string(got) != want {
			t.Fatalf("wrong response: %s", string(got))
		}
	})

	t.Run("verify_tunnel_is_removed", func(t *testing.T) {
		_, err := nl.LinkByName("doublezero0")
		if !errors.As(err, &nl.LinkNotFoundError{}) {
			t.Fatalf("expected LinkNotFoundError; got: %v", err)
		}
	})

	t.Run("verify_ip_rules_are_removed", func(t *testing.T) {
		// ip rule 100: from all to 3.0.0.0/24 table 100
		rules, err := nl.RuleListFiltered(0, &nl.Rule{Priority: 100}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching priority 100 rules: %v", err)
		}
		if len(rules) != 0 {
			t.Fatalf("wanted 0 rules found at priority 100; got %d: %+v", len(rules), rules)
		}
		// ip rule 100: from 3.0.0.0/24 to all table 101
		rules, err = nl.RuleListFiltered(0, &nl.Rule{Priority: 101}, nl.RT_FILTER_PRIORITY)
		if err != nil {
			t.Fatalf("error fetching priority 101 rules: %v", err)
		}
		if len(rules) != 0 {
			t.Fatalf("wanted 0 rules found at priority 101; got %d: %+v", len(rules), rules)
		}
	})

	t.Run("verify_default_route_is_removed", func(t *testing.T) {
		route, err := nl.RouteListFiltered(0, &nl.Route{Table: 101}, nl.RT_FILTER_TABLE)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if len(route) != 0 {
			t.Fatalf("wanted 0 routes found in table 101; got %d: %+v", len(route), route)
		}
	})

	// TODO: verify specific routes are removed; this needs namespaces

	t.Run("state_removal_stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			log.Fatalf("timed out waiting for close")
		}
	})

	t.Run("state_removal_verify_state_file_removed", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(rootPath, "doublezerod", "doublezerod.json")); err == nil {
			t.Fatalf("state file still exists when should be removed")
		}
	})

	// case: latency endpoint
	// TODO: call latency endpoint
	// TODO: verify latency samples are returned
}
