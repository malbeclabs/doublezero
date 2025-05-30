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
				Config: dzsdk.Config{
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
						PublicIP: net.IP{7, 7, 7, 7},
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
				Config: dzsdk.Config{
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
								MulticastSubscribers: []net.IP{
									{239, 0, 0, 1},
									{239, 0, 0, 2},
								},
								MulticastPublishers: []net.IP{},
							},
							{
								Id:                   501,
								UnderlaySrcIP:        net.IP{3, 3, 3, 3},
								UnderlayDstIP:        net.IP{4, 4, 4, 4},
								OverlaySrcIP:         net.IP{169, 254, 0, 2},
								OverlayDstIP:         net.IP{169, 254, 0, 3},
								DzIp:                 net.IP{100, 0, 0, 1},
								Allocated:            true,
								IsMulticast:          true,
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
						PublicIP: net.IP{7, 7, 7, 7},
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
				Config: dzsdk.Config{
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
								Id:                   503,
								UnderlaySrcIP:        net.IP{7, 7, 7, 7},
								UnderlayDstIP:        net.IP{8, 8, 8, 8},
								OverlaySrcIP:         net.IP{169, 254, 0, 6},
								OverlayDstIP:         net.IP{169, 254, 0, 7},
								DzIp:                 net.IP{100, 0, 0, 3},
								Allocated:            true,
								IsMulticast:          true,
								MulticastSubscribers: []net.IP{},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
						},
						PublicIP: net.IP{7, 7, 7, 7},
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
				Config: dzsdk.Config{
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
								Id:                   503,
								UnderlaySrcIP:        net.IP{7, 7, 7, 7},
								UnderlayDstIP:        net.IP{8, 8, 8, 8},
								OverlaySrcIP:         net.IP{169, 254, 0, 6},
								OverlayDstIP:         net.IP{169, 254, 0, 7},
								DzIp:                 net.IP{100, 0, 0, 3},
								Allocated:            true,
								IsMulticast:          true,
								MulticastSubscribers: []net.IP{},
								MulticastPublishers: []net.IP{
									{239, 0, 0, 5},
									{239, 0, 0, 6},
								},
							},
						},
						PublicIP: net.IP{7, 7, 7, 7},
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
	Users           []dzsdk.User
	Devices         []dzsdk.Device
	MulticastGroups []dzsdk.MulticastGroup
	Config          dzsdk.Config
}

func (m *mockAccountFetcher) Load(context.Context) error {
	return nil
}

func (m *mockAccountFetcher) GetDevices() []dzsdk.Device {
	return m.Devices
}

func (m *mockAccountFetcher) GetUsers() []dzsdk.User {
	return m.Users
}

func (m *mockAccountFetcher) GetMulticastGroups() []dzsdk.MulticastGroup {
	return m.MulticastGroups
}

func (m *mockAccountFetcher) GetConfig() dzsdk.Config {
	return m.Config
}

func TestStateCache(t *testing.T) {
	tests := []struct {
		Name            string
		Description     string
		Config          dzsdk.Config
		Users           []dzsdk.User
		Devices         []dzsdk.Device
		MulticastGroups []dzsdk.MulticastGroup
		StateCache      stateCache
	}{
		{
			Name: "populate_device_cache_successfully",
			Config: dzsdk.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []dzsdk.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
				},
			},
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       dzsdk.UserStatusActivated,
				},
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{100, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{10, 1, 1, 2, 31},
					Status:       dzsdk.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []dzsdk.Device{
				{
					AccountType:    dzsdk.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         dzsdk.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
				},
			},
			StateCache: stateCache{
				Config: dzsdk.Config{
					MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
				},
				MulticastGroups: map[string]dzsdk.MulticastGroup{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:      [32]uint8{1},
						MulticastIp: [4]uint8{239, 0, 0, 1},
						Subscribers: [][32]uint8{
							{1},
							{2},
						},
					},
				},
				Devices: map[string]*Device{
					"4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM": {
						PubKey:   "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
						PublicIP: net.IP{2, 2, 2, 2},
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
						},
						TunnelSlots: 20,
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			lis, err := net.Listen("tcp", net.JoinHostPort("localhost", "7004"))
			if err != nil {
				log.Fatalf("failed to listen: %v", err)
			}

			m := &mockAccountFetcher{
				Config:          test.Config,
				Users:           test.Users,
				Devices:         test.Devices,
				MulticastGroups: test.MulticastGroups,
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

// TestEndToEnd verifies on-chain data can be fetched, the local state cache updated, and a config
// can be rendered and sent back to the client via gRPC.
func TestEndToEnd(t *testing.T) {
	tests := []struct {
		Name            string
		Config          dzsdk.Config
		Users           []dzsdk.User
		Devices         []dzsdk.Device
		MulticastGroups []dzsdk.MulticastGroup
		AgentRequest    *pb.ConfigRequest
		DevicePubKey    string
		Want            string
	}{
		{
			Name: "fetch_accounts_and_render_config_successfully",
			Config: dzsdk.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []dzsdk.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
				},
			},
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       dzsdk.UserStatusActivated,
				},
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{100, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{169, 254, 0, 2, 31},
					Status:       dzsdk.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []dzsdk.Device{
				{
					AccountType:    dzsdk.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         dzsdk.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
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
			Config: dzsdk.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			MulticastGroups: []dzsdk.MulticastGroup{
				{
					PubKey:      [32]uint8{1},
					MulticastIp: [4]uint8{239, 0, 0, 1},
					Subscribers: [][32]uint8{
						{1},
						{2},
					},
				},
			},
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeIBRL),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       dzsdk.UserStatusActivated,
				},
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeMulticast),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{3, 3, 3, 3},
					DzIp:         [4]uint8{100, 100, 100, 101},
					TunnelId:     uint16(501),
					TunnelNet:    [5]uint8{169, 254, 0, 2, 31},
					Status:       dzsdk.UserStatusActivated,
					Subscribers:  [][32]uint8{{1}},
				},
			},
			Devices: []dzsdk.Device{
				{
					AccountType:    dzsdk.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         dzsdk.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
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
			Config: dzsdk.Config{
				MulticastGroupBlock: [5]uint8{239, 0, 0, 0, 24},
			},
			Users: []dzsdk.User{},
			Devices: []dzsdk.Device{
				{
					AccountType:    dzsdk.AccountType(0),
					Owner:          [32]uint8{},
					LocationPubKey: [32]uint8{},
					ExchangePubKey: [32]uint8{},
					DeviceType:     0,
					PublicIp:       [4]uint8{2, 2, 2, 2},
					Status:         dzsdk.DeviceStatusActivated,
					Code:           "abc01",
					PubKey:         [32]byte{1},
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
				Users:           test.Users,
				Devices:         test.Devices,
				MulticastGroups: test.MulticastGroups,
				Config:          test.Config,
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
