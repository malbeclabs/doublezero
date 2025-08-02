//go:build qa

package e2e

import (
	"context"
	"flag"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

var (
	hosts = flag.String("hosts", "chi-dn-bm2,chi-dn-bm3", "comma separated list of hosts to run tests against")
	port  = flag.String("port", "7009", "port to connect to on each host")
)

func TestConnectivityUnicast(t *testing.T) {
	flag.Parse()

	hosts := strings.Split(*hosts, ",")
	cleanup := disconnectUsersFunc(t, hosts)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, host := range hosts {
		t.Run("connect_ibrl_mode_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			client, err := getQAClient(net.JoinHostPort(host, *port))
			require.NoError(t, err, "Failed to create QA client")

			// TODO: pick random host to use IBRL w/ allocated address mode
			req := &pb.ConnectUnicastRequest{
				Mode: pb.ConnectUnicastRequest_IBRL,
			}
			result, err := client.ConnectUnicast(ctx, req)
			require.NoError(t, err, "ConnectUnicast failed")

			if result.GetSuccess() == false || result.GetReturnCode() != 0 {
				require.Fail(t, "ConnectUnicast failed", result.GetOutput())
			}
		})
	}

	for _, host := range hosts {
		t.Run("connectivity_check_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			client, err := getQAClient(net.JoinHostPort(host, *port))
			require.NoError(t, err, "Failed to create QA client")

			resp, err := client.GetStatus(ctx, &emptypb.Empty{})
			require.NoError(t, err, "GetStatus failed")

			localAddr := ""
			for _, status := range resp.Status {
				if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
					localAddr = status.DoubleZeroIp
				}
			}
			if localAddr == "" {
				require.Fail(t, "No local address found in status response")
			}
			opts := []dzsdk.Option{}
			opts = append(opts, dzsdk.WithServiceabilityProgramID(serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET))

			ledger, err := dzsdk.New(nil, dzsdk.DZ_LEDGER_RPC_URL, opts...)
			require.NoError(t, err, "Failed to create ledger client")
			data, err := ledger.Serviceability.GetProgramData(ctx)
			require.NoError(t, err, "Failed to get program data")

			peers := []string{}
			for _, user := range data.Users {
				if user.UserType == serviceability.UserTypeIBRL || user.UserType == serviceability.UserTypeIBRLWithAllocatedIP {
					// skip ourselves
					if net.IP(user.DzIp[:]).String() == localAddr {
						continue
					}
					peers = append(peers, net.IP(user.DzIp[:]).String())
				}
			}

			if len(peers) == 0 {
				require.Fail(t, "No peers found for connectivity check")
			}

			for _, peer := range peers {
				t.Run("to_"+peer, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
					defer cancel()
					pingReq := &pb.PingRequest{
						TargetIp:    peer,
						SourceIp:    localAddr,
						SourceIface: "doublezero0",
					}
					pingResp, err := client.Ping(ctx, pingReq)
					require.NoError(t, err, "Ping failed for %s", peer)

					if pingResp.PacketsSent == 0 || pingResp.PacketsReceived == 0 {
						require.Fail(t, "Ping to %s failed: Sent=%d, Received=%d", peer, pingResp.PacketsSent, pingResp.PacketsReceived)
					}

					if pingResp.PacketsReceived < pingResp.PacketsSent {
						require.Fail(t, "Ping to %s had loss: Sent=%d, Received=%d", peer, pingResp.PacketsSent, pingResp.PacketsReceived)
					}
				})
			}
		})
	}
}

func getQAClient(addr string) (pb.QAAgentServiceClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return pb.NewQAAgentServiceClient(conn), nil
}

func disconnectUsersFunc(t *testing.T, hosts []string) func() {
	return func() {
		for _, host := range hosts {
			t.Run("disconnect_from_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				client, err := getQAClient(net.JoinHostPort(host, *port))
				require.NoError(t, err, "Failed to create QA client")

				_, err = client.Disconnect(ctx, &emptypb.Empty{})
				require.NoError(t, err, "Disconnect failed")
			})
		}
	}
}
