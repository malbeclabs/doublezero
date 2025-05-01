//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"log"
	"net"
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
	tests := []struct {
		name       string
		goldenFile string
		// example table file:  "fixtures/ibrl_with_allocated_addr/doublezero_user_list_user_added.txt"
		// example kv file: "fixtures/ibrl_with_allocated_addr/doublezero_status_connected.txt"
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
			testOutputType: "kv",
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
}

// TestIBRLWithAllocatedAddress__Disconnect_Networking verifies the client and agent configuration after a
// user has been disconnected.
func TestIBRLWithAllocatedAddress_Disconnect_Networking(t *testing.T) {
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
			testOutputType: "kv",
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
			testOutputType: "kv",
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
			testOutputType: "kv",
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
	case "kv":
		diff := diffCliMapToGoldenMapKV(want, got)
		return diff, nil
	default:
		return "", fmt.Errorf("unexepcted testOutputType: %s\n", testOutputType)

	}
}

func diffCliMapToGoldenMapTable(want []byte, got []byte) string {
	gotMap := mapFromTable(got)
	wantMap := mapFromTable(want)

	return cmp.Diff(gotMap, wantMap)
}

func diffCliMapToGoldenMapKV(want []byte, got []byte) string {
	gotMap := mapFromKV(got)
	wantMap := mapFromKV(want)

	ignoreKeys := []string{"Last Session Update"}

	return cmp.Diff(gotMap, wantMap, cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
		for _, k := range ignoreKeys {
			if key == k {
				return true
			}
		}
		return false
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
		return strings.Compare(strings.ToLower(a["pubkey"]), strings.ToLower(b["pubkey"]))
	})

	return sliceOfMaps
}

// example KV
// Doublezero IP: 64.86.249.81
// Name: doublezero0
// Tunnel dst: 64.86.249.80
// Tunnel src: 64.86.249.86
// Tunnel status: up
func mapFromKV(output []byte) map[string]string {
	formattedMap := make(map[string]string)

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for i := 0; scanner.Scan(); i++ {
		line := scanner.Text()
		split := strings.Split(line, ":")
		formattedMap[split[0]] = strings.TrimSpace(split[1])
	}
	return formattedMap
}
