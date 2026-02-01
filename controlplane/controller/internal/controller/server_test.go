package controller

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/gagliardetto/solana-go"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/malbeclabs/doublezero/controlplane/controller/config"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

// helper that creates a slice of Tunnel structs with sequential IDs. We can use this to populate
// a list of tunnel slots so we don't have to update tests by hand when MaxUserTunnelSlots changes.
func generateEmptyTunnelSlots(startID, count int) []*Tunnel {
	tunnels := make([]*Tunnel, count)
	for i := 0; i < count; i++ {
		tunnels[i] = &Tunnel{Id: startID + i}
	}
	return tunnels
}

// seq generates a sequence of integers from start to end (inclusive)
func seq(start, end int) []int {
	if start > end {
		return []int{}
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

// add returns the sum of two integers
func add(a, b int) int {
	return a + b
}

// renderTemplateFile reads a file and renders it as a template with the given data
func renderTemplateFile(filepath string, data any) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	tmpl := template.New("").Funcs(template.FuncMap{
		"seq": seq,
		"add": add,
	})
	tmpl, err = tmpl.Parse(string(content))
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		StateCache  stateCache
		NoHardware  bool
		Pubkey      string
		Want        string
	}{
		{
			Name:        "render_unicast_config_successfully",
			Description: "render configuration for a set of unicast devices successfully",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Devices: map[string]*Device{
					"abc123": {
						Interfaces:   []Interface{},
						ExchangeCode: "tst",
						BgpCommunity: 10050,
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{1, 1, 1, 1},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{169, 254, 0, 0},
								OverlayDstIP:  net.IP{169, 254, 0, 1},
								DzIp:          net.IP{100, 0, 0, 0},
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{4, 4, 4, 4},
								OverlaySrcIP:  net.IP{169, 254, 0, 2},
								OverlayDstIP:  net.IP{169, 254, 0, 3},
								DzIp:          net.IP{100, 0, 0, 1},
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{5, 5, 5, 5},
								UnderlayDstIP: net.IP{6, 6, 6, 6},
								OverlaySrcIP:  net.IP{169, 254, 0, 4},
								OverlayDstIP:  net.IP{169, 254, 0, 5},
								DzIp:          net.IP{100, 0, 0, 2},
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
						},
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						Vpn4vLoopbackIntfName: "Loopback255",
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						DevicePathologies:     []string{},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/unicast.tunnel.tmpl",
		},
		{
			Name:        "render_multicast_config_successfully",
			Description: "render configuration for a set of multicast devices successfully",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Devices: map[string]*Device{
					"abc123": {
						Interfaces:   []Interface{},
						ExchangeCode: "tst",
						BgpCommunity: 10050,
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{1, 1, 1, 1},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{169, 254, 0, 0},
								OverlayDstIP:  net.IP{169, 254, 0, 1},
								DzIp:          net.IP{100, 0, 0, 0},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastPublishers: []net.IP{},
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{4, 4, 4, 4},
								OverlaySrcIP:  net.IP{169, 254, 0, 2},
								OverlayDstIP:  net.IP{169, 254, 0, 3},
								DzIp:          net.IP{100, 0, 0, 1},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
								},
								MulticastSubscribers: []net.IP{},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
								},
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{5, 5, 5, 5},
								UnderlayDstIP: net.IP{6, 6, 6, 6},
								OverlaySrcIP:  net.IP{169, 254, 0, 4},
								OverlayDstIP:  net.IP{169, 254, 0, 5},
								DzIp:          net.IP{100, 0, 0, 2},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
						},
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						Vpn4vLoopbackIntfName: "Loopback255",
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						DevicePathologies:     []string{},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/multicast.tunnel.tmpl",
		},
		{
			Name:        "get_config_mixed_tunnels_successfully",
			Description: "get config for a mix of unicast and multicast tunnels",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Devices: map[string]*Device{
					"abc123": {
						ExchangeCode: "tst",
						BgpCommunity: 10050,
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{1, 1, 1, 1},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{169, 254, 0, 0},
								OverlayDstIP:  net.IP{169, 254, 0, 1},
								DzIp:          net.IP{100, 0, 0, 0},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastPublishers: []net.IP{},
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{4, 4, 4, 4},
								OverlaySrcIP:  net.IP{169, 254, 0, 2},
								OverlayDstIP:  net.IP{169, 254, 0, 3},
								DzIp:          net.IP{100, 0, 0, 1},
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{5, 5, 5, 5},
								UnderlayDstIP: net.IP{6, 6, 6, 6},
								OverlaySrcIP:  net.IP{169, 254, 0, 4},
								OverlayDstIP:  net.IP{169, 254, 0, 5},
								DzIp:          net.IP{100, 0, 0, 2},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
								},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
							{
								Id:            503,
								UnderlaySrcIP: net.IP{7, 7, 7, 7},
								UnderlayDstIP: net.IP{8, 8, 8, 8},
								OverlaySrcIP:  net.IP{169, 254, 0, 6},
								OverlayDstIP:  net.IP{169, 254, 0, 7},
								DzIp:          net.IP{100, 0, 0, 3},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastSubscribers: []net.IP{},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
						},
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						Vpn4vLoopbackIntfName: "Loopback255",
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						DevicePathologies:     []string{},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/mixed.tunnel.tmpl",
		},
		{
			Name:        "get_config_nohardware_tunnels_successfully",
			Description: "get config for a mix of unicast and multicast tunnels with no hardware option",
			NoHardware:  true,
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Devices: map[string]*Device{
					"abc123": {
						ExchangeCode: "tst",
						BgpCommunity: 10050,
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{1, 1, 1, 1},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{169, 254, 0, 0},
								OverlayDstIP:  net.IP{169, 254, 0, 1},
								DzIp:          net.IP{100, 0, 0, 0},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastPublishers: []net.IP{},
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{4, 4, 4, 4},
								OverlaySrcIP:  net.IP{169, 254, 0, 2},
								OverlayDstIP:  net.IP{169, 254, 0, 3},
								DzIp:          net.IP{100, 0, 0, 1},
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{5, 5, 5, 5},
								UnderlayDstIP: net.IP{6, 6, 6, 6},
								OverlaySrcIP:  net.IP{169, 254, 0, 4},
								OverlayDstIP:  net.IP{169, 254, 0, 5},
								DzIp:          net.IP{100, 0, 0, 2},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 3},
									{239, 0, 0, 4},
								},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
							{
								Id:            503,
								UnderlaySrcIP: net.IP{7, 7, 7, 7},
								UnderlayDstIP: net.IP{8, 8, 8, 8},
								OverlaySrcIP:  net.IP{169, 254, 0, 6},
								OverlayDstIP:  net.IP{169, 254, 0, 7},
								DzIp:          net.IP{100, 0, 0, 3},
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
								MulticastSubscribers: []net.IP{},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
						},
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						Vpn4vLoopbackIntfName: "Loopback255",
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						DevicePathologies:     []string{},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/nohardware.tunnel.tmpl",
		},
		{
			Name:        "render_base_config_successfully",
			Description: "render base configuration with BGP peers",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{15, 15, 15, 15},
						PeerName: "remote-dzd",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "remote-dzd",
					},
				},
				Devices: map[string]*Device{
					"abc123": {
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						Ipv4LoopbackIP:        net.IP{13, 13, 13, 13},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIntfName:  "Loopback256",
						DevicePathologies:     []string{},
						Tunnels:               []*Tunnel{},
						TunnelSlots:           0,
						ExchangeCode:          "tst",
						BgpCommunity:          10050,
						Interfaces: []Interface{
							{
								Name:           "Loopback255",
								InterfaceType:  InterfaceTypeLoopback,
								LoopbackType:   LoopbackTypeVpnv4,
								Ip:             netip.MustParsePrefix("14.14.14.14/32"),
								NodeSegmentIdx: 15,
							},
							{
								Name:          "Ethernet1/1",
								InterfaceType: InterfaceTypePhysical,
								Ip:            netip.MustParsePrefix("172.16.0.2/31"),
								Metric:        40000,
								IsLink:        true,
							},
							{
								Name:          "Ethernet1/2",
								InterfaceType: InterfaceTypePhysical,
								Ip:            netip.MustParsePrefix("172.16.0.4/31"),
								Metric:        40000,
								IsLink:        false, // make sure we don't render an isis config since it's not in a link
							},
						},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/base.config.txt",
		},
		{
			Name:        "render_base_config_with_mgmt_vrf_successfully",
			Description: "render base configuration with BGP peers",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{15, 15, 15, 15},
						PeerName: "remote-dzd",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "remote-dzd",
					},
				},
				Devices: map[string]*Device{
					"abc123": {
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
						Ipv4LoopbackIP:        net.IP{13, 13, 13, 13},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIntfName:  "Loopback256",
						DevicePathologies:     []string{},
						Tunnels:               []*Tunnel{},
						TunnelSlots:           0,
						MgmtVrf:               "test-mgmt-vrf",
						ExchangeCode:          "tst",
						BgpCommunity:          10050,
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/base.config.with.mgmt.vrf.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			controller := &Controller{
				noHardware:     test.NoHardware,
				deviceLocalASN: 65342,
			}
			pb.RegisterControllerServer(server, controller)

			go func() {
				if err := server.Serve(listener); err != nil {
					log.Fatal(err)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			opts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return listener.Dial()
				}),
			}
			conn, err := grpc.NewClient("passthrough://bufnet", opts...)
			if err != nil {
				t.Fatalf("error creating controller client: %v", err)
			}
			defer conn.Close()
			defer cancel()

			agent := pb.NewControllerClient(conn)

			// update the state cache in the controller per the test
			controller.swapCache(test.StateCache)

			// grab the test fixture for the expected rendered config
			var want []byte
			if strings.HasSuffix(test.Want, ".tmpl") {
				templateData := map[string]int{
					"StartTunnel": config.StartUserTunnelNum,
					"EndTunnel":   config.StartUserTunnelNum + config.MaxUserTunnelSlots - 1,
				}
				rendered, err := renderTemplateFile(test.Want, templateData)
				if err != nil {
					t.Fatalf("error rendering test fixture %s: %v", test.Want, err)
				}
				want = []byte(rendered)
			} else {
				var err error
				want, err = os.ReadFile(test.Want)
				if err != nil {
					t.Fatalf("error reading test fixture %s: %v", test.Want, err)
				}
			}

			// get fetch the rendered config for the device's pubkey
			got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: test.Pubkey})
			if err != nil {
				t.Errorf("error while fetching config: %v", err)
			}
			if diff := cmp.Diff(string(want), got.GetConfig()); diff != "" {
				t.Errorf("GetConfig mismatch in fixture %s (-want +got): %s\n", test.Want, diff)
			}
		})
	}
}

