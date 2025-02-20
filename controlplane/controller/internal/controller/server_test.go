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
		DeviceCache map[string]*Device
		Pubkey      string
		Want        string
	}{
		{
			Name:        "render_config_successfully",
			Description: "render configuration for a set of devices successfully",
			DeviceCache: map[string]*Device{
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
				},
			},
			Pubkey: "abc123",
			Want:   "fixtures/tunnel.txt",
		},
	}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	controller := &Controller{}
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

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			// update the device cache in the controller per the test
			controller.swapCache(test.DeviceCache)

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
	Users   []dzsdk.User
	Devices []dzsdk.Device
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

func TestDeviceCache(t *testing.T) {
	tests := []struct {
		Name        string
		Description string
		Users       []dzsdk.User
		Devices     []dzsdk.Device
		DeviceCache deviceCache
	}{
		{
			Name: "populate_device_cache_successfully",
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeServer),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{10, 1, 1, 0, 31},
					Status:       dzsdk.UserStatusActivated,
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
			DeviceCache: map[string]*Device{
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
						{Id: 501},
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
					TunnelSlots:     20,
					UnknownBgpPeers: []net.IP{},
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

			m := &mockAccountFetcher{Users: test.Users, Devices: test.Devices}
			controller, err := NewController(WithAccountFetcher(m), WithListener(lis))
			if err != nil {
				t.Fatalf("error creating controller: %v", err)
			}
			if err := controller.updateDeviceCache(context.Background()); err != nil {
				t.Fatalf("error populating device cache: %v", err)
			}
			if diff := cmp.Diff(test.DeviceCache, controller.cache); diff != "" {
				t.Errorf("DeviceCache mismatch (-want +got): %s\n", diff)
			}
		})
	}
}

// TestEndToEnd verifies on-chain data can be fetched, the local device cache updated, and a config
// can be rendered and sent back to the client via gRPC.
func TestEndToEnd(t *testing.T) {
	tests := []struct {
		Name         string
		Users        []dzsdk.User
		Devices      []dzsdk.Device
		AgentRequest *pb.ConfigRequest
		DevicePubKey string
		Want         string
	}{
		{
			Name: "fetch_accounts_and_render_config_successfully",
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeServer),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       dzsdk.UserStatusActivated,
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
			Users: []dzsdk.User{
				{
					AccountType:  dzsdk.AccountType(0),
					Owner:        [32]uint8{},
					UserType:     dzsdk.UserUserType(dzsdk.UserTypeServer),
					DevicePubKey: [32]uint8{1},
					CyoaType:     dzsdk.CyoaTypeGREOverDIA,
					ClientIp:     [4]uint8{1, 1, 1, 1},
					DzIp:         [4]uint8{100, 100, 100, 100},
					TunnelId:     uint16(500),
					TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
					Status:       dzsdk.UserStatusActivated,
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
			Name:  "remove_last_user_from_device",
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
			m := &mockAccountFetcher{Users: test.Users, Devices: test.Devices}

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
				t.Fatalf("timed out waiting for device cache update")
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
