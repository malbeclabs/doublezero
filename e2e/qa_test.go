//go:build qa

package e2e

import (
	"context"
	"flag"
	"log"
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
)

var (
	hosts = flag.String("hosts", "chi-dn-bm2,chi-dn-bm3", "comma separated list of hosts to run tests against")
	port  = flag.String("port", "7009", "port to connect to on each host")
)

func TestConnectivityUnicast(t *testing.T) {
	flag.Parse()

	hosts := strings.Split(*hosts, ",")
	cleanup := disconnectUsers(t, hosts)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, host := range hosts {
		t.Run("connect_ibrl_mode_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			client, err := getQAClient(net.JoinHostPort(host, *port))
			if err != nil {
				t.Fatalf("Failed to create QA client: %v", err)
			}
			// TODO: pick random host to use IBRL w/ allocated address mode
			req := &pb.ConnectUnicastRequest{
				Mode: pb.ConnectUnicastRequest_IBRL,
			}
			result, err := client.ConnectUnicast(ctx, req)
			if err != nil {
				log.Fatalf("ConnectUnicast failed: %v", err)
			}
			if result.GetSuccess() == false || result.GetReturnCode() != 0 {
				log.Fatalf("ConnectUnicast failed: %v", result.GetOutput())
			}
		})
	}

	for _, host := range hosts {
		t.Run("connectivity_check_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			client, err := getQAClient(net.JoinHostPort(host, *port))
			if err != nil {
				t.Fatalf("Failed to create QA client: %v", err)
			}
			resp, err := client.GetStatus(ctx, &emptypb.Empty{})
			if err != nil {
				t.Fatalf("GetStatus failed: %v", err)
			}

			localAddr := ""
			for _, status := range resp.Status {
				if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
					localAddr = status.DoubleZeroIp
				}
			}
			if localAddr == "" {
				t.Fatalf("No local address found in status response")
			}
			opts := []dzsdk.Option{}
			opts = append(opts, dzsdk.WithServiceabilityProgramID(serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET))

			ledger, err := dzsdk.New(nil, dzsdk.DZ_LEDGER_RPC_URL, opts...)
			if err != nil {
				t.Fatalf("Failed to create ledger client: %v", err)
			}
			data, err := ledger.Serviceability.GetProgramData(ctx)
			if err != nil {
				t.Fatalf("Failed to get program data: %v", err)
			}

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
				t.Fatalf("No peers found for connectivity check")
			}

			for _, peer := range peers {
				t.Run("to_"+peer, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					pingReq := &pb.PingRequest{
						TargetIp:    peer,
						SourceIp:    localAddr,
						SourceIface: "doublezero0",
					}
					pingResp, err := client.Ping(ctx, pingReq)
					if err != nil {
						t.Fatalf("Ping failed for %s: %v", peer, err)
					}

					if pingResp.PacketsSent == 0 || pingResp.PacketsReceived == 0 {
						t.Fatalf("Ping to %s failed: Sent=%d, Received=%d", peer, pingResp.PacketsSent, pingResp.PacketsReceived)
					}

					if pingResp.PacketsReceived < pingResp.PacketsSent {
						t.Fatalf("Ping to %s had loss: Sent=%d, Received=%d", peer, pingResp.PacketsSent, pingResp.PacketsReceived)
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

func disconnectUsers(t *testing.T, hosts []string) func() {
	return func() {
		for _, host := range hosts {
			t.Run("disconnect_from_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				client, err := getQAClient(net.JoinHostPort(host, *port))
				if err != nil {
					t.Errorf("Failed to create QA client: %v", err)
					return
				}
				_, err = client.Disconnect(ctx, &emptypb.Empty{})
				if err != nil {
					t.Errorf("Disconnect failed: %v", err)
				}
			})
		}
	}
}