func TestGetConfigWithPathologies(t *testing.T) {
	tests := []struct {
		Name              string
		Description       string
		StateCache        stateCache
		Pubkey            string
		ExpectedErrorCode string
		ExpectedErrorMsg  string
	}{
		{
			Name:        "device_with_pathologies_returns_failed_precondition",
			Description: "GetConfig should return FailedPrecondition error for device with pathologies",
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				UnicastVrfs: []uint16{1},
				Devices: map[string]*Device{
					"abc123": {
						PubKey:   "abc123",
						PublicIP: net.IP{1, 2, 3, 4},
						DevicePathologies: []string{
							"no or invalid VPNv4 loopback interface found for device",
							"ISIS NET could not be generated",
						},
						ExchangeCode: "tst",
						BgpCommunity: 10050,
					},
				},
			},
			Pubkey:            "abc123",
			ExpectedErrorCode: "FailedPrecondition",
			ExpectedErrorMsg:  "cannot render config for device abc123:",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			controller := &Controller{
				log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
				deviceLocalASN: 65342,
			}
			pb.RegisterControllerServer(server, controller)

			go func() {
				if err := server.Serve(listener); err != nil {
					log.Fatal(err)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			opts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return listener.Dial()
				}),
			}
			conn, err := grpc.NewClient("passthrough://bufnet", opts...)
			if err != nil {
				t.Fatalf("error creating controller client: %v", err)
			}
			defer conn.Close()
			defer cancel()

			agent := pb.NewControllerClient(conn)

			// update the state cache in the controller per the test
			controller.swapCache(test.StateCache)

			// attempt to fetch config and verify it returns the expected error
			_, err = agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: test.Pubkey})
			if err == nil {
				t.Errorf("expected error but got nil")
				return
			}

			if !strings.Contains(err.Error(), test.ExpectedErrorCode) {
				t.Errorf("expected error to contain '%s', got: %v", test.ExpectedErrorCode, err)
			}

			if !strings.Contains(err.Error(), test.ExpectedErrorMsg) {
				t.Errorf("expected error to contain '%s', got: %v", test.ExpectedErrorMsg, err)
			}
		})
	}
}

type mockServiceabilityProgramClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
	ProgramIDFunc      func() solana.PublicKey
}

func (m *mockServiceabilityProgramClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func (m *mockServiceabilityProgramClient) ProgramID() solana.PublicKey {
	return m.ProgramIDFunc()
}

func TestStateCache(t *testing.T) {
	tests := []struct {
		Name            string
		Description     string
		GlobalConfig    serviceability.GlobalConfig
		Users           []serviceability.User
		Devices         []serviceability.Device
		Links           []serviceability.Link
		MulticastGroups []serviceability.MulticastGroup
		Exchanges       []serviceability.Exchange
		Tenants         []serviceability.Tenant
		StateCache      stateCache
	}{
		{
			Name: "populate_device_cache_successfully",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
				},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{147, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{10, 1, 1, 2, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
				{
					// Should not be added to StateCache due to invalid ClientIp
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{0, 0, 0, 0},
					DzIp:         [4]uint8{147, 100, 100, 102},
					TunnelId:     uint16(502),
					TunnelNet:    [5]uint8{10, 1, 1, 3, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
				{
					// Should not be added to StateCache due to invalid DzIp
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{5, 5, 5, 5},
					DzIp:         [4]uint8{0, 0, 0, 0},
					TunnelId:     uint16(502),
					TunnelNet:    [5]uint8{10, 1, 1, 4, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
				{
					// Should not be added to StateCache due to invalid ClientIp and DzIp
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{0, 0, 0, 0},
					DzIp:         [4]uint8{0, 0, 0, 0},
					TunnelId:     uint16(502),
					TunnelNet:    [5]uint8{10, 1, 1, 5, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
				{
					// Should not be added to StateCache due to martian DzIp (10.x.x.x)
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{6, 6, 6, 6},
					DzIp:         [4]uint8{10, 0, 0, 1},
					TunnelId:     uint16(503),
					TunnelNet:    [5]uint8{10, 1, 1, 6, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					// Should not be added to StateCache due to martian DzIp (127.x.x.x)
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{7, 7, 7, 7},
					DzIp:         [4]uint8{127, 0, 0, 1},
					TunnelId:     uint16(504),
					TunnelNet:    [5]uint8{10, 1, 1, 8, 31},
					Status:       serviceability.UserStatusActivated,
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Interfaces: []serviceability.Interface{
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{14, 14, 14, 14, 32},
							Name:          "Loopback255",
						},
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{12, 12, 12, 12, 32},
							Name:          "Loopback256",
						},
						{
							InterfaceType: serviceability.InterfaceTypePhysical,
							Name:          "Ethernet1/1",
							IpNet:         [5]uint8{172, 16, 0, 2, 31},
							Status:        serviceability.InterfaceStatusActivated,
						},
						{
							InterfaceType: serviceability.InterfaceTypePhysical,
							Name:          "Ethernet1/2",
							IpNet:         [5]uint8{172, 16, 0, 4, 31},
							Status:        serviceability.InterfaceStatusActivated,
						},
						{
							InterfaceType: serviceability.InterfaceTypePhysical,
							Name:          "Ethernet1/3",
							IpNet:         [5]uint8{172, 16, 0, 6, 31},
							Status:        serviceability.InterfaceStatusActivated,
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
			},
			Links: []serviceability.Link{
				{
					AccountType:     serviceability.LinkType,
					Owner:           [32]uint8{},
					SideAPubKey:     [32]uint8{1},
					SideZPubKey:     [32]uint8{2},
					DelayNs:         400000000,
					DelayOverrideNs: 0,
					Status:          serviceability.LinkStatusActivated,
					SideAIfaceName:  "Ethernet1/1",
					SideZIfaceName:  "Ethernet1/1",
				},
				{
					AccountType:     serviceability.LinkType,
					Owner:           [32]uint8{},
					SideAPubKey:     [32]uint8{1},
					SideZPubKey:     [32]uint8{2},
					DelayNs:         1000,
					DelayOverrideNs: 0,
					Status:          serviceability.LinkStatusActivated,
					SideAIfaceName:  "Ethernet1/2",
					SideZIfaceName:  "Ethernet1/2",
				},
				{
					AccountType:     serviceability.LinkType,
					Owner:           [32]uint8{},
					SideAPubKey:     [32]uint8{1},
					SideZPubKey:     [32]uint8{2},
					DelayNs:         1000,
					DelayOverrideNs: 50000,
					Status:          serviceability.LinkStatusActivated,
					SideAIfaceName:  "Ethernet1/3",
					SideZIfaceName:  "Ethernet1/3",
				},
			},
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:      [32]uint8{1},
						MulticastIp: [4]uint8{239, 0, 0, 1},
					},
				},
				Tenants:     map[string]serviceability.Tenant{},
				UnicastVrfs: []uint16{1},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{14, 14, 14, 14},
						PeerName: "abc01",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "abc01",
					},
				},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:            "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP:          net.IP{2, 2, 2, 2},
						Vpn4vLoopbackIP:   net.IP{14, 14, 14, 14},
						IsisNet:           "49.0000.0e0e.0e0e.0000.00",
						Ipv4LoopbackIP:    net.IP{12, 12, 12, 12},
						DevicePathologies: []string{},
						ExchangeCode:      "tst",
						BgpCommunity:      10050,
						Tunnels: append([]*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{147, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{3, 3, 3, 3},
								OverlaySrcIP:  net.IP{10, 1, 1, 2},
								OverlayDstIP:  net.IP{10, 1, 1, 3},
								DzIp:          net.IP{147, 100, 100, 101},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								IsMulticast:   true,
								MulticastBoundaryList: []net.IP{
									{239, 0, 0, 1},
								},
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 1},
								},
							},
						}, generateEmptyTunnelSlots(config.StartUserTunnelNum+2, config.MaxUserTunnelSlots-2)...),
						TunnelSlots: config.MaxUserTunnelSlots,
						Interfaces: []Interface{
							{
								InterfaceType: InterfaceTypePhysical,
								Ip:            netip.MustParsePrefix("172.16.0.2/31"),
								Name:          "Ethernet1/1",
								IsLink:        true,
								Metric:        400000,
								LinkStatus:    serviceability.LinkStatusActivated,
							},
							{
								InterfaceType: InterfaceTypePhysical,
								Ip:            netip.MustParsePrefix("172.16.0.4/31"),
								Name:          "Ethernet1/2",
								IsLink:        true,
								Metric:        1,
								LinkStatus:    serviceability.LinkStatusActivated,
							},
							{
								InterfaceType: InterfaceTypePhysical,
								Ip:            netip.MustParsePrefix("172.16.0.6/31"),
								Name:          "Ethernet1/3",
								IsLink:        true,
								Metric:        50,
								LinkStatus:    serviceability.LinkStatusActivated,
							},
							{
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeVpnv4,
								Ip:            netip.MustParsePrefix("14.14.14.14/32"),
								Name:          "Loopback255",
							},
							{
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeIpv4,
								Ip:            netip.MustParsePrefix("12.12.12.12/32"),
								Name:          "Loopback256",
							},
						},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIntfName:  "Loopback256",
						Status:                serviceability.DeviceStatusActivated,
						Code:                  "abc01",
						ContributorCode:       "unknown",
						LocationCode:          "unknown",
					},
				},
			},
		},
		{
			Name: "device_with_pathologies_added_to_cache",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{3, 3, 3, 3},
					Interfaces:     []serviceability.Interface{}, // No VPNv4 loopback interface
					Status:         serviceability.DeviceStatusActivated,
					Code:           "abc02",
					PubKey:         [32]byte{1},
				},
			},
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{},
				Tenants:         map[string]serviceability.Tenant{},
				UnicastVrfs:     []uint16{1},
				Vpnv4BgpPeers:   nil, // No BGP peers since device has pathologies
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP: net.IP{3, 3, 3, 3},
						DevicePathologies: []string{
							"no or invalid VPNv4 loopback interface found for device",
							"no or invalid IPv4 loopback interface found for device",
							"ISIS NET could not be generated",
						},
						ExchangeCode:    "tst",
						BgpCommunity:    10050,
						Status:          serviceability.DeviceStatusActivated,
						Code:            "abc02",
						ContributorCode: "unknown",
						LocationCode:    "unknown",
						Tunnels: append([]*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{147, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
						}, generateEmptyTunnelSlots(config.StartUserTunnelNum+1, config.MaxUserTunnelSlots-1)...),
						TunnelSlots: config.MaxUserTunnelSlots,
					},
				},
			},
		},
		{
			Name: "device_with_out_of_range_exchange_bgp_community_pathology",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 5000, // Out of valid range (10000-10999)
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{3},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{3, 3, 3, 3},
					Interfaces: []serviceability.Interface{
						{
							Name:          "Loopback255",
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{10, 10, 10, 1, 32},
						},
						{
							Name:          "Loopback256",
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{10, 10, 10, 2, 32},
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc03",
					PubKey: [32]byte{1},
				},
			},
			Links: []serviceability.Link{},
			StateCache: stateCache{
				GlobalConfig: serviceability.GlobalConfig{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{},
				Tenants:         map[string]serviceability.Tenant{},
				UnicastVrfs:     []uint16{1},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP: net.IP{3, 3, 3, 3},
						DevicePathologies: []string{
							"exchange BGP community 5000 is out of valid range (10000-10999)",
						},
						ExchangeCode:          "tst",
						BgpCommunity:          5000,
						Status:                serviceability.DeviceStatusActivated,
						Code:                  "abc03",
						ContributorCode:       "unknown",
						LocationCode:          "unknown",
						Vpn4vLoopbackIP:       net.IP{10, 10, 10, 1},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIP:        net.IP{10, 10, 10, 2},
						Ipv4LoopbackIntfName:  "Loopback256",
						IsisNet:               "49.0000.0a0a.0a01.0000.00",
						Interfaces: []Interface{
							{
								Name:          "Loopback255",
								Ip:            netip.MustParsePrefix("10.10.10.1/32"),
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeVpnv4,
							},
							{
								Name:          "Loopback256",
								Ip:            netip.MustParsePrefix("10.10.10.2/32"),
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeIpv4,
							},
						},
						Tunnels: append([]*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{147, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
						}, generateEmptyTunnelSlots(config.StartUserTunnelNum+1, config.MaxUserTunnelSlots-1)...),
						TunnelSlots: config.MaxUserTunnelSlots,
					},
				},
			},
		},
		{
			Name: "user_with_explicit_tunnel_endpoint_uses_that_ip",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					UserType:       serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey:   [32]uint8{1},
					CyoaType:       serviceability.CyoaTypeGREOverDIA,
					ClientIp:       [4]uint8{1, 1, 1, 1},
					DzIp:           [4]uint8{147, 100, 100, 100},
					TunnelId:       uint16(500),
					TunnelNet:      [5]uint8{10, 1, 1, 0, 31},
					Status:         serviceability.UserStatusActivated,
					TunnelEndpoint: [4]uint8{5, 5, 5, 5}, // Explicit tunnel endpoint
				},
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					UserType:       serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey:   [32]uint8{1},
					CyoaType:       serviceability.CyoaTypeGREOverDIA,
					ClientIp:       [4]uint8{2, 2, 2, 2},
					DzIp:           [4]uint8{147, 100, 100, 101},
					TunnelId:       uint16(501),
					TunnelNet:      [5]uint8{10, 1, 1, 2, 31},
					Status:         serviceability.UserStatusActivated,
					TunnelEndpoint: [4]uint8{0, 0, 0, 0}, // Unspecified - should fall back to device PublicIP
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{3},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{3, 3, 3, 3},
					Interfaces: []serviceability.Interface{
						{
							Name:          "Loopback255",
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{10, 10, 10, 1, 32},
						},
						{
							Name:          "Loopback256",
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{10, 10, 10, 2, 32},
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
			},
			Links: []serviceability.Link{},
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{},
				Tenants:         map[string]serviceability.Tenant{},
				UnicastVrfs:     []uint16{1},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{10, 10, 10, 1},
						PeerName: "abc01",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{10, 10, 10, 2},
						PeerName: "abc01",
					},
				},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:                "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP:              net.IP{3, 3, 3, 3},
						Vpn4vLoopbackIP:       net.IP{10, 10, 10, 1},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIP:        net.IP{10, 10, 10, 2},
						Ipv4LoopbackIntfName:  "Loopback256",
						IsisNet:               "49.0000.0a0a.0a01.0000.00",
						DevicePathologies:     []string{},
						ExchangeCode:          "tst",
						BgpCommunity:          10050,
						Status:                serviceability.DeviceStatusActivated,
						Code:                  "abc01",
						ContributorCode:       "unknown",
						LocationCode:          "unknown",
						Interfaces: []Interface{
							{
								Name:          "Loopback255",
								Ip:            netip.MustParsePrefix("10.10.10.1/32"),
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeVpnv4,
							},
							{
								Name:          "Loopback256",
								Ip:            netip.MustParsePrefix("10.10.10.2/32"),
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeIpv4,
							},
						},
						Tunnels: append([]*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{5, 5, 5, 5}, // Uses explicit TunnelEndpoint
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{147, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3}, // Falls back to device PublicIP
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{10, 1, 1, 2},
								OverlayDstIP:  net.IP{10, 1, 1, 3},
								DzIp:          net.IP{147, 100, 100, 101},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
						}, generateEmptyTunnelSlots(config.StartUserTunnelNum+2, config.MaxUserTunnelSlots-2)...),
						TunnelSlots: config.MaxUserTunnelSlots,
					},
				},
			},
		},
		{
			Name: "tenant_vrf_assignment",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Tenants: []serviceability.Tenant{
				{
					PubKey: [32]byte{10},
					VrfId:  1,
				},
				{
					PubKey: [32]byte{20},
					VrfId:  2,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					TenantPubKey: [32]uint8{10},
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					TenantPubKey: [32]uint8{20},
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{2, 2, 2, 2},
					DzIp:         [4]uint8{147, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{10, 1, 1, 2, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					TenantPubKey: [32]uint8{99},
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{147, 100, 100, 102},
					TunnelId:     uint16(502),
					TunnelNet:    [5]uint8{10, 1, 1, 4, 31},
					Status:       serviceability.UserStatusActivated,
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         serviceability.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
					Interfaces: []serviceability.Interface{
						{
							Name:           "Loopback255",
							InterfaceType:  serviceability.InterfaceTypeLoopback,
							LoopbackType:   serviceability.LoopbackTypeVpnv4,
							IpNet:          [5]uint8{14, 14, 14, 14, 32},
							NodeSegmentIdx: 101,
						},
						{
							Name:          "Loopback256",
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{12, 12, 12, 12, 32},
						},
					},
				},
			},
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{},
				Tenants: map[string]serviceability.Tenant{
					"g35TxFqwMx95vCk63fTxGTHb6ei4W24qg5t2x6xD3cT": {
						PubKey: [32]byte{10},
						VrfId:  1,
					},
					"2M59vuWgsiuHAqQVB6KvuXuaBCJR8138gMAm4uCuR6Du": {
						PubKey: [32]byte{20},
						VrfId:  2,
					},
				},
				UnicastVrfs: []uint16{1, 2},
				Vpnv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{14, 14, 14, 14},
						PeerName: "abc01",
					},
				},
				Ipv4BgpPeers: []BgpPeer{
					{
						PeerIP:   net.IP{12, 12, 12, 12},
						PeerName: "abc01",
					},
				},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:            "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP:          net.IP{2, 2, 2, 2},
						Vpn4vLoopbackIP:   net.IP{14, 14, 14, 14},
						IsisNet:           "49.0000.0e0e.0e0e.0000.00",
						Ipv4LoopbackIP:    net.IP{12, 12, 12, 12},
						DevicePathologies: []string{},
						ExchangeCode:      "tst",
						BgpCommunity:      10050,
						Tunnels: append([]*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{147, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{10, 1, 1, 2},
								OverlayDstIP:  net.IP{10, 1, 1, 3},
								DzIp:          net.IP{147, 100, 100, 101},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         2,
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{3, 3, 3, 3},
								OverlaySrcIP:  net.IP{10, 1, 1, 4},
								OverlayDstIP:  net.IP{10, 1, 1, 5},
								DzIp:          net.IP{147, 100, 100, 102},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
								VrfId:         1,
								MetroRouting:  true,
							},
						}, generateEmptyTunnelSlots(config.StartUserTunnelNum+3, config.MaxUserTunnelSlots-3)...),
						TunnelSlots: config.MaxUserTunnelSlots,
						Interfaces: []Interface{
							{
								InterfaceType:  InterfaceTypeLoopback,
								LoopbackType:   LoopbackTypeVpnv4,
								Ip:             netip.MustParsePrefix("14.14.14.14/32"),
								Name:           "Loopback255",
								NodeSegmentIdx: 101,
							},
							{
								InterfaceType: InterfaceTypeLoopback,
								LoopbackType:  LoopbackTypeIpv4,
								Ip:            netip.MustParsePrefix("12.12.12.12/32"),
								Name:          "Loopback256",
							},
						},
						Vpn4vLoopbackIntfName: "Loopback255",
						Ipv4LoopbackIntfName:  "Loopback256",
						Status:                serviceability.DeviceStatusActivated,
						Code:                  "abc01",
						ContributorCode:       "unknown",
						LocationCode:          "unknown",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				log.Fatalf("failed to listen: %v", err)
			}

			m := &mockServiceabilityProgramClient{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						GlobalConfig:    &test.GlobalConfig,
						Users:           test.Users,
						Devices:         test.Devices,
						Links:           test.Links,
						MulticastGroups: test.MulticastGroups,
						Exchanges:       test.Exchanges,
						Tenants:         test.Tenants,
					}, nil
				},
				ProgramIDFunc: func() solana.PublicKey {
					return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
				},
			}
			controller, err := NewController(
				WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				WithServiceabilityProgramClient(m),
				WithListener(lis),
				WithDeviceLocalASN(65342),
			)
			if err != nil {
				t.Fatalf("error creating controller: %v", err)
			}
			if err := controller.updateStateCache(context.Background()); err != nil {
				t.Fatalf("error populating state cache: %v", err)
			}
			if diff := cmp.Diff(test.StateCache, controller.cache, cmpopts.EquateComparable(netip.Prefix{})); diff != "" {
				t.Errorf("StateCache mismatch (-want +got): %s\n", diff)
			}
		})
	}
}

