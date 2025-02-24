package agent

import (
	"context"
	"log"
	"net"
	"testing"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type mockControllerClient struct {
	pb.UnimplementedControllerServer
}

func (*mockControllerClient) GetConfig(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
	return &pb.ConfigResponse{
		Config: "Here is your config",
	}, nil
}

// Create a mock gRPC dialer with mockAristaEapiMgr attached so we can simulate gRPC calls
func newDzControllerMockDialer() func(context.Context, string) (net.Conn, error) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()

	pb.RegisterControllerServer(server, &mockControllerClient{})

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()
	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}

// Create a mock gRPC client using the mock dialer.
func newDzControllerMockClientConn() (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(newDzControllerMockDialer()),
	}
	conn, err := grpc.NewClient("passthrough://bufnet", opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func TestGetConfigFromServer(t *testing.T) {
	mockDzControllerClientConn, err := newDzControllerMockClientConn()
	if err != nil {
		log.Printf("Call to newDzMockClientConn failed with error: %q", err)
		return
	}
	defer mockDzControllerClientConn.Close()

	mockDzControllerClient := pb.NewControllerClient(mockDzControllerClientConn)

	tests := []struct {
		Name              string
		ExpectError       bool
		Ctx               context.Context
		ControllerClient  pb.ControllerClient
		LocalDevicePubkey string
		NeighborIpList    []string
		Timeout           float64
	}{
		{
			Name:              "success",
			ExpectError:       false,
			Ctx:               context.Background(),
			ControllerClient:  mockDzControllerClient,
			LocalDevicePubkey: "fakepubkey111111111111111111",
			NeighborIpList:    []string{"1.2.3.4"},
			Timeout:           2.0,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err := GetConfigFromServer(test.Ctx, test.ControllerClient, test.LocalDevicePubkey, test.NeighborIpList, &test.Timeout)
			if err != nil {
				t.Errorf("Call to GetConfigFromServer failed with error %q", err)
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
