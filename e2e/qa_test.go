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
	"sort"
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
	env            = flag.String("env", "", "environment to run in (devnet, testnet, mainnet-beta)")
	forcePublisher = flag.String("force-publisher", "", "host to force as publisher for multicast tests (optional)")
	useGroup       = flag.String("use-group", "", "use existing multicast group by code (optional)")

	serviceabilityClient *serviceability.Client

	clients      map[string]pb.QAAgentServiceClient
	clientsMutex sync.RWMutex

	hostList []string
	devices  []*Device
)

type Device struct {
	PubKey     string
	Code       string
	MaxUsers   int
	UsersCount int
}

type DeviceTestResult struct {
	Device  *Device
	Success bool
	Error   string
}

func TestMain(m *testing.M) {
	flag.Parse()
	switch *env {
	case "devnet", "testnet", "mainnet-beta":
	case "":
		log.Fatal("The -env flag is required. Must be one of: devnet, testnet, mainnet-beta")
	default:
		log.Fatalf("Invalid value for -env flag: %q. Must be one of: devnet, testnet, mainnet-beta", *env)
	}

	networkConfig, err := config.NetworkConfigForEnv(*env)
	if err != nil {
		log.Fatalf("failed to get network config for env %s: %v", *env, err)
	}
	serviceabilityClient = serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	// Get all devices from on-chain data
	ctx := context.Background()
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		log.Fatalf("failed to get program data: %v", err)
	}

	for _, d := range data.Devices {
		dev := &Device{
			PubKey:     base58.Encode(d.PubKey[:]),
			Code:       d.Code,
			MaxUsers:   int(d.MaxUsers),
			UsersCount: int(d.UsersCount),
		}
		devices = append(devices, dev)
	}

	fmt.Printf("Found %d devices on-chain\n", len(devices))
	for _, dev := range devices {
		fmt.Printf("Device PubKey: %s, Code: %s, MaxUsers: %d, UsersCount: %d\n", dev.PubKey, dev.Code, dev.MaxUsers, dev.UsersCount)
	}

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
			Duration: 60,
		}
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
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