func TestServiceabilityProgramClientArg(t *testing.T) {
	tests := []struct {
		name                 string
		serviceabilityClient ServiceabilityProgramClient
		wantErr              error
	}{
		{
			name:                 "verify_default_program_id_and_rpc_url_are_set",
			serviceabilityClient: nil,
			wantErr:              ErrServiceabilityRequired,
		},
		{
			name: "verify_custom_program_id_and_rpc_url_are_set",
			serviceabilityClient: &mockServiceabilityProgramClient{
				ProgramIDFunc: func() solana.PublicKey {
					return solana.MustPublicKeyFromBase58("mycustomprogramidthatneeds32charssohere1234")
				},
			},
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := []Option{
				WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				WithListener(bufconn.Listen(1024 * 1024)),
			}

			opts = append(opts, WithServiceabilityProgramClient(test.serviceabilityClient))
			_, err := NewController(opts...)
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
		})
	}
}

// TestEndToEnd verifies on-chain data can be fetched, the local state cache updated, and a config
// can be rendered and sent back to the client via gRPC.
func TestEndToEnd(t *testing.T) {
	tests := []struct {
		Name            string
		GlobalConfig    serviceability.GlobalConfig
		Users           []serviceability.User
		Devices         []serviceability.Device
		Links           []serviceability.Link
		MulticastGroups []serviceability.MulticastGroup
		Exchanges       []serviceability.Exchange
		Tenants         []serviceability.Tenant
		AgentRequest    *pb.ConfigRequest
		DevicePubKey    string
		Want            string
	}{
		{
			Name: "fetch_accounts_and_render_config_successfully",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
				},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{147, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{169, 254, 0, 2, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         serviceability.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
					Interfaces: []serviceability.Interface{
						{
							Name:               "Loopback255",
							InterfaceType:      serviceability.InterfaceTypeLoopback,
							LoopbackType:       serviceability.LoopbackTypeVpnv4,
							IpNet:              [5]uint8{14, 14, 14, 14, 32},
							NodeSegmentIdx:     101,
							UserTunnelEndpoint: false,
						},
						{
							Name:               "Loopback256",
							InterfaceType:      serviceability.InterfaceTypeLoopback,
							LoopbackType:       serviceability.LoopbackTypeIpv4,
							IpNet:              [5]uint8{12, 12, 12, 12, 32},
							UserTunnelEndpoint: false,
						},
						{
							Name:          "Switch1/1/1",
							InterfaceType: serviceability.InterfaceTypePhysical,
							IpNet:         [5]uint8{172, 16, 0, 0, 31},
						},
						{
							Name:          "Switch1/1/2.100",
							InterfaceType: serviceability.InterfaceTypePhysical,
							VlanId:        100,
							IpNet:         [5]uint8{172, 16, 0, 2, 31},
						},
						{
							Name:          "Switch1/1/3",
							InterfaceType: serviceability.InterfaceTypePhysical,
							IpNet:         [5]uint8{172, 16, 0, 4, 31},
						},
					},
				},
			},
			Links: []serviceability.Link{
				{
					AccountType:    serviceability.LinkType,
					Owner:          [32]uint8{},
					SideAPubKey:    [32]uint8{1},
					SideZPubKey:    [32]uint8{2},
					DelayNs:        400000000,
					Status:         serviceability.LinkStatusActivated,
					SideAIfaceName: "Switch1/1/1",
					SideZIfaceName: "Switch1/1/1",
				},
				{
					AccountType:    serviceability.LinkType,
					Owner:          [32]uint8{},
					SideAPubKey:    [32]uint8{1},
					SideZPubKey:    [32]uint8{2},
					DelayNs:        1000,
					Status:         serviceability.LinkStatusActivated,
					SideAIfaceName: "Switch1/1/2.100",
					SideZIfaceName: "Switch1/1/2.100",
				},
				{
					AccountType:     serviceability.LinkType,
					Owner:           [32]uint8{},
					SideAPubKey:     [32]uint8{1},
					SideZPubKey:     [32]uint8{2},
					DelayNs:         1000,
					DelayOverrideNs: 50000,
					Status:          serviceability.LinkStatusActivated,
					SideAIfaceName:  "Switch1/1/3",
					SideZIfaceName:  "Switch1/1/3",
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{},
			},
			Want: "fixtures/e2e.tmpl",
		},
		{
			Name: "remove_unknown_peers_successfully",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				DeviceTunnelBlock:   [5]uint8{172, 16, 0, 0, 16},
				UserTunnelBlock:     [5]uint8{169, 254, 0, 0, 16},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
				},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{147, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{169, 254, 0, 2, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Interfaces: []serviceability.Interface{
						{
							InterfaceType:  serviceability.InterfaceTypeLoopback,
							LoopbackType:   serviceability.LoopbackTypeVpnv4,
							IpNet:          [5]uint8{14, 14, 14, 14, 32},
							Name:           "Loopback255",
							NodeSegmentIdx: 101,
						},
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{12, 12, 12, 12, 32},
							Name:          "Loopback256",
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{22, 22, 22, 22},
					Interfaces: []serviceability.Interface{
						// Because this device does not also have an Ipv4 loopback interface, this peer should not be added to abc01's peers
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{114, 114, 114, 114, 32},
							Name:          "Loopback255",
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc02",
					PubKey: [32]byte{2},
				},
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{23, 23, 23, 23},
					Interfaces: []serviceability.Interface{
						// Because this device does not also have an Vpnv4 loopback interface, this peer should not be added to abc01's peers
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{124, 124, 124, 124, 32},
							Name:          "Loopback256",
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc03",
					PubKey: [32]byte{3},
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey: "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{
					"10.0.0.1",    // Not in any DZ block - should not be flagged for removal
					"172.17.0.1",  // Not in any DZ block - should not be flagged for removal
					"172.16.0.1",  // In DeviceTunnelBlock - should be flagged for removal
					"169.254.0.7", // In UserTunnelBlock - should be flagged for removal
					"169.254.0.3", // In UserTunnelBlock, but associated with a user - should not be flagged for removal
				},
			},
			Want: "fixtures/e2e.peer.removal.tmpl",
		},
		{
			Name: "remove_last_user_from_device",
			GlobalConfig: serviceability.GlobalConfig{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				DeviceTunnelBlock:   [5]uint8{172, 16, 0, 0, 16},
				UserTunnelBlock:     [5]uint8{169, 254, 0, 0, 16},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Users: []serviceability.User{},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Interfaces: []serviceability.Interface{
						{
							InterfaceType:  serviceability.InterfaceTypeLoopback,
							LoopbackType:   serviceability.LoopbackTypeVpnv4,
							IpNet:          [5]uint8{14, 14, 14, 14, 32},
							Name:           "Loopback255",
							NodeSegmentIdx: 101,
						},
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{12, 12, 12, 12, 32},
							Name:          "Loopback256",
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey: "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{
					"10.0.0.1",
					"172.16.0.1",
					"169.254.0.13",
				},
			},
			Want: "fixtures/e2e.last.user.tmpl",
		},
		{
			Name: "tenant_vrf_end_to_end",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
				},
			},
			Exchanges: []serviceability.Exchange{
				{
					PubKey:       [32]uint8{2},
					Code:         "tst",
					BgpCommunity: 10050,
				},
			},
			Tenants: []serviceability.Tenant{
				{
					PubKey:       [32]byte{10},
					VrfId:        1,
					MetroRouting: true,
				},
				{
					PubKey:       [32]byte{20},
					VrfId:        2,
					MetroRouting: true,
				},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					TenantPubKey: [32]uint8{10},
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{147, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       serviceability.UserStatusActivated,
				},
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					TenantPubKey: [32]uint8{20},
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{2, 2, 2, 2},
					DzIp:         [4]uint8{147, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{169, 254, 0, 2, 31},
					Status:       serviceability.UserStatusActivated,
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{2},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Interfaces: []serviceability.Interface{
						{
							InterfaceType:  serviceability.InterfaceTypeLoopback,
							LoopbackType:   serviceability.LoopbackTypeVpnv4,
							IpNet:          [5]uint8{14, 14, 14, 14, 32},
							Name:           "Loopback255",
							NodeSegmentIdx: 101,
						},
						{
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeIpv4,
							IpNet:         [5]uint8{12, 12, 12, 12, 32},
							Name:          "Loopback256",
						},
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{},
			},
			Want: "fixtures/e2e.multi.vrf.tmpl",
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			m := &mockServiceabilityProgramClient{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						GlobalConfig:    &test.GlobalConfig,
						Users:           test.Users,
						Devices:         test.Devices,
						Links:           test.Links,
						MulticastGroups: test.MulticastGroups,
						Exchanges:       test.Exchanges,
						Tenants:         test.Tenants,
					}, nil
				},
				ProgramIDFunc: func() solana.PublicKey {
					return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
				},
			}
			var controller *Controller
			var err error
			controller, err = NewController(
				WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				WithServiceabilityProgramClient(m),
				WithListener(listener),
				WithSignalChan(make(chan struct{})),
				WithDeviceLocalASN(65342),
			)
			if err != nil {
				t.Fatalf("error creating controller: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			opts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return listener.Dial()
				}),
			}
			conn, err := grpc.NewClient("passthrough://bufnet", opts...)
			if err != nil {
				t.Fatalf("error creating controller client: %v", err)
			}
			defer conn.Close()
			defer cancel()

			agent := pb.NewControllerClient(conn)

			ctx, cancel = context.WithCancel(context.Background())
			go func() {
				if err := controller.Run(ctx); err != nil {
					log.Fatalf("error starting controller: %v", err)
				}
			}()
			defer cancel()

			ticker := time.NewTicker(5 * time.Second)
			select {
			case <-controller.updateDone:
			case <-ticker.C:
				t.Fatalf("timed out waiting for state cache update")
			}

			var want []byte
			if strings.HasSuffix(test.Want, ".tmpl") {
				templateData := map[string]int{
					"StartTunnel": config.StartUserTunnelNum,
					"EndTunnel":   config.StartUserTunnelNum + config.MaxUserTunnelSlots - 1,
				}
				rendered, err := renderTemplateFile(test.Want, templateData)
				if err != nil {
					t.Fatalf("error rendering test fixture %s: %v", test.Want, err)
				}
				want = []byte(rendered)
			} else {
				var err error
				want, err = os.ReadFile(test.Want)
				if err != nil {
					t.Fatalf("error reading test fixture %s: %v", test.Want, err)
				}
			}

			got, err := agent.GetConfig(ctx, test.AgentRequest)
			if err != nil {
				t.Fatalf("error while fetching config: %v", err)
			}
			if diff := cmp.Diff(string(want), got.Config); diff != "" {
				t.Errorf("Config mismatch in fixture %s (-want +got):\n%s", test.Want, diff)
			}
		})
	}
}
