//go:build e2e

package e2e_test

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
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
	return "show ip bgp summary vrf all"
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
	return "show ip route vrf all"
}

// TestClientOutputAfterConnect is a set of tests to verify the output of the doublezero
// CLI. These tests utilize golden files of expected output stored in the fixtures directory,
// which are then compared against the std output of each command line call.
//
// Based on the current behavior of the CLI, the output can change with dynamic data which
// cause lines to be sorted differently (i.e. different public keys are generated for a device)
// from test run to test run. Because of this, we treat desired and test generated output as
// slices of strings and verify each line of the desired output is present in the test generated
// output slice.
func TestClientOutputAfterConnect(t *testing.T) {
	tests := []struct {
		name       string
		goldenFile string
		cmd        []string
	}{
		{
			name:       "doublezero_user_list",
			goldenFile: "fixtures/doublezero_user_list_user_added.txt",
			cmd:        []string{"doublezero", "user", "list"},
		},
		{
			name:       "doublezero_device_list",
			goldenFile: "fixtures/doublezero_device_list.txt",
			cmd:        []string{"doublezero", "device", "list"},
		},
		{
			name:       "doublezero_status",
			goldenFile: "fixtures/doublezero_status_connected.txt",
			cmd:        []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

// TestConnect verifies the client and agent configuration after a
// user smartcontract has been established.
func TestConnect(t *testing.T) {
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
		want, err := fs.ReadFile("fixtures/doublezero_agent_config_user_added.txt")
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
}

// TestDisconnect verifies the client and agent configuration after a
// user has been disconnected.
func TestDisconnect(t *testing.T) {
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
		goldenFile := "fixtures/doublezero_user_list_user_removed.txt"
		cmd := []string{"doublezero", "user", "list"}
		diff, err := diffCliToGolden(goldenFile, cmd...)
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
		want, err := fs.ReadFile("fixtures/doublezero_agent_config_user_removed.txt")
		if err != nil {
			t.Fatalf("error loading expected agent configuration from file: %v", err)
		}

		if diff := cmp.Diff(string(want), got.Config); diff != "" {
			t.Fatalf("output mismatch: -(want), +(got): %s", diff)
		}
	})
}

func TestClientOutputAfterDisconnect(t *testing.T) {
	tests := []struct {
		name       string
		goldenFile string
		cmd        []string
	}{
		{
			name:       "doublezero_user_list",
			goldenFile: "fixtures/doublezero_user_list_user_removed.txt",
			cmd:        []string{"doublezero", "user", "list"},
		},
		{
			name:       "doublezero_status",
			goldenFile: "fixtures/doublezero_status_disconnected.txt",
			cmd:        []string{"doublezero", "status"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff, err := diffCliToGolden(test.goldenFile, test.cmd...)
			if err != nil {
				t.Fatalf("error generating diff: %v", err)
			}
			if diff != "" {
				t.Fatalf("output mismatch: -(want), +(got):%s", diff)
			}
		})
	}
}

func diffCliToGolden(goldenFile string, cmds ...string) (string, error) {
	want, err := fs.ReadFile(goldenFile)
	if err != nil {
		return "", fmt.Errorf("error reading golden file %s: %v", goldenFile, err)
	}
	got, err := exec.Command(cmds[0], cmds[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("error running cmd %s: %v", cmds, err)
	}
	opts := []cmp.Option{
		cmpopts.SortSlices(func(a, b string) bool { return a < b }),
	}
	return cmp.Diff(strings.Split(string(want), "\n"), strings.Split(string(got), "\n"), opts...), nil
}
