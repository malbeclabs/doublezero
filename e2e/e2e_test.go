//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aristanetworks/goeapi"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	nl "github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
)

var (
	// Environmental variables here are exported via the calling shell script (e2e_test.sh)
	controllerAddr = "localhost"
	controllerPort = os.Getenv("CONTROLLER_PORT")
	agentPubKey    = os.Getenv("AGENT_PUBKEY")

	// address for doublezero device
	doublezeroDeviceAddr = "64.86.249.80"
	// public address for doublezero client
	publicClientAddr = "64.86.249.86"
	// expected doublezero address to be allocated to client during test
	doublezeroAddr = "64.86.249.81/32"
	// expected link-local address to be allocated to the client during test
	linkLocalAddr = "169.254.0.1/31"

	//go:embed fixtures/*
	fs embed.FS
)

type ShowIPBGPSummary struct {
	VRFs map[string]VRF
}

type VRF struct {
	RouterID string                        `json:"routerId"`
	Peers    map[string]BGPNeighborSummary `json:"peers"`
	VRF      string                        `json:"vrf"`
	ASN      string                        `json:"asn"`
}

type BGPNeighborSummary struct {
	MsgSent             int     `json:"msgSent"`
	InMsgQueue          int     `json:"inMsgQueue"`
	PrefixReceived      int     `json:"prefixReceived"`
	UpDownTime          float64 `json:"upDownTime"`
	Version             int     `json:"version"`
	MsgReceived         int     `json:"msgReceived"`
	PrefixAccepted      int     `json:"prefixAccepted"`
	PeerState           string  `json:"peerState"`
	PeerStateIdleReason string  `json:"peerStateIdleReason,omitempty"`
	OutMsgQueue         int     `json:"outMsgQueue"`
	UnderMaintenance    bool    `json:"underMaintenance"`
	ASN                 string  `json:"asn,string"`
}

func (b *ShowIPBGPSummary) GetCmd() string {
	return "show ip bgp summary vrf vrf1"
}

type ShowIpRoute struct {
	VRFs map[string]ShowIpRouteVRF
}

type ShowIpRouteVRF struct {
	Routes map[string]IpRoute `json:"routes"`
}

type IpRoute struct {
	RouteType string `json:"routeType"`
}

func (r *ShowIpRoute) GetCmd() string {
	return "show ip route vrf vrf1"
}

// show ip mroute sparse-mode
//
// "groups": {
//     "233.84.178.0": {
//         "groupSources": {
//             "0.0.0.0": {
//                 "sourceAddress": "0.0.0.0",
//                 "creationTime": 1748646912.0,
//                 "routeFlags": "W",
//                 "rp": "10.0.0.0",
//                 "rpfInterface": "Null0",
//                 "oifList": [
//                     "Tunnel500"
//                 ]
//             }
//         }
//     }
// }

// ShowIPMroute represents the top-level structure containing multicast routing information.
type ShowIPMroute struct {
	Groups map[string]GroupDetail `json:"groups"`
}

type GroupDetail struct {
	GroupSources map[string]SourceDetail `json:"groupSources"`
}

type SourceDetail struct {
	SourceAddress string   `json:"sourceAddress"`
	CreationTime  float64  `json:"creationTime"`
	RouteFlags    string   `json:"routeFlags"`
	RP            string   `json:"rp"`
	RPFInterface  string   `json:"rpfInterface"`
	OIFList       []string `json:"oifList"`
}

func (r *ShowIPMroute) GetCmd() string {
	return "show ip mroute sparse-mode"
}

// show ip pim neighbor
//
// {
//   "neighbors": {
//     "169.254.0.1": {
//       "address": "169.254.0.1",
//       "intf": "Tunnel500",
//       "creationTime": 1748812548.569936,
//       "lastRefreshTime": 1748812548.5713866,
//       "holdTime": 105,
//       "mode": {
//         "mode": "Sparse",
//         "borderRouter": false
//       },
//       "bfdState": "disabled",
//       "transport": "datagram",
//       "detail": false,
//       "secondaryAddress": [],
//       "maintenanceReceived": null,
//       "maintenanceSent": null
//     }
//   }
// }

// ShowPIMNeighbors represents the top-level structure containing a map of PIM neighbor details.
type ShowPIMNeighbors struct {
	Neighbors map[string]PIMNeighborDetail `json:"neighbors"`
}