// TestConnectivityUnicast_AllDevices tests connectivity across all devices on-chain
// Skip this test with -short flag as it can take a long time
func TestConnectivityUnicast_AllDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-devices test in short mode")
	}

	if len(devices) == 0 {
		t.Skip("No devices found on-chain")
	}

	// Ensure we have exactly 2 hosts
	if len(hostList) != 2 {
		t.Fatal("Exactly 2 hosts are required for all-devices connectivity testing")
	}

	// Filter devices to only include those with sufficient capacity and skip test devices
	var validDevices []*Device
	for _, device := range devices {
		// Skip devices with "test" in the code as these are typically not real hardware
		if strings.Contains(strings.ToLower(device.Code), "test") {
			t.Logf("Skipping test device %s", device.Code)
			continue
		}

		// Check if device has capacity for at least 2 users
		if device.MaxUsers > 0 && device.UsersCount >= device.MaxUsers-1 {
			t.Logf("Skipping device %s as it doesn't have capacity for 2 users (%d/%d users)",
				device.Code, device.UsersCount, device.MaxUsers)
			continue
		}
		validDevices = append(validDevices, device)
	}

	if len(validDevices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
	}

	// Sort devices by code for consistent ordering
	sort.Slice(validDevices, func(i, j int) bool {
		return validDevices[i].Code < validDevices[j].Code
	})

	t.Logf("Will test devices in order: %v", func() []string {
		codes := make([]string, len(validDevices))
		for i, d := range validDevices {
			codes[i] = d.Code
		}
		return codes
	}())

	// Connect first host to a valid device and keep it connected for the entire test
	firstHost := hostList[0]
	secondHost := hostList[1]

	ctx := context.Background()

	// Try to connect first host to devices until we find one that works
	var client1 pb.QAAgentServiceClient
	var firstHostIP string

	for i, device := range validDevices {
		// Add a delay between attempts to avoid overwhelming the QA agent
		if i > 0 {
			t.Logf("Waiting 5 seconds before next connection attempt...")
			time.Sleep(5 * time.Second)
		}

		t.Logf("Attempting to connect %s to device %s", firstHost, device.Code)

		var err error
		client1, err = getQAClient(firstHost)
		if err != nil {
			t.Logf("Failed to create QA client for first host: %v", err)
			continue
		}

		req1 := &pb.ConnectUnicastRequest{
			Mode:       pb.ConnectUnicastRequest_IBRL,
			DeviceCode: device.Code,
		}

		// Use a shorter timeout for initial connection attempts (20s to allow real devices to connect)
		connCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		result1, err := client1.ConnectUnicast(connCtx, req1)
		cancel()
		if err != nil {
			t.Logf("Failed to connect to device %s: %v", device.Code, err)
			// Try to disconnect to clean up any partial state
			t.Logf("Attempting disconnect to clean up state...")
			disconnectOnError(client1)
			continue
		}

		if !result1.GetSuccess() {
			t.Logf("Connection to device %s failed: %s", device.Code, result1.GetOutput())
			// Try to disconnect to clean up any partial state
			t.Logf("Attempting disconnect to clean up state...")
			disconnectOnError(client1)
			continue
		}

		// Get the IP address of the first host
		statusCtx, statusCancel := context.WithTimeout(ctx, 5*time.Second)
		resp1, err := client1.GetStatus(statusCtx, &emptypb.Empty{})
		statusCancel()
		if err != nil {
			t.Logf("Failed to get status after connecting to device %s: %v", device.Code, err)
			// Disconnect and try next device
			disconnectOnError(client1)
			continue
		}

		firstHostIP = getIPFromStatus(resp1)
		if firstHostIP == "" {
			t.Logf("Failed to get IP for device %s", device.Code)
			// Disconnect and try next device
			disconnectOnError(client1)
			continue
		}

		// Success! We found a working device
		t.Logf("First host %s successfully connected to device %s with IP %s", firstHost, device.Code, firstHostIP)
		break
	}

	require.NotEmpty(t, firstHostIP, "Failed to connect first host to any device")
	require.NotNil(t, client1, "Failed to establish client connection")

	// Ensure we disconnect first host at the end
	defer func() {
		t.Logf("Disconnecting first host %s", firstHost)
		_, _ = client1.Disconnect(context.Background(), &emptypb.Empty{})
	}()

	// Track results for all devices
	results := make([]*DeviceTestResult, 0, len(validDevices))
	var resultsMutex sync.Mutex

	// Now loop through all valid devices, connecting second host to each
	for _, device := range validDevices {
		device := device // capture loop variable
		t.Run(fmt.Sprintf("device_%s", device.Code), func(t *testing.T) {
			result := &DeviceTestResult{
				Device:  device,
				Success: true,
				Error:   "",
			}

			defer func() {
				resultsMutex.Lock()
				results = append(results, result)
				resultsMutex.Unlock()
			}()

			// Run the connectivity test and capture any errors
			err := testSecondHostConnectionToDevice(t, device, firstHost, firstHostIP, secondHost)
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				t.Errorf("Device %s failed: %v", device.Code, err)
			}
		})
	}

	// Print summary
	printTestSummary(t, results)
}

