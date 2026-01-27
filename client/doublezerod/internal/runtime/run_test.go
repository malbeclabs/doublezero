//go:build !race && container_tests

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
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/gopacket"
	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/pim"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/runtime"
	"github.com/malbeclabs/doublezero/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

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
	nexthop := gobgp.NewPathAttributeNextHop("169.254.0.0")
	param := gobgp.NewAs4PathParam(2, []uint32{65001})
	aspath := gobgp.NewPathAttributeAsPath([]gobgp.AsPathParamInterface{param})
	update := gobgp.NewBGPUpdateMessage(
		[]*gobgp.IPAddrPrefix{},
		[]gobgp.PathAttributeInterface{origin, nexthop, aspath},
		[]*gobgp.IPAddrPrefix{
			gobgp.NewIPAddrPrefix(32, "5.5.5.5"),
			gobgp.NewIPAddrPrefix(32, "4.4.4.4"),
		},
	)
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

func TestEndToEnd_IBRL_Basic(t *testing.T) {
	runIBRLTest(t, api.UserTypeIBRL, map[string]any{

		"tunnel_src":     "192.168.1.0",
		"tunnel_dst":     "192.168.1.1",
		"tunnel_net":     "169.254.0.0/31",
		"doublezero_ip":  "192.168.1.0",
		"user_type":      "IBRL",
		"bgp_local_asn":  65000,
		"bgp_remote_asn": 65342,
	}, "./fixtures/doublezerod.ibrl.json")
}

func TestEndToEnd_IBRL_WithAllocatedIP(t *testing.T) {
	runIBRLTest(t, api.UserTypeIBRLWithAllocatedIP, map[string]any{

		"tunnel_src":     "192.168.1.0",
		"tunnel_dst":     "192.168.1.1",
		"tunnel_net":     "169.254.0.0/31",
		"doublezero_ip":  "192.168.1.0",
		"user_type":      "IBRLWithAllocatedIP",
		"bgp_local_asn":  65000,
		"bgp_remote_asn": 65342,
	}, "./fixtures/doublezerod.ibrl.with.allocated.ip.json")
}