// PIMNeighborDetail holds the specific information for a PIM neighbor.
type PIMNeighborDetail struct {
	Address             string          `json:"address"`
	Interface           string          `json:"intf"`
	CreationTime        float64         `json:"creationTime"`
	LastRefreshTime     float64         `json:"lastRefreshTime"`
	HoldTime            int             `json:"holdTime"`
	Mode                PIMNeighborMode `json:"mode"`
	BFDState            string          `json:"bfdState"`
	Transport           string          `json:"transport"`
	Detail              bool            `json:"detail"`
	SecondaryAddresses  []string        `json:"secondaryAddress"`
	MaintenanceReceived string          `json:"maintenanceReceived"`
	MaintenanceSent     string          `json:"maintenanceSent"`
}

// PIMNeighborMode describes the operational mode of the PIM neighbor.
type PIMNeighborMode struct {
	Mode string `json:"mode"`
}

func (n *ShowPIMNeighbors) GetCmd() string {
	return "show ip pim neighbor"
}

func TestWaitForLatencyResults(t *testing.T) {
	condition := func() (bool, error) {
		buf, err := fetchClientEndpoint("/latency")
		if err != nil {
			return false, fmt.Errorf("error fetching latency results: %v", err)
		}
		results := []map[string]any{}
		if err := json.Unmarshal(buf, &results); err != nil {
			return false, fmt.Errorf("error unmarshaling latency data: %v", err)
		}
		if len(results) > 0 {
			for _, result := range results {
				// Check to make sure ny5-dz01 is reachable
				if result["device_pk"] == "8scDVeZ8aB1TRTkBqaZgzxuk7WwpARdF1a39wYA7nR3W" && result["reachable"] == true {
					return true, nil
				}
			}
		}
		return false, nil
	}
	err := waitForCondition(t, condition, 75*time.Second)
	if err != nil {
		t.Fatalf("timed out waiting for latency results: %v", err)
	}
}

func TestWaitForClientTunnelUp(t *testing.T) {
	condition := func() (bool, error) {
		buf, err := fetchClientEndpoint("/status")
		if err != nil {
			t.Fatalf("error fetching status: %v", err)
		}
		status := []map[string]any{}
		if err := json.Unmarshal(buf, &status); err != nil {
			t.Fatalf("error unmarshaling latency data: %v", err)
		}
		for _, s := range status {
			if session, ok := s["doublezero_status"]; ok {
				if sessionStatus, ok := session.(map[string]any)["session_status"]; ok {
					if sessionStatus == "up" {
						return true, nil
					}
				}
			}
		}
		return false, nil
	}
	err := waitForCondition(t, condition, 60*time.Second)
	if err != nil {
		t.Fatalf("timed out waiting for up status: %v", err)
	}
}

