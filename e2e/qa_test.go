//go:build qa

package e2e

import (
	"context"
	"flag"
	"math/rand"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

var (
	hosts = flag.String("hosts", "", "comma separated list of hosts to run tests against")
	port  = flag.String("port", "7009", "port to connect to on each host")
)

func TestConnectivityUnicast(t *testing.T) {
	flag.Parse()

	if *hosts == "" {
		t.Skip("No hosts provided, skipping unicast connectivity test")
	}
	hosts := strings.Split(*hosts, ",")
	cleanup := unicastCleanupFunc(t, hosts)
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
			opts = append(opts, dzsdk.WithServiceabilityProgramID(config.DevnetServiceabilityProgramID))

			ledger, err := dzsdk.New(nil, config.DevnetLedgerPublicRPCURL, opts...)
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

func TestConnectivityMulticast(t *testing.T) {
	flag.Parse()

	if *hosts == "" {
		t.Skip("No hosts provided, skipping multicast connectivity test")
	}
	hosts := strings.Split(*hosts, ",")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pick a random host to be the publisher.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	publisherIndex := r.Intn(len(hosts))
	publisher := hosts[publisherIndex]
	code := "qa-test-group"

	cleanup := multicastCleanupFunc(t, hosts, publisher, code)
	defer cleanup()

	// Remove the publisher from the slice; the rest are subscribers.
	subscribers := make([]string, 0, len(hosts)-1)
	subscribers = append(subscribers, hosts[:publisherIndex]...)
	subscribers = append(subscribers, hosts[publisherIndex+1:]...)
	require.Greater(t, len(subscribers), 0, "Not enough hosts to test multicast, need at least one subscriber")

	t.Logf("Using publisher: %s, subscribers: %v, hosts: %v", publisher, subscribers, hosts)
	t.Run("create_multicast_group", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		client, err := getQAClient(net.JoinHostPort(publisher, *port))
		require.NoError(t, err, "Failed to create QA client")

		req := &pb.CreateMulticastGroupRequest{
			Code:         code,
			MaxBandwidth: "1Gbps",
		}
		result, err := client.CreateMulticastGroup(ctx, req)
		require.NoError(t, err, "CreateMulticastGroup failed")
		if result.GetSuccess() == false || result.GetReturnCode() != 0 {
			require.Fail(t, "CreateMulticastGroup failed", result.GetOutput())
		}
	})

	// get pubkey of created multicast group for deletion later
	pubKey := ""
	ownerPubKey := ""
	var groupAddr net.IP
	var status serviceability.MulticastGroupStatus
	condition := func() (bool, error) {
		group, ok, err := findMulticastGroupByCode(ctx, code)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		pubKey = base58.Encode(group.PubKey[:])
		ownerPubKey = base58.Encode(group.Owner[:])
		groupAddr = net.IP(group.MulticastIp[:])
		status = group.Status

		if pubKey == "" || groupAddr == nil || ownerPubKey == "" || status != serviceability.MulticastGroupStatusActivated {
			return false, nil
		}
		return true, nil
	}

	err := poll.Until(ctx, condition, 30*time.Second, 1*time.Second)
	require.NoError(t, err, "Failed to get pubkey for multicast group")
	t.Logf("Multicast group created with pubkey: %s address: %s owner: %s status: %d", pubKey, groupAddr, ownerPubKey, status)

	t.Run("update_multicast_allow_list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		client, err := getQAClient(net.JoinHostPort(publisher, *port))
		require.NoError(t, err, "Failed to create QA client")

		req := &pb.MulticastAllowListAddRequest{
			Mode:   pb.MulticastAllowListAddRequest_PUBLISHER,
			Code:   code,
			Pubkey: ownerPubKey,
		}
		result, err := client.MulticastAllowListAdd(ctx, req)
		require.NoError(t, err, "MulticastAllowListAdd failed")
		if result.GetSuccess() == false || result.GetReturnCode() != 0 {
			require.Fail(t, "MulticastAllowListAdd failed", result.GetOutput())
		}
		t.Logf("Multicast group %s added to allow list for publisher %s", code, ownerPubKey)

		req = &pb.MulticastAllowListAddRequest{
			Mode:   pb.MulticastAllowListAddRequest_SUBSCRIBER,
			Code:   code,
			Pubkey: ownerPubKey,
		}
		result, err = client.MulticastAllowListAdd(ctx, req)
		require.NoError(t, err, "MulticastAllowListAdd failed")
		if result.GetSuccess() == false || result.GetReturnCode() != 0 {
			require.Fail(t, "MulticastAllowListAdd failed", result.GetOutput())
		}
		t.Logf("Multicast group %s added to allow list for subscriber %s", code, ownerPubKey)
	})

	t.Run("connect_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("subscribe_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				client, err := getQAClient(net.JoinHostPort(host, *port))
				require.NoError(t, err, "Failed to create QA client")
				req := &pb.ConnectMulticastRequest{
					Mode: pb.ConnectMulticastRequest_SUBSCRIBER,
					Code: code,
				}
				result, err := client.ConnectMulticast(ctx, req)
				require.NoError(t, err, "ConnectMulticast failed")
				if result.GetSuccess() == false || result.GetReturnCode() != 0 {
					require.Fail(t, "ConnectMulticast failed", result.GetOutput())
				}

				_, err = client.MulticastJoin(ctx, &pb.MulticastJoinRequest{
					Groups: []*pb.MulticastGroup{
						{
							Group: groupAddr.String(),
							Port:  7000,
							Iface: "doublezero1",
						},
					},
				})
				require.NoError(t, err, "MulticastJoin failed")
			})
		}
	})

	t.Run("connect_multicast_publisher_"+publisher, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		client, err := getQAClient(net.JoinHostPort(publisher, *port))
		require.NoError(t, err, "Failed to create QA client")
		req := &pb.ConnectMulticastRequest{
			Mode: pb.ConnectMulticastRequest_PUBLISHER,
			Code: code,
		}
		result, err := client.ConnectMulticast(ctx, req)
		require.NoError(t, err, "ConnectMulticast failed")
		if result.GetSuccess() == false || result.GetReturnCode() != 0 {
			require.Fail(t, "ConnectMulticast failed", result.GetOutput())
		}
		sendReq := &pb.MulticastSendRequest{
			Group:    groupAddr.String(),
			Port:     7000,
			Duration: 30,
		}
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err = client.MulticastSend(ctx, sendReq)
		require.NoError(t, err, "MulticastSend failed")
	})

	t.Run("check_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("check_subscriber_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				client, err := getQAClient(net.JoinHostPort(host, *port))
				require.NoError(t, err, "Failed to create QA client")
				resp, err := client.MulticastReport(ctx, &pb.MulticastReportRequest{
					Groups: []*pb.MulticastGroup{
						{
							Group: groupAddr.String(),
							Port:  7000,
						},
					},
				})
				require.NoError(t, err, "MulticastReport failed")
				t.Logf("Multicast report for group %s on subscriber %s: %+v", groupAddr.String(), host, resp)
				require.Greater(t, resp.Reports[groupAddr.String()].PacketCount, uint64(0), "No packets received for group %s on subscriber %s", groupAddr.String(), host)
			})
		}
	})

	t.Run("stop_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("stop_subscriber_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				client, err := getQAClient(net.JoinHostPort(host, *port))
				require.NoError(t, err, "Failed to create QA client")
				_, err = client.MulticastLeave(ctx, &emptypb.Empty{})
				require.NoError(t, err, "MulticastLeave failed")
			})
		}
	})
}