func runIBRLTest(t *testing.T, userType api.UserType, provisioningRequest map[string]any, goldenStateFile string) {
	teardown, err := setupTest(t)
	rootPath := os.Getenv("XDG_STATE_HOME")
	t.Cleanup(teardown)
	defer os.RemoveAll(rootPath)
	if err != nil {
		t.Fatalf("%v\n", err)
	}

	// TODO: start corebgp instance in network namespace
	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		peerNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			t.Logf("error creating namespace: %v", err)
		}
		if err = netns.Set(peerNS); err != nil {
			t.Logf("error setting namespace: %v", err)
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

		t.Log("starting bgp server")
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}
	}()

	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("IBRL", func(t *testing.T) {
		sockFile := filepath.Join(rootPath, "doublezerod.sock")
		go func() {
			err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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
			body, _ := json.Marshal(provisioningRequest)
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

		t.Run("verify_routes_are_installed", func(t *testing.T) {
			time.Sleep(5 * time.Second)
			got, err := nl.RouteListFiltered(nl.FAMILY_V4, &nl.Route{Protocol: unix.RTPROT_BGP}, nl.RT_FILTER_PROTOCOL)
			if err != nil {
				t.Fatalf("error fetching routes: %v", err)
			}
			tun, err := nl.LinkByName("doublezero0")
			if err != nil {
				t.Fatalf("error fetching tunnel info: %v", err)
			}
			want := []nl.Route{
				{
					LinkIndex: tun.Attrs().Index,
					Table:     254,
					Dst: &net.IPNet{
						IP:   net.IP{4, 4, 4, 4},
						Mask: net.IPv4Mask(255, 255, 255, 255),
					},
					Gw:       net.IP{169, 254, 0, 0},
					Protocol: unix.RTPROT_BGP,
					Src:      net.IP{192, 168, 1, 0},
					Family:   nl.FAMILY_V4,
					Type:     syscall.RTN_UNICAST,
				},
				{
					LinkIndex: tun.Attrs().Index,
					Table:     254,
					Dst: &net.IPNet{
						IP:   net.IP{5, 5, 5, 5},
						Mask: net.IPv4Mask(255, 255, 255, 255),
					},
					Gw:       net.IP{169, 254, 0, 0},
					Protocol: unix.RTPROT_BGP,
					Src:      net.IP{192, 168, 1, 0},
					Family:   nl.FAMILY_V4,
					Type:     syscall.RTN_UNICAST,
				},
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Route mismatch (-want +got): %s\n", diff)
			}
		})

		t.Run("verify_state_file_is_created", func(t *testing.T) {
			got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
			if err != nil {
				t.Fatalf("error reading state file: %v", err)
			}
			want, err := os.ReadFile(goldenStateFile)
			if err != nil {
				t.Fatalf("error reading state file: %v", err)
			}
			if diff := cmp.Diff(string(want), string(got)); diff != "" {
				t.Fatalf("State mismatch (-want +got): %s\n", diff)
			}
		})

		t.Run("verify_routes_flushed_on_session_down_event", func(t *testing.T) {
			if userType == api.UserTypeIBRLWithAllocatedIP {
				t.Skip("we don't flush routes in IBRLWithAllocatedIP mode")
			}

			if err := srv.DeletePeer(netip.AddrFrom4([4]byte{169, 254, 0, 1})); err != nil {
				t.Fatalf("error deleting peer: %v", err)
			}
			// wait for peer status to be deleted
			down, err := waitForPeerStatus(httpClient, userType, bgp.SessionStatusPending, 10*time.Second)
			if err != nil {
				t.Fatalf("error while waiting for peer status: %v", err)
			}
			if !down {
				t.Fatalf("timed out waiting for peer status of pending")
			}
			// should not have any routes tagged bgp
			got, err := nl.RouteListFiltered(nl.FAMILY_V4, &nl.Route{Protocol: unix.RTPROT_BGP}, nl.RT_FILTER_PROTOCOL)
			if err != nil {
				t.Fatalf("error fetching routes: %v", err)
			}
			if len(got) > 0 {
				t.Fatalf("expected no routes, got %d, %+v\n", len(got), got)
			}

			// 	re-add peer
			d := &dummyPlugin{}
			err = srv.AddPeer(corebgp.PeerConfig{
				RemoteAddress: netip.MustParseAddr("169.254.0.1"),
				LocalAS:       65342,
				RemoteAS:      65000,
			}, d, corebgp.WithPassive())
			if err != nil {
				log.Fatalf("error creating dummy bgp server: %v", err)
			}

			// wait for peer status to come back up
			up, err := waitForPeerStatus(httpClient, userType, bgp.SessionStatusUp, 10*time.Second)
			if err != nil {
				t.Fatalf("error while waiting for peer status: %v", err)
			}
			if !up {
				t.Fatalf("timed out waiting for peer status of pending")
			}
			// ensure that 4.4.4.4,3.3.3.3 are added and tagged with bgp (unix.RTPROT_BGP)
			got, err = nl.RouteListFiltered(nl.FAMILY_V4, &nl.Route{Protocol: unix.RTPROT_BGP}, nl.RT_FILTER_PROTOCOL)
			if err != nil {
				t.Fatalf("error fetching routes: %v", err)
			}
			tun, err := nl.LinkByName("doublezero0")
			if err != nil {
				t.Fatalf("error fetching tunnel info: %v", err)
			}
			want := []nl.Route{
				{
					LinkIndex: tun.Attrs().Index,
					Table:     254,
					Dst: &net.IPNet{
						IP:   net.IP{4, 4, 4, 4},
						Mask: net.IPv4Mask(255, 255, 255, 255),
					},
					Gw:       net.IP{169, 254, 0, 0},
					Protocol: unix.RTPROT_BGP,
					Src:      net.IP{192, 168, 1, 0},
					Family:   nl.FAMILY_V4,
					Type:     syscall.RTN_UNICAST,
				},
				{
					LinkIndex: tun.Attrs().Index,
					Table:     254,
					Dst: &net.IPNet{
						IP:   net.IP{5, 5, 5, 5},
						Mask: net.IPv4Mask(255, 255, 255, 255),
					},
					Gw:       net.IP{169, 254, 0, 0},
					Protocol: unix.RTPROT_BGP,
					Src:      net.IP{192, 168, 1, 0},
					Family:   nl.FAMILY_V4,
					Type:     syscall.RTN_UNICAST,
				},
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("Route mismatch (-want +got): %s\n", diff)
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
			err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(fmt.Sprintf(`{"user_type": "%s"}`, userType)))
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
			path, _ := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))

			var p []*api.ProvisionRequest
			if err := json.Unmarshal(path, &p); err != nil {
				t.Errorf("error unmarshaling db file: %v", err)
			}

			if len(p) != 0 {
				t.Fatalf("provisioned requests should be empty; got %+v", p)

			}
		})
	})
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
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
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
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(fmt.Sprintf(`{"user_type": "%s"}`, api.UserTypeEdgeFiltering)))
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
		path, _ := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))

		var p []*api.ProvisionRequest
		if err := json.Unmarshal(path, &p); err != nil {
			t.Errorf("error unmarshaling db file: %v", err)
		}

		if len(p) != 0 {
			t.Fatalf("provisioned requests should be empty; got %+v %s", p, string(path))
		}
	})

	// case: latency endpoint
	// TODO: call latency endpoint
	// TODO: verify latency samples are returned
}