// TestIBRLWithAllocatedAddress_Connect_Output is a set of tests to verify the output of the doublezero
// CLI. These tests utilize golden files of expected output stored in the fixtures directory,
// which are then compared against the std output of each command line call.
//
// Based on the current behavior of the CLI, the output can change with dynamic data which
// cause lines to be sorted differently (i.e. different public keys are generated for a device)
// from test run to test run. Because of this, we treat desired and test generated output as
// slices of strings and verify each line of the desired output is present in the test generated
// output slice.
func TestIBRLWithAllocatedAddress_Connect_Output(t *testing.T) {
	config, err := fs.ReadFile("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_added.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pick_up_new_user", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up user for IBRL with allocated address")
		}
	})

	tests := []struct {
		name       string
		goldenFile string
		// example table file:  "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_added.txt"
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_user_list",
			goldenFile:     "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_added.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "user", "list"},
		},
		{
			name:           "doublezero_device_list",
			goldenFile:     "fixtures/ibrl_with_allocated_addr/doublezero_device_list.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "device", "list"},
		},
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/ibrl_with_allocated_addr/doublezero_status_connected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestIBRLWithAllocatedAddress_Connect_Networking verifies the client and agent configuration after a
// user smartcontract has been established.
func TestIBRLWithAllocatedAddress_Connect_Networking(t *testing.T) {
	t.Run("check_tunnel_interface_is_configured", func(t *testing.T) {
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
		// TODO: check IP address configuration
		// TODO: check routing tables
		// TODO: check IP rules
	})
	t.Run("check_doublezero_address_is_configured", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}
		// want, err := nl.ParseAddr(doublezeroAddr)
		// if err != nil {
		// 	t.Fatalf("error parsing address: %v", err)
		// }
		findAddr := func(addr string) (bool, error) {
			want, err := nl.ParseAddr(addr)
			if err != nil {
				return false, fmt.Errorf("error parsing address: %v", err)
			}
			return slices.ContainsFunc(addrs, func(a nl.Addr) bool {
				return a.Equal(*want)
			}), nil
		}
		found, err := findAddr(doublezeroAddr)
		if err != nil {
			t.Fatalf("error while checking for doublezero address: %v", err)
		}
		if !found {
			t.Fatalf("doublezero address %s not found on tunnel\n", doublezeroAddr)
		}
		found, err = findAddr(linkLocalAddr)
		if err != nil {
			t.Fatalf("error while checking for link-local address: %v", err)
		}
		if !found {
			t.Fatalf("link-local address %s not found on tunnel\n", doublezeroAddr)
		}
	})
	t.Run("check_agent_configuration", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_added.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}
	})

	t.Run("check_user_session_is_established", func(t *testing.T) {
		dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
		if err != nil {
			t.Fatalf("error connecting to dut: %v", err)
		}
		handle, err := dut.GetHandle("json")
		neighbors := &ShowIPBGPSummary{}
		routes := &ShowIpRoute{}
		handle.AddCommand(neighbors)
		handle.AddCommand(routes)
		if err := handle.Call(); err != nil {
			t.Fatalf("error fetching neighbors from doublezero device: %v", err)
		}

		ip := strings.Split(linkLocalAddr, "/")[0]
		peer, ok := neighbors.VRFs["vrf1"].Peers[ip]
		if !ok {
			t.Fatalf("client ip %s missing from doublezero device\n", linkLocalAddr)
		}
		if peer.ASN != "65000" {
			t.Fatalf("client asn should be 65000; got %s\n", peer.ASN)
		}
		if peer.PeerState != "Established" {
			t.Fatalf("client state should be established; got %s\n", peer.PeerState)
		}

		_, ok = routes.VRFs["vrf1"].Routes[doublezeroAddr]
		if !ok {
			t.Fatalf("expected client route of %s installed; got none\n", doublezeroAddr)
		}
	})

	// user ban verified in the `doublezer_user_list_removed.txt` fixture
	t.Run("ban_user", func(t *testing.T) {
		// TODO: this is brittle, come up with a more elastic solution
		cmd := []string{"doublezero", "user", "request-ban", "--pubkey", "AA3fFZM1bJbNzCWhPydZrbQpswGkZx4PFhxd2bHaztyG"}
		_, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil {
			t.Fatalf("error running cmd %s: %v", cmd, err)
		}
	})
}

// TestIBRLWithAllocatedAddress__Disconnect_Networking verifies the client and agent configuration after a
// user has been disconnected.
func TestIBRLWithAllocatedAddress_Disconnect_Networking(t *testing.T) {
	config, err := fs.ReadFile("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_removed.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pickup_disconnected_user", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up disconnected user for IBRL with allocated address")
		}
	})

	t.Run("check_tunnel_interface_is_removed", func(t *testing.T) {
		links, err := nl.LinkList()
		if err != nil {
			t.Fatalf("error fetching links: %v\n", err)
		}
		found := slices.ContainsFunc(links, func(l nl.Link) bool {
			return l.Attrs().Name == "doublezero0"
		})
		if found {
			t.Fatal("doublezero0 tunnel interface not removed on disconnect")
		}
	})

	t.Run("check_user_contract_is_removed", func(t *testing.T) {
		goldenFile := "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_removed.txt"
		cmd := []string{"doublezero", "user", "list"}
		diff, err := diffCliToGolden(goldenFile, "table", cmd...)
		if err != nil {
			t.Fatalf("error generating diff: %v", err)
		}
		if diff != "" {
			t.Fatalf("output mismatch: -(want), +(got):%s", diff)
		}
	})

	t.Run("check_user_tunnel_is_removed_from_agent", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/ibrl_with_allocated_addr/doublezero_agent_config_user_removed.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}

		dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
		if err != nil {
			t.Fatalf("error connecting to dut: %v", err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			handle, err := dut.GetHandle("json")
			if err != nil {
				t.Fatalf("error getting handle: %v", err)
			}
			neighbors := &ShowIPBGPSummary{}
			handle.AddCommand(neighbors)
			if err := handle.Call(); err != nil {
				t.Fatalf("error fetching neighbors from doublezero device: %v", err)
			}

			ip := strings.Split(linkLocalAddr, "/")[0]
			_, ok := neighbors.VRFs["vrf1"].Peers[ip]
			if !ok {
				return
			}
			time.Sleep(1 * time.Second)
		}
		t.Fatalf("bgp neighbor %s has not been removed from doublezero device\n", linkLocalAddr)
	})
}