// testSecondHostConnectionToDevice connects the second host to a specific device and tests bidirectional connectivity with the first host
func testSecondHostConnectionToDevice(t *testing.T, device *Device, firstHost string, firstHostIP string, secondHost string) error {
	// Connect second host to the device
	t.Logf("Connecting %s to device %s", secondHost, device.Code)
	client2, err := getQAClient(secondHost)
	if err != nil {
		return fmt.Errorf("failed to create QA client for second host: %w", err)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer connectCancel()

	req2 := &pb.ConnectUnicastRequest{
		Mode:       pb.ConnectUnicastRequest_IBRL,
		DeviceCode: device.Code,
	}
	result2, err := client2.ConnectUnicast(connectCtx, req2)
	if err != nil {
		return fmt.Errorf("failed to connect second host: %w", err)
	}
	if !result2.GetSuccess() {
		return fmt.Errorf("second host connection failed: %s", result2.GetOutput())
	}

	// Get the IP address of the second host
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer statusCancel()

	resp2, err := client2.GetStatus(statusCtx, &emptypb.Empty{})
	if err != nil {
		// Disconnect on error
		disconnectOnError(client2)
		return fmt.Errorf("failed to get status for second host: %w", err)
	}

	secondHostIP := getIPFromStatus(resp2)
	if secondHostIP == "" {
		// Disconnect on error
		disconnectOnError(client2)
		return fmt.Errorf("failed to get IP for second host on device %s", device.Code)
	}
	t.Logf("Second host %s connected to device %s with IP %s", secondHost, device.Code, secondHostIP)

	// Run the connectivity tests
	testErr := runPingTests(t, client2, device, firstHost, firstHostIP, secondHost, secondHostIP)

	// Always disconnect the second host after tests complete
	t.Logf("Disconnecting second host %s from device %s", secondHost, device.Code)
	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	disconnectResult, disconnectErr := client2.Disconnect(disconnectCtx, &emptypb.Empty{})
	disconnectCancel()

	if disconnectErr != nil {
		t.Logf("Warning: disconnect failed for second host from device %s: %v", device.Code, disconnectErr)
	} else if disconnectResult != nil && disconnectResult.GetSuccess() {
		t.Logf("Successfully disconnected from device %s", device.Code)
	}

	// Wait a bit to ensure clean disconnect before next connection
	time.Sleep(2 * time.Second)

	return testErr
}

// disconnectOnError is a helper to disconnect a client when an error occurs
func disconnectOnError(client pb.QAAgentServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = client.Disconnect(ctx, &emptypb.Empty{})
}

// runPingTests performs the bidirectional ping tests
func runPingTests(t *testing.T, client2 pb.QAAgentServiceClient, device *Device, firstHost string, firstHostIP string, secondHost string, secondHostIP string) error {
	// Add a small delay to ensure routing is established
	t.Logf("Waiting 3 seconds for routing to stabilize on device %s", device.Code)
	time.Sleep(3 * time.Second)

	// Test connectivity from second host to first host with retries
	t.Logf("Testing ping from second host (%s) to first host (%s) on device %s", secondHostIP, firstHostIP, device.Code)

	var lastErr error
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			t.Logf("Retry attempt %d/%d after 2 second delay", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

		pingReq := &pb.PingRequest{
			TargetIp:    firstHostIP,
			SourceIp:    secondHostIP,
			SourceIface: "doublezero0",
			PingType:    pb.PingRequest_ICMP,
		}

		t.Logf("Attempt %d: Sending ping request: target=%s, source=%s", attempt, firstHostIP, secondHostIP)
		pingResp, err := client2.Ping(pingCtx, pingReq)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("attempt %d: ping from second host to first host failed: %w", attempt, err)
			t.Logf("Attempt %d failed: %v", attempt, err)
			continue
		}

		if pingResp.PacketsSent == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets sent from second host", attempt)
			t.Logf("Attempt %d failed: no packets sent", attempt)
			continue
		}
		if pingResp.PacketsReceived == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets received by second host (sent=%d)", attempt, pingResp.PacketsSent)
			t.Logf("Attempt %d failed: no packets received (sent=%d)", attempt, pingResp.PacketsSent)
			continue
		}
		if pingResp.PacketsReceived < pingResp.PacketsSent {
			lastErr = fmt.Errorf("attempt %d: packet loss detected: sent=%d, received=%d", attempt, pingResp.PacketsSent, pingResp.PacketsReceived)
			t.Logf("Attempt %d failed: packet loss (sent=%d, received=%d)", attempt, pingResp.PacketsSent, pingResp.PacketsReceived)
			continue
		}

		t.Logf("Successfully pinged first host (%s) from second host (%s) on device %s (attempt %d)",
			firstHostIP, secondHostIP, device.Code, attempt)
		lastErr = nil
		break
	}

	if lastErr != nil {
		return lastErr
	}

	// Test connectivity from first host to second host with retries
	t.Logf("Testing ping from first host (%s) to second host (%s) on device %s", firstHostIP, secondHostIP, device.Code)
	client1, err := getQAClient(firstHost)
	if err != nil {
		return fmt.Errorf("failed to create QA client for first host: %w", err)
	}

	lastErr = nil
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			t.Logf("Retry attempt %d/%d after 2 second delay", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		pingCtx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)

		pingReq2 := &pb.PingRequest{
			TargetIp:    secondHostIP,
			SourceIp:    firstHostIP,
			SourceIface: "doublezero0",
			PingType:    pb.PingRequest_ICMP,
		}

		t.Logf("Attempt %d: Sending ping request: target=%s, source=%s", attempt, secondHostIP, firstHostIP)
		pingResp2, err := client1.Ping(pingCtx2, pingReq2)
		cancel2()

		if err != nil {
			lastErr = fmt.Errorf("attempt %d: ping from first host to second host failed: %w", attempt, err)
			t.Logf("Attempt %d failed: %v", attempt, err)
			continue
		}

		if pingResp2.PacketsSent == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets sent from first host", attempt)
			t.Logf("Attempt %d failed: no packets sent", attempt)
			continue
		}
		if pingResp2.PacketsReceived == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets received by first host (sent=%d)", attempt, pingResp2.PacketsSent)
			t.Logf("Attempt %d failed: no packets received (sent=%d)", attempt, pingResp2.PacketsSent)
			continue
		}
		if pingResp2.PacketsReceived < pingResp2.PacketsSent {
			lastErr = fmt.Errorf("attempt %d: packet loss detected: sent=%d, received=%d", attempt, pingResp2.PacketsSent, pingResp2.PacketsReceived)
			t.Logf("Attempt %d failed: packet loss (sent=%d, received=%d)", attempt, pingResp2.PacketsSent, pingResp2.PacketsReceived)
			continue
		}

		t.Logf("Successfully pinged second host (%s) from first host (%s) on device %s (attempt %d)",
			secondHostIP, firstHostIP, device.Code, attempt)
		lastErr = nil
		break
	}

	return lastErr
}

