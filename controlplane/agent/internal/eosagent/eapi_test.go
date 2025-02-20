package eosagent

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type mockAristaEapiMgr struct {
	pb.UnimplementedEapiMgrServiceServer
}

func (*mockAristaEapiMgr) RunConfigCmds(ctx context.Context, req *pb.RunConfigCmdsRequest) (*pb.RunConfigCmdsResponse, error) {
	return &pb.RunConfigCmdsResponse{
		Response: &pb.EapiResponse{
			Success:   true,
			Responses: []string{string("Bro")},
		},
	}, nil
}

func (*mockAristaEapiMgr) RunShowCmd(ctx context.Context, req *pb.RunShowCmdRequest) (*pb.RunShowCmdResponse, error) {
	var resp []string
	switch req.Command {
	case "show ip bgp neighbors":
		resp = []string{string("{ \"vrfs\": { \"default\": { \"peerList\": [ { \"peerAddress\": \"192.168.1.1\", \"asn\": \"65432\", \"linkType\": \"internal\", \"routerId\": \"0.0.0.0\" }, { \"peerAddress\": \"192.168.1.1\", \"asn\": \"65432\", \"linkType\": \"internal\", \"routerId\": \"0.0.0.0\" } ] } } }")}
	case "show configuration sessions":
		resp = []string{string("{\"sessions\": {\"doublezero-eosagent-123456789000\": {\"state\": \"pending\", \"completedTime\": 1736543591.7917519, \"commitUser\": \"\", \"description\": \"\", \"instances\": {\"868\": {\"user\": \"root\", \"terminal\": \"vty5\", \"currentTerminal\": false}}}, \"blah1\": {\"state\": \"pending\", \"commitUser\": \"\", \"description\": \"\", \"instances\": {}}}, \"maxSavedSessions\": 1, \"maxOpenSessions\": 5, \"mergeOnCommit\": false, \"saveToStartupConfigOnCommit\": false}")}
	case "show configuration lock":
		resp = []string{string("{ \"userInfo\": { \"username\": \"root\", \"userTty\": \"vty4\", \"transactionName\": \"doublezero\", \"lockAcquireTime\": 5000.0 } }")}
	case "configure session doublezero-eosagent-123456789000 abort":
		resp = []string{string("{ }")}
	default:
		return nil, fmt.Errorf("Unknown RunShowCmd request \"%s\"", req)
	}
	return &pb.RunShowCmdResponse{
		Response: &pb.EapiResponse{
			Success:   true,
			Responses: resp,
		},
	}, nil
}

// Create a mock gRPC dialer with mockAristaEapiMgr attached so we can simulate gRPC calls
func newMockDialer() func(context.Context, string) (net.Conn, error) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()

	pb.RegisterEapiMgrServiceServer(server, &mockAristaEapiMgr{})

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
func newMockClientConn() (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(newMockDialer()),
	}
	conn, err := grpc.NewClient("passthrough://bufnet", opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func TestGetEosConfigurationSessionDistinguisher(t *testing.T) {
	now := time.Now().Unix()
	got, err := strconv.Atoi(getEosConfigurationSessionDistinguisher())
	if err != nil {
		t.Fatal("GetEosConfigurationSessionDistinguisher: strconv.Atoi failed with error: ", err)
	}
	if int64(got) < now {
		t.Fatalf("GetEosConfigurationSessionDistinguisher didn't return a number (%d) that was >= the current timestamp (%d)", got, now)
	}
}

func TestCheckConfigChanges(t *testing.T) {
	tests := []struct {
		Name        string
		ExpectError bool
		diffCmd     *exec.Cmd
	}{
		{
			Name:        "diff_found",
			ExpectError: false,
			diffCmd:     exec.Command("echo", fmt.Sprintf("If this was not a test I'd run \"show session-config named XXXXX diffs\"")),
		},
		{
			Name:        "no_diff_found",
			ExpectError: false,
			diffCmd:     exec.Command("true"),
		},
		{
			Name:        "bad_exec_command",
			ExpectError: true,
			diffCmd:     exec.Command("bad_command"),
		},
	}

	mockClientConn, err := newMockClientConn()
	if err != nil {
		log.Printf("Call to newMockClientConn failed with error: %q", err)
		return
	}
	defer mockClientConn.Close()

	eapiClient, err := NewEapiClient("127.0.0.1:9543", mockClientConn)
	if err != nil {
		t.Fatalf("Call to NewEapiClient failed with error %q", err)
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, _ = eapiClient.CheckConfigChanges("session-X", test.diffCmd)
		})
	}
}

func TestNewEapiClient(t *testing.T) {
	_, err := NewEapiClient("127.0.0.1:9543", nil)
	if err != nil {
		t.Errorf("Call to NewEapiClient failed with error %q", err)
		return
	}
}