func TestIBRLWithAllocatedAddress_Disconnect_Output(t *testing.T) {
	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/ibrl_with_allocated_addr/doublezero_status_disconnected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

func TestIBRL_Connect_Output(t *testing.T) {
	want, err := fs.ReadFile("fixtures/ibrl/doublezero_agent_config_user_added.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pick_up_new_user", func(t *testing.T) {
		if !waitForController(want) {
			t.Fatal("timed out waiting for controller to pick up user for IBRL")
		}
	})

	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_user_list",
			goldenFile:     "fixtures/ibrl/doublezero_user_list_user_added.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "user", "list"},
		},
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/ibrl/doublezero_status_connected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestIBRL_Connect_Networking verifies the client and agent configuration after a
// smartcontract has been established. This covers the "bring your own IP" version
// of IBRL.
func TestIBRL_Connect_Networking(t *testing.T) {
	t.Run("check_tunnel_interface_is_configured", func(t *testing.T) {
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
	t.Run("check_doublezero_address_is_configured", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero0")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}
		if len(addrs) > 1 {
			t.Fatalf("only expecting link-local address configured; found %d: %v", len(addrs), addrs)
		}
		findAddr := func(addr string) (bool, error) {
			want, err := nl.ParseAddr(addr)
			if err != nil {
				return false, fmt.Errorf("error parsing address: %v", err)
			}
			return slices.ContainsFunc(addrs, func(a nl.Addr) bool {
				return a.Equal(*want)
			}), nil
		}
		found, err := findAddr(linkLocalAddr)
		if err != nil {
			t.Fatalf("error while checking for link-local address: %v", err)
		}
		if !found {
			t.Fatalf("link-local address %s not found on tunnel\n", linkLocalAddr)
		}
	})
	// TODO: check routing tables
	t.Run("check_learned_route_installed", func(t *testing.T) {
		// 8.8.8.8/32 should be received from the attached dz device and installed

		// in the main routing table on the client
		// TODO: figure out why this sleep is now needed
		time.Sleep(1 * time.Second)
		route, err := nl.RouteListFiltered(
			0,
			&nl.Route{
				Table: syscall.RT_TABLE_MAIN,
				Dst: &net.IPNet{
					IP:   net.IP{8, 8, 8, 8},
					Mask: net.IPMask{255, 255, 255, 255},
				},
			},
			nl.RT_FILTER_TABLE,
		)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}
		if len(route) == 0 {
			t.Fatalf("no route found")
		}
		if !route[0].Src.Equal(net.ParseIP(publicClientAddr)) {
			t.Fatalf("route src hint should be %s; got %s", publicClientAddr, route[0].Src)
		}
		if route[0].Dst.String() != "8.8.8.8/32" {
			t.Fatalf("route dst should be 8.8.8.8/32; got %s", route[0].Dst)
		}
		if !route[0].Gw.Equal(net.IP{169, 254, 0, 0}) {
			t.Fatalf("route gw should be 169.254.0.0; got %s", route[0].Gw)
		}
	})

	t.Run("check_agent_configuration", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/ibrl/doublezero_agent_config_user_added.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}
	})

	t.Run("check_user_session_is_established", func(t *testing.T) {
		dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
		if err != nil {
			t.Fatalf("error connecting to dut: %v", err)
		}
		handle, err := dut.GetHandle("json")
		neighbors := &ShowIPBGPSummary{}
		routes := &ShowIpRoute{}
		handle.AddCommand(neighbors)
		handle.AddCommand(routes)
		if err := handle.Call(); err != nil {
			t.Fatalf("error fetching neighbors from doublezero device: %v", err)
		}

		ip := strings.Split(linkLocalAddr, "/")[0]
		peer, ok := neighbors.VRFs["vrf1"].Peers[ip]
		if !ok {
			t.Fatalf("client ip %s missing from doublezero device\n", linkLocalAddr)
		}
		if peer.ASN != "65000" {
			t.Fatalf("client asn should be 65000; got %s\n", peer.ASN)
		}
		if peer.PeerState != "Established" {
			t.Fatalf("client state should be established; got %s\n", peer.PeerState)
		}

		want := publicClientAddr + "/32"
		_, ok = routes.VRFs["vrf1"].Routes[want]
		if !ok {
			t.Fatalf("expected client route of %s installed; got none\n", want)
		}
	})
}