func getQAClient(addr string) (pb.QAAgentServiceClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return pb.NewQAAgentServiceClient(conn), nil
}

func unicastCleanupFunc(t *testing.T, hosts []string) func() {
	return func() {
		disconnectUsers(t, hosts) // Disconnect all users after tests
	}
}

func multicastCleanupFunc(t *testing.T, hosts []string, publisher, code string) func() {
	return func() {
		disconnectUsers(t, hosts)
		removeMulticastGroup(t, code, publisher)
	}
}

func disconnectUsers(t *testing.T, hosts []string) {
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

func removeMulticastGroup(t *testing.T, code, publisher string) {
	t.Run("delete_multicast_group", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		group, ok, err := findMulticastGroupByCode(ctx, code)
		require.NoError(t, err, "Failed to find multicast group by code")
		if !ok {
			require.Fail(t, "Multicast group not found")
		}
		client, err := getQAClient(net.JoinHostPort(publisher, *port))
		require.NoError(t, err, "Failed to create QA client")

		pubKey := base58.Encode(group.PubKey[:])
		req := &pb.DeleteMulticastGroupRequest{
			Pubkey: pubKey,
		}
		result, err := client.DeleteMulticastGroup(ctx, req)
		require.NoError(t, err, "DeleteMulticastGroup failed")
		if result.GetSuccess() == false || result.GetReturnCode() != 0 {
			require.Fail(t, "DeleteMulticastGroup failed", result.GetOutput())
		}
	})
}

func findMulticastGroupByCode(ctx context.Context, code string) (group serviceability.MulticastGroup, ok bool, err error) {
	opts := []dzsdk.Option{}
	opts = append(opts, dzsdk.WithServiceabilityProgramID(config.DevnetServiceabilityProgramID))

	ledger, err := dzsdk.New(nil, config.DevnetLedgerPublicRPCURL, opts...)
	if err != nil {
		return serviceability.MulticastGroup{}, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	data, err := ledger.Serviceability.GetProgramData(ctx)
	if err != nil {
		return serviceability.MulticastGroup{}, false, err
	}
	for _, group := range data.MulticastGroups {
		if group.Code == code {
			return group, true, nil
		}
	}
	return serviceability.MulticastGroup{}, false, nil
}