func TestAddConfigToDevice(t *testing.T) {
	mockClientConn, err := newMockClientConn()

	if err != nil {
		log.Printf("Call to newMockClientConn failed with error: %q", err)
		return
	}
	defer mockClientConn.Close()

	tests := []struct {
		Name        string
		ExpectError bool
		Ctx         context.Context
		Device      string
		ClientConn  *grpc.ClientConn
		Config      string
	}{
		{
			Name:        "success",
			ExpectError: false,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543",
			ClientConn:  mockClientConn,
			Config:      "blah",
		},
		{
			Name:        "connection_failure",
			ExpectError: true,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543",
			// When ClientConn is set to nil, we will create a new connection.
			// This will not fail as expected if you have an EOS instance running on 127.0.0.1:9543
			ClientConn: nil,
			Config:     "This should blow up",
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			eapiClient, err := NewEapiClient(test.Device, test.ClientConn)
			if err != nil {
				t.Fatalf("Call to NewEapiClient failed with error %q", err)
			}
			var configSlice []string
			diffCmd := exec.Command("echo", fmt.Sprintf("If this was not a test I'd run \"show session-config named XXXXX diffs\""))

			configSlice, err = eapiClient.AddConfigToDevice(test.Ctx, test.Config, diffCmd, 600)

			if !test.ExpectError && err != nil {
				t.Fatalf("error: %v", err)
			}
			if test.ExpectError && err == nil {
				t.Fatalf("wanted error but returned nil")
			}

			// Using HasPrefix/HasSuffix instead of checking the whole string because the session name includes a random distinguisher
			if !test.ExpectError && !strings.HasPrefix(configSlice[0], "configure session doublezero-eosagent-") {
				t.Fatal("Call to eapiClient.AddConfigToDevice did not prepend config session, instead got:", configSlice[0])
			}

			// if !test.Error && !strings.HasSuffix(configSlice[len(configSlice)-1], "commit") {
			// 	t.Fatalf("Call to eapiClient.AddConfigToDevice did not append config session commit")
			// }
		})
	}
}

func TestGetBgpNeighbors(t *testing.T) {
	mockClientConn, err := newMockClientConn()

	if err != nil {
		log.Fatalf("Call to newMockClientConn failed with error: %q", err)
		return
	}
	defer mockClientConn.Close()

	tests := []struct {
		Name        string
		ExpectError bool
		Ctx         context.Context
		Device      string
		ClientConn  *grpc.ClientConn
		Want        []string
	}{
		{
			Name:        "success",
			ExpectError: false,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543",
			ClientConn:  mockClientConn,
			Want:        []string{"192.168.1.1"},
		},
		{
			Name:        "connection_failure",
			ExpectError: true,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543j",
			// When ClientConn is set to nil, we will create a new connection.
			// This will not fail as expected if you have an EOS instance running on 127.0.0.1:9543
			ClientConn: nil,
			Want:       nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			eapiClient, err := NewEapiClient(test.Device, test.ClientConn)

			if err != nil {
				t.Errorf("Call to NewEapiClient failed with error %q", err)
			} else {
				resp, err := eapiClient.GetBgpNeighbors(test.Ctx)
				if test.Want != nil && (test.Want[0] != resp[0]) {
					t.Errorf("Expected peer 0 to be %s but instead got %s", test.Want[0], resp[0])
				}
				if test.ExpectError {
					if err == nil {
						t.Errorf("Should have failed to connect but no error was raised %q", err)
					}
				} else {
					if err != nil {
						t.Errorf("Call to eapiClient.GetBgpNeighbors failed with error: %q", err)
					}
				}
			}
		})
	}
}

func TestClearStaleConfigSessions(t *testing.T) {
	mockClientConn, err := newMockClientConn()

	if err != nil {
		log.Fatalf("Call to newMockClientConn failed with error: %q", err)
		return
	}
	defer mockClientConn.Close()

	tests := []struct {
		Name        string
		ExpectError bool
		Ctx         context.Context
		Device      string
		ClientConn  *grpc.ClientConn
	}{
		{
			Name:        "success",
			ExpectError: false,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543",
			ClientConn:  mockClientConn,
		},
		{
			Name:        "failure",
			ExpectError: true,
			Ctx:         context.Background(),
			Device:      "127.0.0.1:9543",
			ClientConn:  nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			eapiClient, err := NewEapiClient(test.Device, test.ClientConn)
			if err != nil {
				t.Fatalf("NewEapiClient failed with %q", err)
			}

			_ = eapiClient.clearStaleConfigSessions(test.Ctx)
		})
	}
}