func TestIBRL_Disconnect_Output(t *testing.T) {
	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_user_list",
			goldenFile:     "fixtures/ibrl/doublezero_user_list_user_removed.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "user", "list"},
		},
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/ibrl/doublezero_status_disconnected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestIBRL_Disconnect_Networking verifies the client and agent configuration after a
// user has been disconnected.
func TestIBRL_Disconnect_Networking(t *testing.T) {
	want, err := fs.ReadFile("fixtures/ibrl/doublezero_agent_config_user_removed.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}
	t.Run("wait_for_controller_to_pickup_disconnected_user", func(t *testing.T) {
		if !waitForController(want) {
			t.Fatal("timed out waiting for controller to pick up disconnected user for IBRL")
		}
	})

	t.Run("check_tunnel_interface_is_removed", func(t *testing.T) {
		links, err := nl.LinkList()
		if err != nil {
			t.Fatalf("error fetching links: %v\n", err)
		}
		found := slices.ContainsFunc(links, func(l nl.Link) bool {
			return l.Attrs().Name == "doublezero0"
		})
		if found {
			t.Fatal("doublezero0 tunnel interface not removed on disconnect")
		}
	})

	t.Run("check_user_contract_is_removed", func(t *testing.T) {
		goldenFile := "fixtures/ibrl/doublezero_user_list_user_removed.txt"
		testOutputType := "table"
		cmd := []string{"doublezero", "user", "list"}
		diff, err := diffCliToGolden(goldenFile, testOutputType, cmd...)
		if err != nil {
			t.Fatalf("error generating diff: %v", err)
		}
		if diff != "" {
			t.Fatalf("output mismatch: -(want), +(got):%s", diff)
		}
	})

	t.Run("check_user_tunnel_is_removed_from_agent", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/ibrl/doublezero_agent_config_user_removed.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}

		dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
		if err != nil {
			t.Fatalf("error connecting to dut: %v", err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			handle, err := dut.GetHandle("json")
			if err != nil {
				t.Fatalf("error getting handle: %v", err)
			}
			neighbors := &ShowIPBGPSummary{}
			handle.AddCommand(neighbors)
			if err := handle.Call(); err != nil {
				t.Fatalf("error fetching neighbors from doublezero device: %v", err)
			}

			ip := strings.Split(linkLocalAddr, "/")[0]
			_, ok := neighbors.VRFs["vrf1"].Peers[ip]
			if !ok {
				return
			}
			time.Sleep(1 * time.Second)
		}
		t.Fatalf("bgp neighbor %s has not been removed from doublezero device\n", linkLocalAddr)
	})
}

func diffCliToGolden(goldenFile string, testOutputType string, cmds ...string) (string, error) {
	want, err := fs.ReadFile(goldenFile)
	if err != nil {
		return "", fmt.Errorf("error reading golden file %s: %v", goldenFile, err)
	}

	got, err := exec.Command(cmds[0], cmds[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("error running cmd %s: %v", cmds, err)
	}

	switch testOutputType {
	case "table":
		diff := diffCliMapToGoldenMapTable(want, got)
		return diff, nil
	default:
		return "", fmt.Errorf("unexepcted testOutputType: %s\n", testOutputType)

	}
}

func diffCliMapToGoldenMapTable(want []byte, got []byte) string {
	gotMap := mapFromTable(got)
	wantMap := mapFromTable(want)

	ignoreKeys := []string{"Last Session Update"}

	return cmp.Diff(gotMap, wantMap, cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
		return slices.Contains(ignoreKeys, key)
	}))
}