// getIPFromStatus extracts the DoubleZero IP from a status response
func getIPFromStatus(resp *pb.StatusResponse) string {
	for _, status := range resp.Status {
		if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
			return status.DoubleZeroIp
		}
	}
	return ""
}

// printTestSummary prints a summary of test results
func printTestSummary(t *testing.T, results []*DeviceTestResult) {
	var passed, failed []*DeviceTestResult

	for _, result := range results {
		if result.Success {
			passed = append(passed, result)
		} else {
			failed = append(failed, result)
		}
	}

	t.Logf("\n========================================")
	t.Logf("TEST SUMMARY")
	t.Logf("========================================")
	t.Logf("Total devices tested: %d", len(results))
	t.Logf("Passed: %d", len(passed))
	t.Logf("Failed: %d", len(failed))

	if len(passed) > 0 {
		t.Logf("\n✅ PASSED DEVICES:")
		for _, result := range passed {
			t.Logf("  - %s", result.Device.Code)
		}
	}

	if len(failed) > 0 {
		t.Logf("\n❌ FAILED DEVICES:")
		for _, result := range failed {
			t.Logf("  - %s: %s", result.Device.Code, result.Error)
		}
	}

	t.Logf("========================================\n")

	// Fail the test if any device failed
	if len(failed) > 0 {
		t.Errorf("Connectivity test failed for %d out of %d devices", len(failed), len(results))
	}
}
