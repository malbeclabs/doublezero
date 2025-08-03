package controller

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/google/go-cmp/cmp"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

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
						PublicIP:        net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/unicast.tunnel.txt",
		},
		{
			Name:        "render_multicast_config_successfully",
			Description: "render configuration for a set of multicast devices successfully",
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
						PublicIP:        net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/multicast.tunnel.txt",
		},
		{
			Name:        "get_config_mixed_tunnels_successfully",
			Description: "get config for a mix of unicast and multicast tunnels",
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
						PublicIP:        net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/mixed.tunnel.txt",
		},
		{
			Name:        "get_config_nohardware_tunnels_successfully",
			Description: "get config for a mix of unicast and multicast tunnels with no hardware option",
			NoHardware:  true,
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
						PublicIP:        net.IP{7, 7, 7, 7},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
					},
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/nohardware.tunnel.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			controller := &Controller{
				noHardware: test.NoHardware,
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

type mockAccountFetcher struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockAccountFetcher) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
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
				Vpnv4BgpPeers: []Vpnv4BgpPeer{
					{
						PeerIP:    net.IP{14, 14, 14, 14},
						PeerName:  "abc01",
						SourceInt: "Loopback255",
					},
				},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:          "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP:        net.IP{2, 2, 2, 2},
						Vpn4vLoopbackIP: net.IP{14, 14, 14, 14},
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
						},
						TunnelSlots: 64,
						Interfaces: []serviceability.Interface{
							{
								InterfaceType: serviceability.InterfaceTypeLoopback,
								LoopbackType:  serviceability.LoopbackTypeVpnv4,
								IpNet:         [5]uint8{14, 14, 14, 14, 32},
								Name:          "Loopback255",
							},
						},
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
				Vpnv4BgpPeers:   nil, // No BGP peers since device is excluded
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

			m := &mockAccountFetcher{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						Config:          test.Config,
						Users:           test.Users,
						Devices:         test.Devices,
						MulticastGroups: test.MulticastGroups,
					}, nil
				},
			}
			controller, err := NewController(WithAccountFetcher(m), WithListener(lis))
			if err != nil {
				t.Fatalf("error creating controller: %v", err)
			}
			if err := controller.updateStateCache(context.Background()); err != nil {
				t.Fatalf("error populating state cache: %v", err)
			}
			if diff := cmp.Diff(test.StateCache, controller.cache); diff != "" {
				t.Errorf("StateCache mismatch (-want +got): %s\n", diff)
			}
		})
	}
}

func TestAccountFetcherArgs(t *testing.T) {
	tests := []struct {
		name            string
		programId       string
		rpcEndpoint     string
		wantProgramId   string
		wantRpcEndpoint string
	}{
		{
			name:            "verify_default_program_id_and_rpc_url_are_set",
			programId:       "",
			rpcEndpoint:     "",
			wantProgramId:   serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET,
			wantRpcEndpoint: dzsdk.DZ_LEDGER_RPC_URL,
		},
		{
			name:            "verify_custom_program_id_and_rpc_url_are_set",
			programId:       "mycustomprogramidthatneeds32charssohere1234",
			rpcEndpoint:     "https://custom-rpc-url.com",
			wantProgramId:   "mycustomprogramidthatneeds32charssohere1234",
			wantRpcEndpoint: "https://custom-rpc-url.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := []Option{
				WithListener(bufconn.Listen(1024 * 1024)),
			}

			if test.rpcEndpoint != "" {
				opts = append(opts, WithRpcEndpoint(test.rpcEndpoint))
			}
			if test.programId != "" {
				opts = append(opts, WithProgramId(test.programId))
			}
			controller, err := NewController(opts...)
			if err != nil {
				t.Fatalf("error creating controller: %v", err)
			}

			if controller.programId != test.wantProgramId {
				t.Errorf("expected program ID %s, got %s", test.wantProgramId, controller.programId)
			}
			if controller.rpcEndpoint != test.wantRpcEndpoint {
				t.Errorf("expected RPC URL %s, got %s", test.wantRpcEndpoint, controller.rpcEndpoint)
			}
		})
	}
}

// TestEndToEnd verifies on-chain data can be fetched, the local state cache updated, and a config
// can be rendered and sent back to the client via gRPC.
func TestEndToEnd(t *testing.T) {
	tests := []struct {
		Name            string
		Config          serviceability.Config
		Users           []serviceability.User
		Devices         []serviceability.Device
		MulticastGroups []serviceability.MulticastGroup
		AgentRequest    *pb.ConfigRequest
		DevicePubKey    string
		Want            string
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
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{14, 14, 14, 14, 32},
							Name:          "Loopback255",
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
			Want: "fixtures/e2e.txt",
		},
		{
			Name: "remove_unknown_peers_successfully",
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
							InterfaceType: serviceability.InterfaceTypeLoopback,
							LoopbackType:  serviceability.LoopbackTypeVpnv4,
							IpNet:         [5]uint8{14, 14, 14, 14, 32},
							Name:          "Loopback255",
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
					"169.254.0.7",
				},
			},
			Want: "fixtures/e2e.peer.removal.txt",
		},
		{
			Name: "remove_last_user_from_device",
			Config: serviceability.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Users: []serviceability.User{},
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
			m := &mockAccountFetcher{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						Config:          test.Config,
						Users:           test.Users,
						Devices:         test.Devices,
						MulticastGroups: test.MulticastGroups,
					}, nil
				},
			}

			controller, err := NewController(
				WithAccountFetcher(m),
				WithListener(listener),
				WithSignalChan(make(chan struct{})),
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
