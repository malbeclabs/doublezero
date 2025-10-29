//go:build linux

package rpc

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const bufSize = 1024 * 1024

func newTestQAAgent(t *testing.T, logger *slog.Logger, opts ...Option) (*QAAgent, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	agent, err := NewQAAgent(logger, "", opts...)
	require.NoError(t, err)
	agent.listener = lis
	return agent, lis
}

func TestQAAgentConnectivity(t *testing.T) {
	if _, err := exec.LookPath("doublezero"); err != nil {
		t.Skip("skipping test: doublezero binary not found in PATH")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create a mock HTTP server to simulate the doublezerod unix socket API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			_, _ = w.Write([]byte(`[{"tunnel_name":"dz-1","doublezero_ip":"100.64.0.1","user_type":"ibrl","doublezero_status":{"session_status":"up"}}]`))
		}
	}))
	defer mockServer.Close()

	opts := []Option{
		WithDZClient(mockServer.Client()),
		WithDZStatusURL(mockServer.URL + "/status"),
		WithJoiner(&DummyJoiner{}),
		WithNetlinker(&DummyNetlinker{}),
	}
	agent, lis := newTestQAAgent(t, logger, opts...)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = agent.Start(ctx)
	}()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewQAAgentServiceClient(conn)

	t.Run("Ping", func(t *testing.T) {
		pingResult, err := client.Ping(ctx, &pb.PingRequest{
			TargetIp: "127.0.0.1",
			PingType: pb.PingRequest_ICMP,
		})
		require.NoError(t, err)
		require.NotNil(t, pingResult)
	})

	t.Run("GetStatus", func(t *testing.T) {
		statusResult, err := client.GetStatus(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, statusResult)
		require.Equal(t, "up", statusResult.GetStatus()[0].GetSessionStatus())
	})

	t.Run("GetPublicIP", func(t *testing.T) {
		resp, err := client.GetPublicIP(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, "6.6.6.6", resp.GetPublicIp())
	})
}

type DummyJoiner struct{}

func (d *DummyJoiner) JoinGroup(ctx context.Context, group net.IP, port string, ifName string) error {
	return nil
}
func (d *DummyJoiner) Stop()                       {}
func (d *DummyJoiner) GetStatistics(net.IP) uint64 { return 0 }

type DummyNetlinker struct{}

func (d *DummyNetlinker) RouteGet(dest net.IP) ([]Route, error) {
	return []Route{
		{
			Dst: &net.IPNet{
				IP:   net.ParseIP("0.0.0.0"),
				Mask: net.CIDRMask(0, 0),
			},
			Src: net.ParseIP("6.6.6.6"),
			Gw:  net.ParseIP("10.1.1.1"),
		},
	}, nil
}
