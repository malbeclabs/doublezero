package agent

import (
	"context"
	"log"
	"net"
	"reflect"
	"testing"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type mockControllerClient struct {
	pb.UnimplementedControllerServer
	LastReceivedRequest *pb.ConfigRequest
}

func (m *mockControllerClient) GetConfig(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
	m.LastReceivedRequest = req
	return &pb.ConfigResponse{
		Config: "Here is your config",
	}, nil
}

func TestGetConfigFromServer_GetConfigRequestValidation(t *testing.T) {
	mockServer := &mockControllerClient{}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterControllerServer(server, mockServer)

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewControllerClient(conn)

	// neighborIpMap := map[string][]string{"vrf1": {"1.2.3.4"}}
	// timeout := 2.0
	// localDevicePubkey := "fakepubkey111111111111111111"

	tests := []struct {
		Name                  string
		ExpectError           bool
		LocalDevicePubkey     string
		NeighborIpMap         map[string][]string
		Timeout               float64
		ExpectedBgpPeers      []string
		ExpectedBgpPeersByVrf map[string]*pb.BgpPeers
	}{
		{
			Name:              "success",
			ExpectError:       false,
			LocalDevicePubkey: "fakepubkey111111111111111111",
			NeighborIpMap: map[string][]string{
				"default": {"172.16.0.1", "172.16.0.2"},
				"vrf1":    {"10.0.0.1", "10.0.0.2"},
				"vrf2":    {"10.0.0.1"},
			},
			Timeout:          2.0,
			ExpectedBgpPeers: []string{"10.0.0.1", "10.0.0.1", "10.0.0.2", "172.16.0.1", "172.16.0.2"},
			ExpectedBgpPeersByVrf: map[string]*pb.BgpPeers{
				"default": {Peers: []string{"172.16.0.1", "172.16.0.2"}},
				"vrf1":    {Peers: []string{"10.0.0.1", "10.0.0.2"}},
				"vrf2":    {Peers: []string{"10.0.0.1"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err = GetConfigFromServer(context.Background(), client, test.LocalDevicePubkey, test.NeighborIpMap, &test.Timeout, "test-version")
			if err != nil {
				t.Fatalf("GetConfigFromServer failed: %v", err)
			}

			// Verify the request sent to GetConfig
			receivedReq := mockServer.LastReceivedRequest
			if receivedReq == nil {
				t.Fatal("Expected request to be sent, but got nil")
			}

			if receivedReq.Pubkey != test.LocalDevicePubkey {
				t.Errorf("Expected Pubkey %q, got %q", test.LocalDevicePubkey, receivedReq.Pubkey)
			}

			if !reflect.DeepEqual(receivedReq.BgpPeers, test.ExpectedBgpPeers) {
				t.Errorf("Expected BgpPeers %v, got %v", test.ExpectedBgpPeers, receivedReq.BgpPeers)
			}

			if !reflect.DeepEqual(receivedReq.BgpPeersByVrf, test.ExpectedBgpPeersByVrf) {
				t.Errorf("Expected BgpPeersByVrf %v, got %v", test.ExpectedBgpPeersByVrf, receivedReq.BgpPeersByVrf)
			}
		})
	}
}

func TestGetDzClient(t *testing.T) {
	tests := []struct {
		Name                     string
		ExpectError              bool
		Ctx                      context.Context
		ControllerAddressAndPort string
	}{
		{
			Name:                     "success",
			ExpectError:              false,
			Ctx:                      context.Background(),
			ControllerAddressAndPort: "127.0.0.1:9543",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err := GetDzClient(test.ControllerAddressAndPort)
			if err != nil {
				t.Errorf("Call to GetDzClient failed with error %q", err)
			} else {
			}
		})
	}
}
