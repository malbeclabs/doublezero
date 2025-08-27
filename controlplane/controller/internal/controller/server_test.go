package controller

import (
	"context"
	"log"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/gagliardetto/solana-go"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func TestGetConfig(t *testing.T) {
	tests := []struct {
		Name               string
		Description        string
		StateCache         stateCache
		NoHardware         bool
		InterfacesAndPeers bool
		Pubkey             string
		Want               string
	}{
		{
			Name:               "render_unicast_config_successfully",
			Description:        "render configuration for a set of unicast devices successfully",
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				Devices: map[string]*Device{
					"abc123": {
						Interfaces: []Interface{},
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{1, 1, 1, 1},
								UnderlayDstIP: net.IP{2, 2, 2, 2},
								OverlaySrcIP:  net.IP{169, 254, 0, 0},
								OverlayDstIP:  net.IP{169, 254, 0, 1},
								DzIp:          net.IP{100, 0, 0, 0},
								Allocated:     true,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{3, 3, 3, 3},
								UnderlayDstIP: net.IP{4, 4, 4, 4},
								OverlaySrcIP:  net.IP{169, 254, 0, 2},
								OverlayDstIP:  net.IP{169, 254, 0, 3},
								DzIp:          net.IP{100, 0, 0, 1},
								Allocated:     true,
							},
							{
								Id:            502,
								UnderlaySrcIP: net.IP{5, 5, 5, 5},
								UnderlayDstIP: net.IP{6, 6, 6, 6},
								OverlaySrcIP:  net.IP{169, 254, 0, 4},
								OverlayDstIP:  net.IP{169, 254, 0, 5},
								DzIp:          net.IP{100, 0, 0, 2},
								Allocated:     true,
							},
						},
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP:       net.IP{14, 14, 14, 14},
						Vpn4vLoopbackIntfName: "Loopback255",
						IsisNet:               "49.0000.0e0e.0e0e.0000.00",
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/unicast.tunnel.txt",
		},
		{
			Name:               "render_multicast_config_successfully",
			Description:        "render configuration for a set of multicast devices successfully",
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				Devices: map[string]*Device{
					"abc123": {
						Interfaces: []Interface{},
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
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/multicast.tunnel.txt",
		},
		{
			Name:               "get_config_mixed_tunnels_successfully",
			Description:        "get config for a mix of unicast and multicast tunnels",
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				Devices: map[string]*Device{
					"abc123": {
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
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/mixed.tunnel.txt",
		},
		{
			Name:               "get_config_nohardware_tunnels_successfully",
			Description:        "get config for a mix of unicast and multicast tunnels with no hardware option",
			NoHardware:         true,
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				Devices: map[string]*Device{
					"abc123": {
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
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/nohardware.tunnel.txt",
		},
		{
			Name:               "render_base_config_successfully",
			Description:        "render base configuration with BGP peers",
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
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
						Tunnels:               []*Tunnel{},
						TunnelSlots:           0,
						Interfaces: []Interface{
							{
								Name:           "Loopback255",
								InterfaceType:  InterfaceTypeLoopback,
								LoopbackType:   LoopbackTypeVpnv4,
								Ip:             netip.MustParsePrefix("14.14.14.14/32"),
								NodeSegmentIdx: 15,
							},
						},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/base.config.txt",
		},
		{
			Name:               "render_base_config_with_mgmt_vrf_successfully",
			Description:        "render base configuration with BGP peers",
			InterfacesAndPeers: true,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
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
						Tunnels:               []*Tunnel{},
						TunnelSlots:           0,
						MgmtVrf:               "test-mgmt-vrf",
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/base.config.with.mgmt.vrf.txt",
		},
		{
			Name:               "render_base_config_without_interfaces_and_peers_successfully",
			Description:        "render base configuration without interfaces and peers",
			InterfacesAndPeers: false,
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				Devices: map[string]*Device{
					"abc123": {
						PublicIP:              net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIntfName: "Loopback255",
						Tunnels:               []*Tunnel{},
						TunnelSlots:           0,
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/base.config.without.interfaces.peers.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			controller := &Controller{}
			if test.InterfacesAndPeers == true {
				controller = &Controller{
					noHardware:               test.NoHardware,
					enableInterfacesAndPeers: true,
				}
			} else {
				controller = &Controller{
					noHardware: test.NoHardware,
				}
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
			want, err := os.ReadFile(test.Want)
			if err != nil {
				t.Fatalf("error reading test fixture %s: %v", test.Want, err)
			}

			// get fetch the rendered config for the device's pubkey
			got, err := agent.GetConfig(ctx, &pb.ConfigRequest{Pubkey: test.Pubkey})
			if err != nil {
				t.Errorf("error while fetching config: %v", err)
			}
			if diff := cmp.Diff(string(want), got.GetConfig()); diff != "" {
				t.Errorf("GetConfig mismatch (-want +got): %s\n", diff)
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
		Config          serviceability.Config
		Users           []serviceability.User
		Devices         []serviceability.Device
		MulticastGroups []serviceability.MulticastGroup
		StateCache      stateCache
	}{
		{
			Name: "populate_device_cache_successfully",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
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
					DzIp:         [4]uint8{100, 100, 100, 100},
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
					DzIp:         [4]uint8{100, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{10, 1, 1, 2, 31},
					Status:       serviceability.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
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
					},
					Status: serviceability.DeviceStatusActivated,
					Code:   "abc01",
					PubKey: [32]byte{1},
				},
			},
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:      [32]uint8{1},
						MulticastIp: [4]uint8{239, 0, 0, 1},
						Subscribers: [][32]uint8{
							{1},
							{2},
						},
					},
				},
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
						PubKey:          "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP:        net.IP{2, 2, 2, 2},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
						IsisNet:         "49.0000.0e0e.0e0e.0000.00",
						Ipv4LoopbackIP:  net.IP{12, 12, 12, 12},
						Tunnels: []*Tunnel{
							{
								Id:            500,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{1, 1, 1, 1},
								OverlaySrcIP:  net.IP{10, 1, 1, 0},
								OverlayDstIP:  net.IP{10, 1, 1, 1},
								DzIp:          net.IP{100, 100, 100, 100},
								PubKey:        "11111111111111111111111111111111",
								Allocated:     true,
							},
							{
								Id:            501,
								UnderlaySrcIP: net.IP{2, 2, 2, 2},
								UnderlayDstIP: net.IP{3, 3, 3, 3},
								OverlaySrcIP:  net.IP{10, 1, 1, 2},
								OverlayDstIP:  net.IP{10, 1, 1, 3},
								DzIp:          net.IP{100, 100, 100, 101},
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
							{Id: 502},
							{Id: 503},
							{Id: 504},
							{Id: 505},
							{Id: 506},
							{Id: 507},
							{Id: 508},
							{Id: 509},
							{Id: 510},
							{Id: 511},
							{Id: 512},
							{Id: 513},
							{Id: 514},
							{Id: 515},
							{Id: 516},
							{Id: 517},
							{Id: 518},
							{Id: 519},
							{Id: 520},
							{Id: 521},
							{Id: 522},
							{Id: 523},
							{Id: 524},
							{Id: 525},
							{Id: 526},
							{Id: 527},
							{Id: 528},
							{Id: 529},
							{Id: 530},
							{Id: 531},
							{Id: 532},
							{Id: 533},
							{Id: 534},
							{Id: 535},
							{Id: 536},
							{Id: 537},
							{Id: 538},
							{Id: 539},
							{Id: 540},
							{Id: 541},
							{Id: 542},
							{Id: 543},
							{Id: 544},
							{Id: 545},
							{Id: 546},
							{Id: 547},
							{Id: 548},
							{Id: 549},
							{Id: 550},
							{Id: 551},
							{Id: 552},
							{Id: 553},
							{Id: 554},
							{Id: 555},
							{Id: 556},
							{Id: 557},
							{Id: 558},
							{Id: 559},
							{Id: 560},
							{Id: 561},
							{Id: 562},
							{Id: 563},
							{Id: 564},
							{Id: 565},
							{Id: 566},
							{Id: 567},
							{Id: 568},
							{Id: 569},
							{Id: 570},
							{Id: 571},
							{Id: 572},
							{Id: 573},
							{Id: 574},
							{Id: 575},
							{Id: 576},
							{Id: 577},
							{Id: 578},
							{Id: 579},
							{Id: 580},
							{Id: 581},
							{Id: 582},
							{Id: 583},
							{Id: 584},
							{Id: 585},
							{Id: 586},
							{Id: 587},
							{Id: 588},
							{Id: 589},
							{Id: 590},
							{Id: 591},
							{Id: 592},
							{Id: 593},
							{Id: 594},
							{Id: 595},
							{Id: 596},
							{Id: 597},
							{Id: 598},
							{Id: 599},
							{Id: 600},
							{Id: 601},
							{Id: 602},
							{Id: 603},
							{Id: 604},
							{Id: 605},
							{Id: 606},
							{Id: 607},
							{Id: 608},
							{Id: 609},
							{Id: 610},
							{Id: 611},
							{Id: 612},
							{Id: 613},
							{Id: 614},
							{Id: 615},
							{Id: 616},
							{Id: 617},
							{Id: 618},
							{Id: 619},
							{Id: 620},
							{Id: 621},
							{Id: 622},
							{Id: 623},
							{Id: 624},
							{Id: 625},
							{Id: 626},
							{Id: 627},
						},
						TunnelSlots: 128,
						Interfaces: []Interface{
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
					},
				},
			},
		},
		{
			Name: "exclude_device_without_vpnv4_loopback",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
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
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{3, 3, 3, 3},
					Interfaces:     []serviceability.Interface{}, // No VPNv4 loopback interface
					Status:         serviceability.DeviceStatusActivated,
					Code:           "abc02",
					PubKey:         [32]byte{1},
				},
			},
			StateCache: stateCache{
				Config: serviceability.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]serviceability.MulticastGroup{},
				Vpnv4BgpPeers:   nil,                  // No BGP peers since device is excluded
				Devices:         map[string]*Device{}, // Device should not be in cache
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
						Config:          test.Config,
						Users:           test.Users,
						Devices:         test.Devices,
						MulticastGroups: test.MulticastGroups,
					}, nil
				},
				ProgramIDFunc: func() solana.PublicKey {
					return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
				},
			}
			controller, err := NewController(
				WithServiceabilityProgramClient(m),
				WithListener(lis),
				WithEnableInterfacesAndPeers(),
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
		Name               string
		Config             serviceability.Config
		Users              []serviceability.User
		Devices            []serviceability.Device
		MulticastGroups    []serviceability.MulticastGroup
		InterfacesAndPeers bool
		AgentRequest       *pb.ConfigRequest
		DevicePubKey       string
		Want               string
	}{
		{
			Name: "fetch_accounts_and_render_config_successfully",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
				},
			},
			InterfacesAndPeers: true,
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
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
					DzIp:         [4]uint8{100, 100, 100, 101},
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
					ExchangePubKey: [32]uint8{},
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
					},
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{},
			},
			Want: "fixtures/e2e.txt",
		},
		{
			Name: "remove_unknown_peers_successfully",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				TunnelTunnelBlock:   [5]uint8{172, 16, 0, 0, 16},
				UserTunnelBlock:     [5]uint8{169, 254, 0, 0, 16},
			},
			MulticastGroups: []serviceability.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
				},
			},
			InterfacesAndPeers: true,
			Users: []serviceability.User{
				{
					AccountType:  serviceability.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     serviceability.UserUserType(serviceability.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     serviceability.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
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
					DzIp:         [4]uint8{100, 100, 100, 101},
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
					ExchangePubKey: [32]uint8{},
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
					PubKey: [32]byte{1},
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
					PubKey: [32]byte{1},
				},
			},
			AgentRequest: &pb.ConfigRequest{
				Pubkey: "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{
					"10.0.0.1",    // Not in any DZ block - should not be flagged for removal
					"172.17.0.1",  // Not in any DZ block - should not be flagged for removal
					"172.16.0.1",  // In TunnelTunnelBlock - should be flagged for removal
					"169.254.0.7", // In UserTunnelBlock - should be flagged for removal
					"169.254.0.3", // In UserTunnelBlock, but associated with a user - should not be flagged for removal
				},
			},
			Want: "fixtures/e2e.peer.removal.txt",
		},
		{
			Name: "base_config_without_interfaces_and_peers",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				TunnelTunnelBlock:   [5]uint8{172, 16, 0, 0, 16},
				UserTunnelBlock:     [5]uint8{169, 254, 0, 0, 16},
			},
			InterfacesAndPeers: false,
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
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
				Pubkey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
				BgpPeers: []string{},
			},
			Want: "fixtures/e2e.without.interfaces.peers.txt",
		},
		{
			Name: "remove_last_user_from_device",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				TunnelTunnelBlock:   [5]uint8{172, 16, 0, 0, 16},
				UserTunnelBlock:     [5]uint8{169, 254, 0, 0, 16},
			},
			InterfacesAndPeers: true,
			Users:              []serviceability.User{},
			Devices: []serviceability.Device{
				{
					AccountType:    serviceability.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
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
			Want: "fixtures/e2e.last.user.txt",
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			m := &mockServiceabilityProgramClient{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						Config:          test.Config,
						Users:           test.Users,
						Devices:         test.Devices,
						MulticastGroups: test.MulticastGroups,
					}, nil
				},
				ProgramIDFunc: func() solana.PublicKey {
					return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
				},
			}
			var controller *Controller
			var err error
			if test.InterfacesAndPeers {
				controller, err = NewController(
					WithServiceabilityProgramClient(m),
					WithListener(listener),
					WithSignalChan(make(chan struct{})),
					WithEnableInterfacesAndPeers(),
				)
			} else {
				controller, err = NewController(
					WithServiceabilityProgramClient(m),
					WithListener(listener),
					WithSignalChan(make(chan struct{})),
				)
			}
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

			want, err := os.ReadFile(test.Want)
			if err != nil {
				t.Fatalf("error reading test fixture %s: %v", test.Want, err)
			}

			got, err := agent.GetConfig(ctx, test.AgentRequest)
			if err != nil {
				t.Fatalf("error while fetching config: %v", err)
			}
			if diff := cmp.Diff(string(want), got.Config); diff != "" {
				t.Errorf("Config mismatch (-want +got): %s\n", diff)
			}
		})
	}
}