// example table
// pubkey                                       | user_type           | device   | cyoa_type  | client_ip    | tunnel_id | tunnel_net      | dz_ip        | status    | owner
// NR8fpCK7mqeFVJ3mUmhndX2JtRCymZzgQgGj5JNbGp8  | IBRL                | la2-dz01 | GREOverDIA | 1.2.3.4      | 500       | 169.254.0.2/31  | 1.2.3.4      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
// 5Rm8dp4dDzR5SE3HtrqGVpqHLaPvvxDEV3EotqPBBUgS | IBRL                | la2-dz01 | GREOverDIA | 5.6.7.8      | 504       | 169.254.0.10/31 | 5.6.7.8      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
func mapFromTable(output []byte) []map[string]string {
	var sliceOfMaps []map[string]string

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Scan()
	header := scanner.Text()
	split := strings.Split(header, "|")
	trimmed_header := make([]string, len(split))
	for i, key := range split {
		trimmed_header[i] = strings.TrimSpace(key)
	}

	for i := 0; scanner.Scan(); i++ {
		formattedMap := make(map[string]string)
		line := scanner.Text()
		split := strings.Split(line, "|")
		for i, key := range split {
			formattedMap[trimmed_header[i]] = strings.TrimSpace(key)
		}
		sliceOfMaps = append(sliceOfMaps, formattedMap)
	}

	slices.SortFunc(sliceOfMaps, func(a, b map[string]string) int {
		return strings.Compare(strings.ToLower(a["account"]), strings.ToLower(b["account"]))
	})

	return sliceOfMaps
}

func waitForController(config []byte) bool {
	target := net.JoinHostPort(controllerAddr, controllerPort)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		log.Fatalf("error creating controller client: %v", err)
	}
	defer conn.Close()
	defer cancel()

	agent := pb.NewControllerClient(conn)
	deadline := time.Now().Add(30 * time.Second)
	var diff string
	for time.Now().Before(deadline) {
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		diff = cmp.Diff(string(config), got.Config)
		if diff == "" {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("output mismatch: +(want), -(got): %s", diff)
	return false
}

func fetchClientEndpoint(endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, "unix", "/var/run/doublezerod/doublezerod.sock")
			},
		},
	}

	url, err := url.JoinPath("http://doublezero/", endpoint)
	if err != nil {
		log.Fatalf("error creating url: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatalf("error creating request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("error during request: %v", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching endpoint %s: %s", endpoint, string(buf))
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("empty response from endpoint %s", endpoint)
	}
	return buf, nil
}

// TestMulticastPublisher_Connect_Output tests the output of multicast publisher connection
func TestMulticastPublisher_Connect_Output(t *testing.T) {
	config, err := fs.ReadFile("fixtures/multicast_publisher/doublezero_agent_config_user_added.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pick_up_multicast_publisher", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up multicast publisher")
		}
	})

	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_multicast_group_list",
			goldenFile:     "fixtures/multicast_publisher/doublezero_multicast_group_list.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "multicast", "group", "list"},
		},
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/multicast_publisher/doublezero_status_connected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestMulticastPublisher_Connect_Networking verifies the multicast publisher configuration
func TestMulticastPublisher_Connect_Networking(t *testing.T) {
	dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
	if err != nil {
		t.Fatalf("error connecting to dut: %v", err)
	}
	handle, err := dut.GetHandle("json")
	if err != nil {
		t.Fatalf("error getting handle: %v", err)
	}

	t.Run("check_tunnel_interface_is_configured", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
			t.Fatalf("tunnel name is not doublezero1: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state, got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("check_multicast_static_routes", func(t *testing.T) {
		// Verify static multicast routes are installed for publisher
		routes, err := nl.RouteListFiltered(
			0,
			&nl.Route{
				Table: syscall.RT_TABLE_MAIN,
			},
			nl.RT_FILTER_TABLE,
		)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}

		// Look for multicast route (233.84.178.0/32 for example)
		foundMcastRoute := false
		for _, route := range routes {
			if route.Dst != nil && route.Dst.IP.Equal(net.ParseIP("233.84.178.0")) {
				foundMcastRoute = true
				break
			}
		}
		if !foundMcastRoute {
			t.Fatalf("multicast route 233.84.178.0/32 not found for publisher")
		}
	})

	t.Run("check_s_comma_g_is_created", func(t *testing.T) {
		// Send single ping to simulate multicast traffic
		cmd := exec.Command("ping", "-c", "1", "-w", "1", "233.84.178.0")
		_ = cmd.Run()

		mroutes := &ShowIPMroute{}
		if err := handle.AddCommand(mroutes); err != nil {
			t.Fatalf("error adding %s command: %v", mroutes.GetCmd(), err)
		}
		if err := handle.Call(); err != nil {
			t.Fatalf("error fetching neighbors from doublezero device: %v", err)
		}

		mGroup := "233.84.178.0"
		groups, ok := mroutes.Groups[mGroup]
		if !ok {
			t.Fatalf("multicast group %s not found in mroutes", mGroup)
		}

		_, ok = groups.GroupSources["64.86.249.81"]
		if !ok {
			t.Fatalf("missing S,G for (64.86.249.81, 233.84.178.0)")
		}
	})

	t.Run("check_agent_configuration", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/multicast_publisher/doublezero_agent_config_user_added.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}
	})
}