func TestMulticastPublisher(t *testing.T) {
	teardown, err := setupTest(t)
	rootPath := os.Getenv("XDG_STATE_HOME")
	t.Cleanup(teardown)
	defer os.RemoveAll(rootPath)
	if err != nil {
		t.Fatalf("%v\n", err)
	}

	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		peerNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			t.Logf("error creating namespace: %v", err)
		}
		if err = netns.Set(peerNS); err != nil {
			t.Logf("error setting namespace: %v", err)
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

		t.Log("starting bgp server")
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}
	}()

	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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
					"tunnel_src":     "192.168.1.0",
					"tunnel_dst":     "192.168.1.1",
					"tunnel_net":     "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": [],
					"user_type": "Multicast",
					"mcast_pub_groups": ["239.0.0.1"],
					"bgp_local_asn":  65000,
					"bgp_remote_asn": 65342
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
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
			t.Fatalf("tunnel name is not doublezero0: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("verify_doublezero_ip_is_added_to_tunnel", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel info: %v", err)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}
		want, err := nl.ParseAddr("3.3.3.3/32 doublezero1")
		if err != nil {
			t.Fatalf("error parsing doublezero ip: %v", err)
		}
		for _, addr := range addrs {
			if addr.Equal(*want) {
				return // found the doublezero ip
			}
		}
		t.Fatalf("addr 3.3.3.3/32 not found on tunnel doublezero1")
	})

	t.Run("verify_routes_are_installed", func(t *testing.T) {
		got, err := nl.RouteListFiltered(nl.FAMILY_V4, &nl.Route{Dst: &net.IPNet{IP: net.IP{239, 0, 0, 1}, Mask: net.IPv4Mask(255, 255, 255, 255)}}, nl.RT_FILTER_DST)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel info: %v", err)
		}
		want := []nl.Route{
			{
				LinkIndex: tun.Attrs().Index,
				Table:     254,
				Dst: &net.IPNet{
					IP:   net.IP{239, 0, 0, 1},
					Mask: net.IPv4Mask(255, 255, 255, 255),
				},
				Gw:       net.IP{169, 254, 0, 0},
				Protocol: unix.RTPROT_STATIC,
				Src:      net.IP{3, 3, 3, 3},
				Family:   nl.FAMILY_V4,
				Type:     syscall.RTN_UNICAST,
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("Route mismatch (-want +got): %s\n", diff)
		}
	})

	t.Run("verify_state_file_is_created", func(t *testing.T) {
		got, err := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		want, err := os.ReadFile("./fixtures/doublezerod.mcast_publisher.json")
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("State mismatch (-want +got): %s\n", diff)
		}
	})

	t.Run("verify_bgp_session_is_up", func(t *testing.T) {
		up, err := waitForPeerStatus(httpClient, api.UserTypeMulticast, bgp.SessionStatusUp, 10*time.Second)
		if err != nil {
			t.Fatalf("error while waiting for peer status: %v", err)
		}
		if !up {
			t.Fatalf("timed out waiting for peer status of up")
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
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
		}
	})

	t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
			t.Fatalf("tunnel name is not doublezero1: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}
		for _, addr := range addrs {
			if addr.String() == "239.0.0.1/32 doublezero1" {
				if addr.Flags&unix.IFA_F_MCAUTOJOIN != 0x400 {
					t.Fatalf("expected to find 0x400, got %x", addr.Flags&unix.IFA_F_MCAUTOJOIN)
				}
			}
		}
	})

	t.Run("send_remove_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "remove")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(fmt.Sprintf(`{"user_type": "%s"}`, api.UserTypeMulticast)))
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
		_, err := nl.LinkByName("doublezero1")
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
		path, _ := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))

		var p []*api.ProvisionRequest
		if err := json.Unmarshal(path, &p); err != nil {
			t.Errorf("error unmarshaling db file: %v", err)
		}

		if len(p) != 0 {
			t.Fatalf("provisioned requests should be empty; got %+v", p)

		}
	})

}

