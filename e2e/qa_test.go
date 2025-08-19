//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

var (
	hosts          = flag.String("hosts", "", "comma separated list of hosts to run tests against")
	port           = flag.String("port", "7009", "port to connect to on each host")
	env            = flag.String("env", "", "environment to run in (devnet, testnet, mainnet)")
	forcePublisher = flag.String("force-publisher", "", "host to force as publisher for multicast tests (optional)")
	useGroup       = flag.String("use-group", "", "use existing multicast group by code (optional)")

	serviceabilityClient *serviceability.Client

	clients      map[string]pb.QAAgentServiceClient
	clientsMutex sync.RWMutex

	hostList []string
)

func TestMain(m *testing.M) {
	flag.Parse()
	switch *env {
	case "devnet", "testnet", "mainnet":
	case "":
		log.Fatal("The -env flag is required. Must be one of: devnet, testnet, mainnet")
	default:
		log.Fatalf("Invalid value for -env flag: %q. Must be one of: devnet, testnet, mainnet", *env)
	}

	networkConfig, err := config.NetworkConfigForEnv(*env)
	if err != nil {
		log.Fatalf("failed to get network config for env %s: %v", *env, err)
	}
	serviceabilityClient = serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	clients = make(map[string]pb.QAAgentServiceClient)
	clientConns := make(map[string]*grpc.ClientConn)

	if *hosts != "" {
		hostList = strings.Split(*hosts, ",")
		if len(hostList) < 2 {
			log.Fatal("At least two hosts are required to run the tests")
		}
		for _, host := range hostList {
			addr := net.JoinHostPort(host, *port)
			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Fatalf("Failed to create gRPC client connection for host %s: %v", host, err)
			}
			clientConns[host] = conn
			clients[host] = pb.NewQAAgentServiceClient(conn)
		}
	}

	exitCode := m.Run()

	for host, conn := range clientConns {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close gRPC connection for host %s: %v", host, err)
		}
	}

	os.Exit(exitCode)
}

func getQAClient(host string) (pb.QAAgentServiceClient, error) {
	clientsMutex.RLock()
	defer clientsMutex.RUnlock()

	client, ok := clients[host]
	if !ok {
		return nil, fmt.Errorf("no client found for host: %s. Ensure it is included in the --hosts flag", host)
	}
	return client, nil
}