// TestMulticastPublisher_Disconnect_Output tests the output after multicast publisher disconnection
func TestMulticastPublisher_Disconnect_Output(t *testing.T) {
	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/multicast_publisher/doublezero_status_disconnected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestMulticastPublisher_Disconnect_Networking verifies cleanup after multicast publisher disconnection
func TestMulticastPublisher_Disconnect_Networking(t *testing.T) {
	config, err := fs.ReadFile("fixtures/multicast_publisher/doublezero_agent_config_user_removed.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pickup_disconnected_multicast_publisher", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up disconnected multicast publisher")
		}
	})

	t.Run("check_tunnel_interface_is_removed", func(t *testing.T) {
		links, err := nl.LinkList()
		if err != nil {
			t.Fatalf("error fetching links: %v\n", err)
		}
		found := slices.ContainsFunc(links, func(l nl.Link) bool {
			return l.Attrs().Name == "doublezero1"
		})
		if found {
			t.Fatal("doublezero1 tunnel interface not removed on disconnect")
		}
	})

	t.Run("check_multicast_routes_removed", func(t *testing.T) {
		routes, err := nl.RouteListFiltered(
			0,
			&nl.Route{
				Table: syscall.RT_TABLE_MAIN,
			},
			nl.RT_FILTER_TABLE,
		)
		if err != nil {
			t.Fatalf("error fetching routes: %v", err)
		}

		// Verify multicast routes are removed
		for _, route := range routes {
			if route.Dst != nil && route.Dst.IP.Equal(net.ParseIP("233.84.178.0")) {
				t.Fatalf("multicast route 233.84.178.0/32 should be removed after disconnect")
			}
		}
	})
}

// TestMulticastSubscriber_Connect_Output tests the output of multicast subscriber connection
func TestMulticastSubscriber_Connect_Output(t *testing.T) {
	config, err := fs.ReadFile("fixtures/multicast_subscriber/doublezero_agent_config_user_added.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pick_up_multicast_subscriber", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up multicast subscriber")
		}
	})

	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_multicast_group_list",
			goldenFile:     "fixtures/multicast_subscriber/doublezero_multicast_group_list.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "multicast", "group", "list"},
		},
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/multicast_subscriber/doublezero_status_connected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestMulticastSubscriber_Connect_Networking verifies the multicast subscriber configuration
func TestMulticastSubscriber_Connect_Networking(t *testing.T) {
	dut, err := goeapi.Connect("http", doublezeroDeviceAddr, "admin", "admin", 80)
	if err != nil {
		t.Fatalf("error connecting to dut: %v", err)
	}
	handle, err := dut.GetHandle("json")
	if err != nil {
		t.Fatalf("error getting handle: %v", err)
	}

	t.Run("check_tunnel_interface_is_configured", func(t *testing.T) {
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		if tun.Attrs().Name != "doublezero1" {
			t.Fatalf("tunnel name is not doublezero1: %s", tun.Attrs().Name)
		}
		if tun.Attrs().OperState != 0 { // 0 == IF_OPER_UNKNOWN
			t.Fatalf("tunnel is not set to up state, got %d", tun.Attrs().OperState)
		}
		if tun.Attrs().MTU != 1476 {
			t.Fatalf("tunnel mtu should be 1476; got %d", tun.Attrs().MTU)
		}
	})

	t.Run("check_multicast_group_addresses", func(t *testing.T) {
		// For subscribers, multicast group addresses should be configured on the tunnel
		tun, err := nl.LinkByName("doublezero1")
		if err != nil {
			t.Fatalf("error fetching tunnel status: %v", err)
		}
		addrs, err := nl.AddrList(tun, nl.FAMILY_V4)
		if err != nil {
			t.Fatalf("error fetching tunnel addresses: %v", err)
		}

		// Check for multicast group address (e.g., 233.84.178.0/32)
		foundMcastAddr := false
		for _, addr := range addrs {
			if addr.IP.Equal(net.ParseIP("233.84.178.0")) {
				foundMcastAddr = true
				break
			}
		}
		if !foundMcastAddr {
			t.Fatalf("multicast group address 233.84.178.0 not found on tunnel for subscriber")
		}
	})

	t.Run("check_pim_neighbor_formed", func(t *testing.T) {
		condition := func() (bool, error) {
			pim := &ShowPIMNeighbors{}
			if err := handle.AddCommand(pim); err != nil {
				t.Fatalf("error adding %s command: %v", pim.GetCmd(), err)
			}
			if err := handle.Call(); err != nil {
				return false, fmt.Errorf("error fetching neighbors from doublezero device: %v", err)
			}
			neighbor, ok := pim.Neighbors["169.254.0.1"]
			if !ok {
				return false, nil
			}
			if neighbor.Interface == "Tunnel500" {
				return true, nil
			}
			return false, nil
		}
		err = waitForCondition(t, condition, 30*time.Second)
		if err != nil {
			t.Fatalf("PIM neighbor not established on Tunnel500: %v", err)
		}
	})

	t.Run("check_pim_join_received", func(t *testing.T) {
		condition := func() (bool, error) {
			mroutes := &ShowIPMroute{}
			if err := handle.AddCommand(mroutes); err != nil {
				t.Fatalf("error adding %s command: %v", mroutes.GetCmd(), err)
			}
			if err := handle.Call(); err != nil {
				return false, fmt.Errorf("error fetching neighbors from doublezero device: %v", err)
			}

			mGroup := "233.84.178.0"
			groups, ok := mroutes.Groups[mGroup]
			if !ok {
				return false, nil
			}

			groupDetails, ok := groups.GroupSources["0.0.0.0"]
			if !ok {
				return false, nil
			}

			if slices.Contains(groupDetails.OIFList, "Tunnel500") {
				return true, nil
			}
			return false, nil
		}

		err = waitForCondition(t, condition, 30*time.Second)
		if err != nil {
			t.Fatalf("PIM join not received for 233.84.178.0: %v", err)
		}
	})

	t.Run("check_agent_configuration", func(t *testing.T) {
		target := net.JoinHostPort(controllerAddr, controllerPort)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			log.Fatalf("error creating controller client: %v", err)
		}
		defer conn.Close()
		defer cancel()

		agent := pb.NewControllerClient(conn)
		got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: agentPubKey})
		if err != nil {
			log.Fatalf("error while fetching config: %v\n", err)
		}
		want, err := fs.ReadFile("fixtures/multicast_subscriber/doublezero_agent_config_user_added.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}
	})
}