func TestMulticastSubscriber(t *testing.T) {
	teardown, err := setupTest(t)
	rootPath := os.Getenv("XDG_STATE_HOME")
	t.Cleanup(teardown)
	defer os.RemoveAll(rootPath)
	if err != nil {
		t.Fatalf("error setting up test: %v", err)
	}

	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		peerNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			t.Logf("error creating namespace: %v", err)
		}
		if err = netns.Set(peerNS); err != nil {
			t.Logf("error setting namespace: %v", err)
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

		t.Log("starting bgp server")
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}
	}()

	pimJoinPruneChan := make(chan []byte, 1)
	pimHelloChan := make(chan []byte, 1)

	// start pim receiver in network namespace;
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		pimNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			log.Fatalf("error creating namespace: %v", err)
		}
		if err = netns.Set(pimNS); err != nil {
			log.Fatalf("error setting namespace: %v", err)
		}

		tun, err := net.InterfaceByName("doublezero0")
		if err != nil {
			log.Fatalf("error getting eth0 interface: %v", err)
		}
		c, err := net.ListenPacket("ip4:103", "0.0.0.0")
		if err != nil {
			log.Fatalf("error creating listener: %v", err)
		}
		defer c.Close()
		p := ipv4.NewPacketConn(c)
		if err := p.JoinGroup(tun, &net.IPAddr{IP: net.IP{224, 0, 0, 13}}); err != nil {
			log.Fatalf("error joining multicast group: %v", err)
		}

		for {
			b := make([]byte, 1500)
			n, _, _, err := p.ReadFrom(b)
			if err != nil {
				log.Printf("error reading from packet conn: %v", err)
				continue
			}
			if n == 0 {
				log.Printf("received empty message")
				continue
			}

			pimType := b[0] & 0x0f // PIM message type is in the first byte, lower 4 bits
			switch pimType {
			case pim.Hello:
				pimHelloChan <- b[:n] // send the hello message to the channel
				log.Printf("received PIM hello message: %x", b[:n])
			case pim.JoinPrune:
				pimJoinPruneChan <- b[:n] // send the join message to the channel
				log.Printf("received PIM join message: %x", b[:n])
			default:
				log.Printf("received unknown PIM message type: %x", b[0])
				// ignore unknown message types
			}
		}
	}()

	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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
		body := `{
					"tunnel_src":     "192.168.1.0",
					"tunnel_dst":     "192.168.1.1",
					"tunnel_net":     "169.254.0.0/31",
					"doublezero_ip": "3.3.3.3",
					"doublezero_prefixes": [],
					"user_type": "Multicast",
					"mcast_sub_groups": ["239.0.0.1"],
					"bgp_local_asn":  65000,
					"bgp_remote_asn": 65342
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
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
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
		want, err := os.ReadFile("./fixtures/doublezerod.mcast_subscriber.json")
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("State mismatch (-want +got): %s\n", diff)
		}
	})

	t.Run("verify_bgp_session_is_up", func(t *testing.T) {
		up, err := waitForPeerStatus(httpClient, api.UserTypeMulticast, bgp.SessionStatusUp, 10*time.Second)
		if err != nil {
			t.Fatalf("error while waiting for peer status: %v", err)
		}
		if !up {
			t.Fatalf("timed out waiting for peer status of up")
		}
	})

	t.Run("verify_pim_hello_message_sent", func(t *testing.T) {
		var msg []byte
		// Wait for a PIM Hello message
		select {
		case msg = <-pimHelloChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM Hello message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.Hello,
					Checksum: 0x4317,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.HelloMessageType).(*pim.HelloMessage); ok {
			want := &pim.HelloMessage{
				Holdtime:     105,
				DRPriority:   1,
				GenerationID: 3614426332,
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.HelloMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("HelloMessage mismatch (-got +want):\n%s", diff)
			}
		}
	})
	t.Run("verify_pim_join_message_sent", func(t *testing.T) {
		// Verify join message is received
		var msg []byte
		select {
		case msg = <-pimJoinPruneChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM Join message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.JoinPrune,
					Checksum: 0x2f45,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
			want := &pim.JoinPruneMessage{
				UpstreamNeighborAddress: net.IP([]byte{169, 254, 0, 0}),
				NumGroups:               1,
				Reserved:                0,
				Holdtime:                120,
				Groups: []pim.Group{{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      1,
					NumPrunedSources:      0,
					MaskLength:            32,
					MulticastGroupAddress: net.IP{239, 0, 0, 1},
					Joins: []pim.SourceAddress{{
						AddressFamily: 1,
						Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
						MaskLength:    32,
						EncodingType:  0,
						Address:       pim.RpAddress,
					},
					},
					Prunes: []pim.SourceAddress{},
				}},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
			}
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

	t.Run("verify_prune_message_sent", func(t *testing.T) {
		// Verify prune message is received
		var msg []byte
		select {
		case msg = <-pimJoinPruneChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM prune message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.JoinPrune,
					Checksum: 0x2fb8,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
			want := &pim.JoinPruneMessage{
				UpstreamNeighborAddress: net.IP([]byte{169, 254, 0, 0}),
				NumGroups:               1,
				Reserved:                0,
				Holdtime:                5,
				Groups: []pim.Group{{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      0,
					NumPrunedSources:      1,
					MaskLength:            32,
					MulticastGroupAddress: net.IP{239, 0, 0, 1},
					Joins:                 []pim.SourceAddress{},
					Prunes: []pim.SourceAddress{{
						AddressFamily: 1,
						Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
						MaskLength:    32,
						EncodingType:  0,
						Address:       pim.RpAddress}}}}}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
			}
		}
	})

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
		}
	})

	t.Run("state_recovery_verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
			t.Fatalf("tunnel name is not doublezero1: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}
		for _, addr := range addrs {
			if addr.String() == "239.0.0.1/32 doublezero1" {
				if addr.Flags&unix.IFA_F_MCAUTOJOIN != 0x400 {
					t.Fatalf("expected to find 0x400, got %x", addr.Flags&unix.IFA_F_MCAUTOJOIN)
				}
			}
		}
	})

	t.Run("send_remove_request", func(t *testing.T) {
		url, err := url.JoinPath("http://localhost/", "remove")
		if err != nil {
			t.Fatalf("error creating url: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(fmt.Sprintf(`{"user_type": "%s"}`, api.UserTypeMulticast)))
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
		path, _ := os.ReadFile(filepath.Join(rootPath, "doublezerod", "doublezerod.json"))

		var p []*api.ProvisionRequest
		if err := json.Unmarshal(path, &p); err != nil {
			t.Errorf("error unmarshaling db file: %v", err)
		}

		if len(p) != 0 {
			t.Fatalf("provisioned requests should be empty; got %+v", p)
		}
	})
}

func TestServiceCoexistence(t *testing.T) {
	teardown, err := setupTest(t)
	rootPath := os.Getenv("XDG_STATE_HOME")
	t.Cleanup(teardown)
	defer os.RemoveAll(rootPath)
	if err != nil {
		t.Fatalf("error setting up test: %v", err)
	}

	srv, _ := corebgp.NewServer(netip.MustParseAddr("2.2.2.2"))
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		peerNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			t.Logf("error creating namespace: %v", err)
		}
		if err = netns.Set(peerNS); err != nil {
			t.Logf("error setting namespace: %v", err)
		}

		// start bgp instance in network namespace
		d := &dummyPlugin{}

		// add IBRL peer
		err = srv.AddPeer(corebgp.PeerConfig{
			RemoteAddress: netip.MustParseAddr("169.254.0.1"),
			LocalAS:       65342,
			RemoteAS:      65000,
		}, d, corebgp.WithPassive())
		if err != nil {
			log.Fatalf("error creating dummy bgp server: %v", err)
		}
		// add multicast subscriber peer
		err = srv.AddPeer(corebgp.PeerConfig{
			RemoteAddress: netip.MustParseAddr("169.254.1.1"),
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

		t.Log("starting bgp server")
		if err := srv.Serve([]net.Listener{dlis}); err != nil {
			t.Logf("error on remote peer bgp server: %v", err)
		}
	}()

	pimJoinPruneChan := make(chan []byte, 1)
	pimHelloChan := make(chan []byte, 1)

	// start pim receiver in network namespace;
	go func() {
		rt.LockOSThread()
		defer rt.UnlockOSThread()

		pimNS, err := netns.GetFromName("doublezero-peer")
		if err != nil {
			log.Fatalf("error creating namespace: %v", err)
		}
		if err = netns.Set(pimNS); err != nil {
			log.Fatalf("error setting namespace: %v", err)
		}

		tun, err := net.InterfaceByName("doublezero1")
		if err != nil {
			log.Fatalf("error getting eth0 interface: %v", err)
		}
		c, err := net.ListenPacket("ip4:103", "0.0.0.0")
		if err != nil {
			log.Fatalf("error creating listener: %v", err)
		}
		defer c.Close()
		p := ipv4.NewPacketConn(c)
		if err := p.JoinGroup(tun, &net.IPAddr{IP: net.IP{224, 0, 0, 13}}); err != nil {
			log.Fatalf("error joining multicast group: %v", err)
		}

		for {
			b := make([]byte, 1500)
			n, _, _, err := p.ReadFrom(b)
			if err != nil {
				log.Printf("error reading from packet conn: %v", err)
				continue
			}
			if n == 0 {
				log.Printf("received empty message")
				continue
			}

			pimType := b[0] & 0x0f // PIM message type is in the first byte, lower 4 bits
			switch pimType {
			case pim.Hello:
				pimHelloChan <- b[:n] // send the hello message to the channel
				log.Printf("received PIM hello message: %x", b[:n])
			case pim.JoinPrune:
				pimJoinPruneChan <- b[:n] // send the join message to the channel
				log.Printf("received PIM join message: %x", b[:n])
			default:
				log.Printf("received unknown PIM message type: %x", b[0])
				// ignore unknown message types
			}
		}
	}()

	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
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

	t.Run("provision_ibrl_tunnel", func(t *testing.T) {
		req := `{
					"tunnel_src":     "192.168.1.0",
					"tunnel_dst":     "192.168.1.1",
					"tunnel_net":     "169.254.0.0/31",
					"doublezero_ip": "192.168.1.0",
					"doublezero_prefixes": [],
					"user_type": "IBRL",
					"mcast_sub_groups": [],
					"mcast_pub_groups": [],
					"bgp_local_asn":  65000,
					"bgp_remote_asn": 65342
				}`
		if err := sendClientRequest(httpClient, "provision", req); err != nil {
			t.Fatalf("error sending provision request: %v", err)
		}
	})

	t.Run("provision_multicast_subscriber_tunnel", func(t *testing.T) {
		req := `{
					"tunnel_src":     "192.168.1.0",
					"tunnel_dst":     "192.168.2.1",
					"tunnel_net":     "169.254.1.0/31",
					"doublezero_ip": "",
					"doublezero_prefixes": [],
					"user_type": "Multicast",
					"mcast_sub_groups": ["239.0.0.1"],
					"mcast_pub_groups": [],
					"bgp_local_asn":  65000,
					"bgp_remote_asn": 65342
				}`
		if err := sendClientRequest(httpClient, "provision", req); err != nil {
			t.Fatalf("error sending provision request: %v", err)
		}
	})

	t.Run("verify_ibrl_state", func(t *testing.T) {
		verifyTunnelIsUp(t, "doublezero0")
		verifyBgpSessionIsUp(t, httpClient, api.UserTypeIBRL)

		want := []nl.Route{
			{
				Table: 254,
				Dst: &net.IPNet{
					IP:   net.IP{4, 4, 4, 4},
					Mask: net.IPv4Mask(255, 255, 255, 255),
				},
				Gw:       net.IP{169, 254, 0, 0},
				Protocol: unix.RTPROT_BGP,
				Src:      net.IP{192, 168, 1, 0},
				Family:   nl.FAMILY_V4,
				Type:     syscall.RTN_UNICAST,
			},
			{
				Table: 254,
				Dst: &net.IPNet{
					IP:   net.IP{5, 5, 5, 5},
					Mask: net.IPv4Mask(255, 255, 255, 255),
				},
				Gw:       net.IP{169, 254, 0, 0},
				Protocol: unix.RTPROT_BGP,
				Src:      net.IP{192, 168, 1, 0},
				Family:   nl.FAMILY_V4,
				Type:     syscall.RTN_UNICAST,
			},
		}
		verifyBgpRoutesAreInstalled(t, "doublezero0", want)
	})

	t.Run("verify_multicast_state", func(t *testing.T) {
		verifyTunnelIsUp(t, "doublezero1")
		verifyBgpSessionIsUp(t, httpClient, api.UserTypeMulticast)
		verifyPimHelloMessageSent(t, pimHelloChan)
		verifyPimJoinMessageSent(t, pimJoinPruneChan, net.IP([]byte{169, 254, 1, 0}))
	})

	t.Run("verify_state_file_is_created", func(t *testing.T) {
		verifyStateFileMatches(t, rootPath, "./fixtures/doublezerod.ibrl_w_mcast_subscriber.json")
	})

	t.Run("stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for close")
		}
	})

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
		errChan <- err
	}()

	<-time.After(5 * time.Second)

	t.Run("restart_runtime", func(t *testing.T) {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error starting runtime: %v", err)
			}
		case <-time.After(10 * time.Second):
		}
	})

	t.Run("verify_ibrl_state_after_restart", func(t *testing.T) {
		verifyTunnelIsUp(t, "doublezero0")
		verifyBgpSessionIsUp(t, httpClient, api.UserTypeIBRL)

		want := []nl.Route{
			{
				Table: 254,
				Dst: &net.IPNet{
					IP:   net.IP{4, 4, 4, 4},
					Mask: net.IPv4Mask(255, 255, 255, 255),
				},
				Gw:       net.IP{169, 254, 0, 0},
				Protocol: unix.RTPROT_BGP,
				Src:      net.IP{192, 168, 1, 0},
				Family:   nl.FAMILY_V4,
				Type:     syscall.RTN_UNICAST,
			},
			{
				Table: 254,
				Dst: &net.IPNet{
					IP:   net.IP{5, 5, 5, 5},
					Mask: net.IPv4Mask(255, 255, 255, 255),
				},
				Gw:       net.IP{169, 254, 0, 0},
				Protocol: unix.RTPROT_BGP,
				Src:      net.IP{192, 168, 1, 0},
				Family:   nl.FAMILY_V4,
				Type:     syscall.RTN_UNICAST,
			},
		}
		verifyBgpRoutesAreInstalled(t, "doublezero0", want)
	})

	t.Run("verify_multicast_state_after_restart", func(t *testing.T) {
		verifyTunnelIsUp(t, "doublezero1")
		verifyBgpSessionIsUp(t, httpClient, api.UserTypeMulticast)
		verifyPruneMessageSent(t, pimJoinPruneChan, net.IP([]byte{169, 254, 1, 0}))
		verifyPimHelloMessageSent(t, pimHelloChan)
		verifyPimJoinMessageSent(t, pimJoinPruneChan, net.IP([]byte{169, 254, 1, 0}))
	})

	t.Run("remove_ibrl_subscriber_tunnel", func(t *testing.T) {
		body := fmt.Sprintf(`{"user_type": "%s"}`, api.UserTypeIBRL)
		if err := sendClientRequest(httpClient, "remove", body); err != nil {
			t.Fatalf("error sending remove request: %v", err)
		}
	})

	t.Run("verify_ibrl_tunnel_is_removed", func(t *testing.T) {
		verifyTunnelIsRemoved(t, "doublezero0")
	})

	t.Run("verify_ibrl_state_removed", func(t *testing.T) {
		verifyStateFileMatches(t, rootPath, "./fixtures/doublezerod.ibrl_w_mcast_subscriber_ibrl_removed.json")
	})

	t.Run("remove_multicast_subscriber_tunnel", func(t *testing.T) {
		body := fmt.Sprintf(`{"user_type": "%s"}`, api.UserTypeMulticast)
		if err := sendClientRequest(httpClient, "remove", body); err != nil {
			t.Fatalf("error sending remove request: %v", err)
		}
	})

	t.Run("verify_multicast_tunnel_is_removed", func(t *testing.T) {
		verifyTunnelIsRemoved(t, "doublezero1")
	})

	t.Run("verify_multicast_state_removed", func(t *testing.T) {
		verifyStateFileMatches(t, rootPath, "./fixtures/doublezerod.empty.json")
	})

	t.Run("stop_runtime", func(t *testing.T) {
		cancel()
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error stopping runtime: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for close")
		}
	})
}

func TestRuntime_Run_ReturnsOnContextCancel(t *testing.T) {
	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	rootPath, err := os.MkdirTemp("", "doublezerod")
	require.NoError(t, err)
	defer os.RemoveAll(rootPath)
	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, newTestLivenessManagerConfig())
		errChan <- err
	}()

	// Give the runtime a moment to start, then cancel the context to force exit.
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(300 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for runtime to exit after context cancel")
	}
}

func TestRuntime_Run_PropagatesLivenessStartupError(t *testing.T) {
	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	rootPath, err := os.MkdirTemp("", "doublezerod")
	require.NoError(t, err)
	defer os.RemoveAll(rootPath)
	t.Setenv("XDG_STATE_HOME", rootPath)

	// Invalid liveness config (port < 0) -> NewManager.Validate() error.
	bad := *newTestLivenessManagerConfig()
	bad.Port = -1

	sockFile := filepath.Join(rootPath, "doublezerod.sock")
	go func() {
		err := runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, &bad)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		require.Error(t, err)
		require.Contains(t, err.Error(), "port must be greater than or equal to 0")
	case <-time.After(5 * time.Second):
		t.Fatalf("expected startup error from runtime.Run with bad liveness config")
	}
}

func TestRuntime_Run_PropagatesLivenessError_FromUDPClosure(t *testing.T) {
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Minimal state dir + socket path
	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(rootPath)
	t.Setenv("XDG_STATE_HOME", rootPath)
	sockFile := filepath.Join(rootPath, "doublezerod.sock")

	// Create a real UDPService we can close to induce a receiver error.
	udp, err := liveness.ListenUDP("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}

	// Build a liveness config that uses our injected UDP service.
	cfg := newTestLivenessManagerConfig()
	cfg.UDP = udp
	cfg.PassiveMode = true

	// Start the runtime.
	go func() {
		errCh <- runtime.Run(ctx, sockFile, "", false, false, newTestNetworkConfig(t), 30, 30, cfg)
	}()

	// Give the liveness receiver a moment to start, then close the UDP socket.
	time.Sleep(200 * time.Millisecond)
	_ = udp.Close()

	// The receiver should error, Manager should send on lm.Err(), and Run should return that error.
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected non-nil error propagated from liveness manager, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for runtime to return error from liveness manager")
	}
}

func setupTest(t *testing.T) (func(), error) {
	abortIfLinksAreUp(t)
	rootPath, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", rootPath)

	path := filepath.Join(rootPath, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	cmds := [][]string{
		{"ip", "netns", "add", "doublezero-peer"},
		// setup veth pair 1
		{"ip", "link", "add", "veth0", "type", "veth", "peer", "name", "veth1"},
		{"ip", "link", "set", "dev", "veth1", "netns", "doublezero-peer"},
		{"ip", "addr", "add", "192.168.1.0/31", "dev", "veth0"},
		{"ip", "link", "set", "dev", "veth0", "up"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "192.168.1.1/31", "dev", "veth1"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "veth1", "up"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "tunnel", "add", "doublezero0", "mode", "gre", "local", "192.168.1.1", "remote", "192.168.1.0", "ttl", "64"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "169.254.0.0/31", "dev", "doublezero0"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "doublezero0", "up"},
		// setup veth pair 2
		{"ip", "link", "add", "veth2", "type", "veth", "peer", "name", "veth3"},
		{"ip", "link", "set", "dev", "veth3", "netns", "doublezero-peer"},
		{"ip", "addr", "add", "192.168.2.0/31", "dev", "veth2"},
		{"ip", "link", "set", "dev", "veth2", "up"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "192.168.2.1/31", "dev", "veth3"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "veth3", "up"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "tunnel", "add", "doublezero1", "mode", "gre", "local", "192.168.2.1", "remote", "192.168.1.0", "ttl", "64"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "addr", "add", "169.254.1.0/31", "dev", "doublezero1"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "link", "set", "dev", "doublezero1", "up"},
		{"ip", "addr", "list"},
		{"ip", "netns", "exec", "doublezero-peer", "ip", "addr", "list"},
	}

	for _, cmd := range cmds {
		_, err := execSysCommand(cmd, t)
		if err != nil {
			return nil, err
		}
	}

	teardown := func() {
		cmds := [][]string{
			{"ip", "link", "del", "veth0"},
			{"ip", "link", "del", "veth2"},
			{"ip", "netns", "del", "doublezero-peer"},
		}

		for _, cmd := range cmds {
			_, err := execSysCommand(cmd, t)
			if err != nil {
				log.Printf("error executing teardown command %s: %v", strings.Join(cmd, " "), err)
			}
		}
	}
	return teardown, nil
}

func execSysCommand(cmdSlice []string, t *testing.T) ([]byte, error) {
	cmd := exec.Command(cmdSlice[0], cmdSlice[1:]...)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	t.Logf("%s output: %s", strings.Join(cmdSlice, " "), string(stdout))
	return stdout, nil
}

func waitForPeerStatus(httpClient http.Client, userType api.UserType, status bgp.SessionStatus, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		url, err := url.JoinPath("http://localhost/", "status")
		if err != nil {
			return false, fmt.Errorf("error creating url: %v", err)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, fmt.Errorf("error creating request: %v", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("error during request: %v", err)
		}
		defer resp.Body.Close()

		got, err := io.ReadAll(resp.Body)
		if err != nil {
			return false, fmt.Errorf("error reading status response: %v", err)
		}
		var statusResponses []api.StatusResponse
		if err := json.Unmarshal(got, &statusResponses); err != nil {
			return false, fmt.Errorf("error unmarshalling status response: %v", err)
		}
		for _, statusResponse := range statusResponses {
			if statusResponse.UserType != userType {
				continue
			}
			if statusResponse.DoubleZeroStatus.SessionStatus == status {
				return true, nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
	return false, nil
}

func sendClientRequest(httpClient http.Client, endpoint, body string) error {
	url, err := url.JoinPath("http://localhost/", endpoint)
	if err != nil {
		return fmt.Errorf("error creating url: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error during request: %v", err)
	}
	defer resp.Body.Close()

	got, _ := io.ReadAll(resp.Body)
	want := `{"status": "ok"}`
	if string(got) != want {
		return fmt.Errorf("wrong response: %s", string(got))
	}
	return nil
}

func verifyTunnelIsUp(t *testing.T, tunName string) {
	t.Run("verify_tunnel_is_up", func(t *testing.T) {
		tun, err := nl.LinkByName(tunName)
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state (6), got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})
}

func verifyTunnelIsRemoved(t *testing.T, tunName string) {
	_, err := nl.LinkByName(tunName)
	if !errors.As(err, &nl.LinkNotFoundError{}) {
		t.Fatalf("expected LinkNotFoundError; got: %v", err)
	}
}

// verifyBgpRoutesAreInstalled checks if the BGP routes are installed in the kernel routing table.
func verifyBgpRoutesAreInstalled(t *testing.T, tunName string, want []nl.Route) {
	t.Run("verify_routes_are_installed", func(t *testing.T) {
		time.Sleep(5 * time.Second)
		got, err := nl.RouteListFiltered(nl.FAMILY_V4, &nl.Route{Protocol: unix.RTPROT_BGP}, nl.RT_FILTER_PROTOCOL)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		tun, err := nl.LinkByName(tunName)
		if err != nil {
			t.Fatalf("error fetching tunnel info: %v", err)
		}
		for i := range want {
			want[i].LinkIndex = tun.Attrs().Index
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("Route mismatch (-want +got): %s\n", diff)
		}
	})
}

// verifyBgpSessionIsUp waits for the BGP session to be up for the specified user type.
func verifyBgpSessionIsUp(t *testing.T, httpClient http.Client, userType api.UserType) {
	t.Run("verify_bgp_session_is_up", func(t *testing.T) {
		up, err := waitForPeerStatus(httpClient, userType, bgp.SessionStatusUp, 10*time.Second)
		if err != nil {
			t.Fatalf("error while waiting for peer status: %v", err)
		}
		if !up {
			t.Fatalf("timed out waiting for peer status of %s", bgp.SessionStatusUp)
		}
	})
}

// verifyStateFileIsCreated reads the on-disk state file and compares its contents to
// the test fixture.
func verifyStateFileMatches(t *testing.T, stateFilepath, fixturePath string) {
	t.Run("verify_state_file_matches", func(t *testing.T) {
		got, err := os.ReadFile(filepath.Join(stateFilepath, "doublezerod", "doublezerod.json"))
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		want, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatalf("error reading state file: %v", err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Fatalf("State mismatch (-want +got): %s\n", diff)
		}
	})
}

// verifyPimHelloMessageSent waits for a byte slice on the provided channel, decodes it into a PIM hello
// packet and diffs it's contents against an expected struct.
func verifyPimHelloMessageSent(t *testing.T, pimHelloChan chan []byte) {
	t.Run("verify_pim_hello_message_sent", func(t *testing.T) {
		var msg []byte
		// Wait for a PIM Hello message
		select {
		case msg = <-pimHelloChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM Hello message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.Hello,
					Checksum: 0x4317,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Fatalf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.HelloMessageType).(*pim.HelloMessage); ok {
			want := &pim.HelloMessage{
				Holdtime:     105,
				DRPriority:   1,
				GenerationID: 3614426332,
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.HelloMessage{}, "BaseLayer")); diff != "" {
				t.Fatalf("HelloMessage mismatch (-got +want):\n%s", diff)
			}
		}
	})
}

// verifyPimJoinMessageSent waits for a byte slice on the provided channel, decodes it into a PIM join
// packet and diffs it's contents against an expected struct.
func verifyPimJoinMessageSent(t *testing.T, pimJoinPruneChan chan []byte, upstreamNeighbor net.IP) {
	t.Run("verify_pim_join_message_sent", func(t *testing.T) {
		// Verify join message is received
		var msg []byte
		select {
		case msg = <-pimJoinPruneChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM Join message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.JoinPrune,
					Checksum: 0x2e45,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Fatalf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
			want := &pim.JoinPruneMessage{
				UpstreamNeighborAddress: upstreamNeighbor,
				NumGroups:               1,
				Reserved:                0,
				Holdtime:                120,
				Groups: []pim.Group{{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      1,
					NumPrunedSources:      0,
					MaskLength:            32,
					MulticastGroupAddress: net.IP{239, 0, 0, 1},
					Joins: []pim.SourceAddress{{
						AddressFamily: 1,
						Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
						MaskLength:    32,
						EncodingType:  0,
						Address:       pim.RpAddress},
					},
					Prunes: []pim.SourceAddress{},
				}},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
				t.Fatalf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
			}
		}
	})
}

// verifyPruneMessageSent waits for a byte slice on the provided channel, decodes it into a PIM prune
// packet and diffs it's contents against an expected struct.
func verifyPruneMessageSent(t *testing.T, pimJoinPruneChan chan []byte, upstreamNeighbor net.IP) {
	t.Run("verify_prune_message_sent", func(t *testing.T) {
		// Verify prune message is received
		var msg []byte
		select {
		case msg = <-pimJoinPruneChan:
			if len(msg) == 0 {
				t.Fatalf("received empty PIM message")
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for PIM prune message")
		}

		p := gopacket.NewPacket(msg, pim.PIMMessageType, gopacket.Default)
		if p.ErrorLayer() != nil {
			t.Fatalf("Error decoding packet: %v", p.ErrorLayer().Error())
		}
		if got, ok := p.Layer(pim.PIMMessageType).(*pim.PIMMessage); ok {
			want := &pim.PIMMessage{
				Header: pim.PIMHeader{
					Version:  2,
					Type:     pim.JoinPrune,
					Checksum: 0x2eb8,
				},
			}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.PIMMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("PIMMessage mismatch (-got +want):\n%s", diff)
			}
		}
		if got, ok := p.Layer(pim.JoinPruneMessageType).(*pim.JoinPruneMessage); ok {
			want := &pim.JoinPruneMessage{
				UpstreamNeighborAddress: upstreamNeighbor,
				NumGroups:               1,
				Reserved:                0,
				Holdtime:                5,
				Groups: []pim.Group{{
					GroupID:               0,
					AddressFamily:         1,
					NumJoinedSources:      0,
					NumPrunedSources:      1,
					MaskLength:            32,
					MulticastGroupAddress: net.IP{239, 0, 0, 1},
					Joins:                 []pim.SourceAddress{},
					Prunes: []pim.SourceAddress{{
						AddressFamily: 1,
						Flags:         pim.RPTreeBit | pim.SparseBit | pim.WildCardBit,
						MaskLength:    32,
						EncodingType:  0,
						Address:       pim.RpAddress}}}}}
			if diff := cmp.Diff(got, want, cmpopts.IgnoreFields(pim.JoinPruneMessage{}, "BaseLayer")); diff != "" {
				t.Errorf("JoinPruneMessage mismatch (-got +want):\n%s", diff)
			}
		}
	})
}

// TODO: should the links be removed instead of aborting?
func abortIfLinksAreUp(t *testing.T) {
	links := []string{"doublezero0", "doublezero1"}
	for _, link := range links {
		tun, _ := nl.LinkByName(link)
		if tun != nil {
			t.Fatalf("tunnel %s is up and needs to be removed", tun.Attrs().Name)
		}
	}
}

func newTestNetworkConfig(t *testing.T) *config.NetworkConfig {
	cfg, err := config.NetworkConfigForEnv(config.EnvLocalnet)
	require.NoError(t, err)
	return cfg
}

func newTestLivenessManagerConfig() *liveness.ManagerConfig {
	return &liveness.ManagerConfig{
		Logger:          slog.Default(),
		BindIP:          "0.0.0.0",
		Port:            44880,
		PassiveMode:     true,
		TxMin:           300 * time.Millisecond,
		RxMin:           300 * time.Millisecond,
		DetectMult:      3,
		MinTxFloor:      50 * time.Millisecond,
		MaxTxCeil:       1 * time.Second,
		MetricsRegistry: prometheus.NewRegistry(),
		ClientVersion:   "1.2.3-dev",
	}
}