func TestConnectivityUnicast(t *testing.T) {
	cleanup := unicastCleanupFunc(t, hostList)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, host := range hostList {
		if !t.Run("connect_ibrl_mode_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			client, err := getQAClient(host)
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
		}) {
			t.Fatalf("Failed to connect IBRL mode from host %s", host)
		}
	}

	// Build host-to-IP map after all hosts are connected
	hostToIP := make(map[string]string)
	for _, host := range hostList {
		client, err := getQAClient(host)
		require.NoError(t, err, "Failed to create QA client for host %s", host)

		resp, err := client.GetStatus(ctx, &emptypb.Empty{})
		require.NoError(t, err, "GetStatus failed for host %s", host)

		for _, status := range resp.Status {
			if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
				hostToIP[host] = status.DoubleZeroIp
				break
			}
		}
		require.NotEmpty(t, hostToIP[host], "No local address found for host %s", host)
	}

	// Run connectivity checks only between hosts participating in the test
	for _, host := range hostList {
		t.Run("connectivity_check_from_"+host, func(t *testing.T) {
			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			localAddr := hostToIP[host]

			// Only ping IPs of hosts in hostList (excluding self)
			peers := []string{}
			for peerHost, peerIP := range hostToIP {
				if peerHost == host {
					continue
				}
				peers = append(peers, peerIP)
			}

			require.NotEmpty(t, peers, "No peers found for connectivity check")

			for _, peer := range peers {
				t.Run("to_"+peer, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
					defer cancel()
					pingReq := &pb.PingRequest{
						TargetIp:    peer,
						SourceIp:    localAddr,
						SourceIface: "doublezero0",
						PingType:    pb.PingRequest_ICMP,
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
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	code := *useGroup
	if code == "" {
		suffix := r.Intn(1000000)
		code = fmt.Sprintf("qa-test-group-%06d", suffix)
		t.Logf("No multicast group code specified, using generated code: %s", code)
	}

	var publisher string
	if *forcePublisher != "" {
		publisher = *forcePublisher
	} else {
		// Pick a random host to be the publisher.
		publisherIndex := r.Intn(len(hostList))
		publisher = hostList[publisherIndex]
	}

	var publisherIndex = -1
	for i, h := range hostList {
		if h == publisher {
			publisherIndex = i
			break
		}
	}
	// Make the selected publisher is actually in the list of hosts.
	if publisherIndex == -1 {
		t.Fatalf("Forced publisher %s is not in the host list: %v", publisher, hostList)
	}

	// Remove the publisher from the slice; the rest are subscribers.
	subscribers := make([]string, 0, len(hostList)-1)
	subscribers = append(subscribers, hostList[:publisherIndex]...)
	subscribers = append(subscribers, hostList[publisherIndex+1:]...)
	require.Greater(t, len(subscribers), 0, "Not enough hosts to test multicast, need at least one subscriber")

	cleanup := func() {
		disconnectUsers(t, hostList)
		// If using an existing group, we don't want to clean it up
		if *useGroup == "" {
			removeMulticastGroup(t, code, publisher)
		}
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Logf("Using publisher: %s, subscribers: %v, hosts: %v", publisher, subscribers, hostList)
	if !t.Run("create_multicast_group", func(t *testing.T) {
		if *useGroup != "" {
			t.Skipf("Using existing multicast group: %s", *useGroup)
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		client, err := getQAClient(publisher)
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
	}) {
		t.Fatalf("Failed to create multicast group with publisher %s", publisher)
	}

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

	err := poll.Until(ctx, condition, 60*time.Second, 1*time.Second)
	require.NoError(t, err, "Failed to get pubkey for multicast group")
	t.Logf("Multicast group created with pubkey: %s address: %s owner: %s status: %d", pubKey, groupAddr, ownerPubKey, status)

	if !t.Run("update_multicast_allow_list", func(t *testing.T) {
		if *useGroup != "" {
			t.Skipf("Using existing multicast group: %s", *useGroup)
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		client, err := getQAClient(publisher)
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
	}) {
		t.Fatal("Failed to update multicast allow list")
	}

	if !t.Run("connect_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("subscribe_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				client, err := getQAClient(host)
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
	}) {
		t.Fatalf("Failed to connect multicast subscribers to group %s", code)
	}

	if !t.Run("connect_multicast_publisher_"+publisher, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		client, err := getQAClient(publisher)
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
	}) {
		t.Fatalf("Failed to connect multicast publisher %s to group %s", publisher, code)
	}

	t.Run("check_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("check_subscriber_"+host, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
				defer cancel()
				client, err := getQAClient(host)
				require.NoError(t, err, "Failed to create QA client")

				condition := func() (bool, error) {
					resp, err := client.MulticastReport(ctx, &pb.MulticastReportRequest{
						Groups: []*pb.MulticastGroup{
							{
								Group: groupAddr.String(),
								Port:  7000,
							},
						},
					})
					if err != nil {
						return false, err
					}
					if len(resp.Reports) == 0 {
						return false, nil
					}
					if _, ok := resp.Reports[groupAddr.String()]; !ok {
						return false, fmt.Errorf("group %s not found in reports on subscriber %s", groupAddr.String(), host)
					}
					if resp.Reports[groupAddr.String()].PacketCount == 0 {
						return false, nil
					}
					t.Logf("Subscriber %s received %d packets for group %s", host, resp.Reports[groupAddr.String()].PacketCount, groupAddr.String())
					return true, nil
				}
				start := time.Now()
				t.Logf("Waiting for packets on subscriber %s for group %s", host, groupAddr.String())
				err = poll.Until(ctx, condition, 60*time.Second, 1*time.Second)
				t.Logf("Waited %s for packets on subscriber %s for group %s", time.Since(start), host, groupAddr.String())
				require.NoError(t, err, "error: %v", fmt.Errorf("No packets received for group %s on subscriber %s", groupAddr.String(), host))

			})
		}
	})

	t.Run("stop_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("stop_subscriber_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				client, err := getQAClient(host)
				require.NoError(t, err, "Failed to create QA client")
				_, err = client.MulticastLeave(ctx, &emptypb.Empty{})
				require.NoError(t, err, "MulticastLeave failed")
			})
		}
	})
}

func unicastCleanupFunc(t *testing.T, hosts []string) func() {
	return func() {
		disconnectUsers(t, hosts) // Disconnect all users after tests
	}
}

func disconnectUsers(t *testing.T, hosts []string) {
	for _, host := range hosts {
		t.Run("disconnect_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			_, err = client.Disconnect(ctx, &emptypb.Empty{})
			require.NoError(t, err, "Disconnect failed")
		})
	}
}

func removeMulticastGroup(t *testing.T, code, publisher string) {
	t.Run("delete_multicast_group", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		group, ok, err := findMulticastGroupByCode(ctx, code)
		require.NoError(t, err, "Failed to find multicast group by code")
		if !ok {
			require.Fail(t, "Multicast group not found")
		}
		client, err := getQAClient(publisher)
		require.NoError(t, err, "Failed to create QA client")

		pubKey := base58.Encode(group.PubKey[:])
		t.Logf("Deleting multicast group %s with pubkey %s", code, pubKey)
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	data, err := serviceabilityClient.GetProgramData(ctx)
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