// TestMulticastSubscriber_Disconnect_Output tests the output after multicast subscriber disconnection
func TestMulticastSubscriber_Disconnect_Output(t *testing.T) {
	tests := []struct {
		name           string
		goldenFile     string
		testOutputType string
		cmd            []string
	}{
		{
			name:           "doublezero_status",
			goldenFile:     "fixtures/multicast_subscriber/doublezero_status_disconnected.txt",
			testOutputType: "table",
			cmd:            []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.testOutputType, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestMulticastSubscriber_Disconnect_Networking verifies cleanup after multicast subscriber disconnection
func TestMulticastSubscriber_Disconnect_Networking(t *testing.T) {
	config, err := fs.ReadFile("fixtures/multicast_subscriber/doublezero_agent_config_user_removed.txt")
	if err != nil {
		t.Fatalf("error loading expected agent configuration from file: %v", err)
	}

	t.Run("wait_for_controller_to_pickup_disconnected_multicast_subscriber", func(t *testing.T) {
		if !waitForController(config) {
			t.Fatal("timed out waiting for controller to pick up disconnected multicast subscriber")
		}
	})

	t.Run("check_tunnel_interface_is_removed", func(t *testing.T) {
		links, err := nl.LinkList()
		if err != nil {
			t.Fatalf("error fetching links: %v\n", err)
		}
		found := slices.ContainsFunc(links, func(l nl.Link) bool {
			return l.Attrs().Name == "doublezero1"
		})
		if found {
			t.Fatal("doublezero1 tunnel interface not removed on disconnect")
		}
	})

	// TODO: Fix me later
	// t.Run("check_pim_is_stopped", func(t *testing.T) {
	// 	// Check if PIM process is stopped after disconnect
	// 	cmd := exec.Command("pgrep", "-f", "pimd")
	// 	err := cmd.Run()
	// 	if err == nil {
	// 		t.Fatalf("PIM daemon should be stopped after multicast subscriber disconnect")
	// 	}
	// })
}

func waitForCondition(t *testing.T, condition func() (bool, error), timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := condition()
		if err != nil {
			return fmt.Errorf("error checking condition: %v", err)
		}
		if ok {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("condition not met within %s", timeout)
}
