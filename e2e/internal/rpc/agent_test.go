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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create a mock HTTP server to simulate the doublezerod unix socket API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			_, _ = w.Write([]byte(`[{"tunnel_name":"dz-1","doublezero_ip":"100.64.0.1","user_type":"ibrl","doublezero_status":{"session_status":"BGP Session Up"}}]`))
		}
		if r.URL.Path == "/latency" {
			_, _ = w.Write([]byte(`[{"device_pk":"8PQkip3CxWhQTdP7doCyhT2kwjSL2csRTdnRg2zbDPs1","device_code":"chi-dn-dzd1","device_ip":"100.0.0.1","min_latency_ns":24989983,"max_latency_ns":25115111,"avg_latency_ns":25063568,"reachable":true}]`))
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

	t.Run("Traceroute", func(t *testing.T) {
		if !hasMTRBinary() {
			t.Skip("skipping test: mtr binary not found")
		}

		res, err := client.Traceroute(ctx, &pb.TracerouteRequest{
			TargetIp:    "127.0.0.1",
			SourceIp:    "127.0.0.1",
			SourceIface: "lo",
			Timeout:     1,
			Count:       1,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Len(t, res.Hops, 1)
		require.Equal(t, "127.0.0.1", res.TargetIp)
		require.Equal(t, "127.0.0.1", res.SourceIp)
		require.Equal(t, uint32(1), res.Timeout)
		require.Equal(t, uint32(1), res.Tests)
	})

	t.Run("TracerouteRaw", func(t *testing.T) {
		if !hasMTRBinary() {
			t.Skip("skipping test: mtr binary not found")
		}

		res, err := client.TracerouteRaw(ctx, &pb.TracerouteRequest{
			TargetIp:    "127.0.0.1",
			SourceIp:    "127.0.0.1",
			SourceIface: "lo",
			Timeout:     1,
			Count:       1,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		require.True(t, res.Success)
		require.Equal(t, int32(0), res.ReturnCode)
		require.NotEmpty(t, res.Output)
	})

	t.Run("GetStatus", func(t *testing.T) {
		if _, err := exec.LookPath("doublezero"); err != nil {
			t.Skip("skipping test: doublezero binary not found")
		}

		statusResult, err := client.GetStatus(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, statusResult)
		require.Equal(t, "BGP Session Up", statusResult.GetStatus()[0].GetSessionStatus())
	})

	t.Run("GetLatency", func(t *testing.T) {
		if _, err := exec.LookPath("doublezero"); err != nil {
			t.Skip("skipping test: doublezero binary not found")
		}

		latencyResult, err := client.GetLatency(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, latencyResult)
		require.NotEmpty(t, latencyResult.GetLatencies())

		require.NotEmpty(t, latencyResult.GetLatencies()[0].GetDevicePk())
		require.Equal(t, latencyResult.GetLatencies()[0].GetDeviceIp(), "100.0.0.1")
		require.Equal(t, latencyResult.GetLatencies()[0].GetDeviceCode(), "chi-dn-dzd1")

		require.Greater(t, latencyResult.GetLatencies()[0].GetMinLatencyNs(), uint64(24989983))
		require.Greater(t, latencyResult.GetLatencies()[0].GetAvgLatencyNs(), uint64(25063568))
		require.Greater(t, latencyResult.GetLatencies()[0].GetMaxLatencyNs(), uint64(25115111))
	})

	t.Run("GetPublicIP", func(t *testing.T) {
		resp, err := client.GetPublicIP(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, "6.6.6.6", resp.GetPublicIp())
	})

	t.Run("GetRoutes", func(t *testing.T) {
		resp, err := client.GetRoutes(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, 1, len(resp.GetInstalledRoutes()))
		require.Equal(t, "0.0.0.0", resp.GetInstalledRoutes()[0].GetDstIp())
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

func (d *DummyNetlinker) RouteByProtocol(protocol int) ([]Route, error) {
	return []Route{
		{
			Dst: &net.IPNet{
				IP:   net.ParseIP("0.0.0.0"),
				Mask: net.CIDRMask(0, 0),
			},
		},
	}, nil
}
