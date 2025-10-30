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
	PubKey       string
	Code         string
	ExchangeCode string
	MaxUsers     int
	UsersCount   int
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

	// Get all devices from onchain data
	ctx := context.Background()
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		log.Fatalf("failed to get program data: %v", err)
	}

	// Create a map of exchange pubkeys to codes for lookup
	exchangeMap := make(map[[32]uint8]string)
	for _, e := range data.Exchanges {
		exchangeMap[e.PubKey] = e.Code
	}

	for _, d := range data.Devices {
		exchangeCode := exchangeMap[d.ExchangePubKey]
		dev := &Device{
			PubKey:       base58.Encode(d.PubKey[:]),
			Code:         d.Code,
			ExchangeCode: exchangeCode,
			MaxUsers:     int(d.MaxUsers),
			UsersCount:   int(d.UsersCount),
		}
		devices = append(devices, dev)
	}

	fmt.Printf("Found %d devices onchain\n", len(devices))

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

	// Use the unified helper with nil devices (QA mode)
	runUnicastConnectivityTest(t, hostList, nil)
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
		t.Logf("Multicast group %s added to allow list for publisher server: %s user-payer: %s", code, publisher, ownerPubKey)

		for _, subscriber := range subscribers {
			client, err := getQAClient(subscriber)
			require.NoError(t, err, "Failed to create QA client")

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
			t.Logf("Multicast group %s added to allow list for subscriber server: %s user-payer: %s", code, subscriber, ownerPubKey)
		}
	}) {
		t.Fatal("Failed to update multicast allow list")
	}

	if !t.Run("connect_multicast_subscribers", func(t *testing.T) {
		for _, host := range subscribers {
			t.Run("subscribe_"+host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				client, err := getQAClient(host)
				require.NoError(t, err, "Failed to create QA client")

				ensureDisconnected(t, ctx, client, host)

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
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		client, err := getQAClient(publisher)
		require.NoError(t, err, "Failed to create QA client")

		ensureDisconnected(t, ctx, client, publisher)

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

func ensureDisconnected(t *testing.T, ctx context.Context, client pb.QAAgentServiceClient, host string) {
	checkCtx, checkCancel := context.WithTimeout(ctx, 30*time.Second)
	checkStatus, checkErr := client.GetStatus(checkCtx, &emptypb.Empty{})
	checkCancel()

	if checkErr != nil {
		require.Fail(t, "Failed to check existing tunnel status", "host: %s, error: %v", host, checkErr)
	}

	if checkStatus != nil {
		for _, s := range checkStatus.Status {
			if s.SessionStatus != "disconnected" {
				t.Logf("Host %s has existing tunnel (session status: %s), disconnecting first", host, s.SessionStatus)
				disconnectCtx, disconnectCancel := context.WithTimeout(ctx, 90*time.Second)
				_, _ = client.Disconnect(disconnectCtx, &emptypb.Empty{})
				disconnectCancel()

				condition := func() (bool, error) {
					statusCtx, statusCancel := context.WithTimeout(ctx, 10*time.Second)
					defer statusCancel()
					status, err := client.GetStatus(statusCtx, &emptypb.Empty{})
					if err != nil {
						return false, err
					}
					for _, s := range status.Status {
						if s.SessionStatus != "disconnected" {
							return false, nil
						}
					}
					return true, nil
				}

				err := poll.Until(ctx, condition, 30*time.Second, 1*time.Second)
				require.NoError(t, err, "Tunnel did not reach disconnected state after disconnect for host %s", host)
				t.Logf("Host %s tunnel is disconnected, proceeding with connection", host)
				break
			}
		}
	}

	// After ensuring disconnected, also wait for the user to be deleted onchain
	waitForUserDeletion(t, ctx, client, host)
}

func waitForUserDeletion(t *testing.T, ctx context.Context, client pb.QAAgentServiceClient, host string) {
	ipCtx, ipCancel := context.WithTimeout(ctx, 10*time.Second)
	ipResp, err := client.GetPublicIP(ipCtx, &emptypb.Empty{})
	ipCancel()

	if err != nil {
		require.NoError(t, err, "Failed to get public IP for host %s", host)
	}

	clientIP := ipResp.GetPublicIp()
	if clientIP == "" {
		require.NotEmpty(t, clientIP, "Empty public IP for host %s", host)
	}

	condition := func() (bool, error) {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return false, err
		}

		for _, user := range data.Users {
			userClientIP := net.IP(user.ClientIp[:]).String()
			if userClientIP == clientIP {
				t.Logf("User with IP %s exists onchain (status: %s), waiting for activator to complete deletion...", clientIP, user.Status)
				return false, nil
			}
		}

		t.Logf("Waiting for user with IP %s to be deleted onchain", clientIP)
		return true, nil
	}

	err = poll.Until(ctx, condition, 60*time.Second, 2*time.Second)
	if err != nil {
		t.Logf("Warning: Timed out waiting for user deletion for IP %s: %v", clientIP, err)
	}
}

func unicastCleanupFunc(t *testing.T, hosts []string) func() {
	return func() {
		disconnectUsers(t, hosts) // Disconnect all users after tests
	}
}

func disconnectUsers(t *testing.T, hosts []string) {
	for _, host := range hosts {
		t.Run("disconnect_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			_, err = client.Disconnect(ctx, &emptypb.Empty{})
			require.NoError(t, err, "Disconnect failed")
		})
	}
	time.Sleep(30 * time.Second) // TODO: remove this after the race in the client is fixed
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

// Skip this test with -short flag as it can take a long time
func TestConnectivityUnicast_AllDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-devices test in short mode")
	}

	if len(devices) == 0 {
		t.Skip("No devices found onchain")
	}

	// Ensure we have at least 2 hosts
	if len(hostList) < 2 {
		t.Fatal("At least 2 hosts are required for all-devices connectivity testing")
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
		availableSlots := device.MaxUsers - device.UsersCount
		if availableSlots < 2 {
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

	// Cleanup at the end
	defer disconnectUsers(t, hostList)

	// Use the unified helper with specific devices (AllDevices mode)
	runUnicastConnectivityTest(t, hostList, validDevices)
}

func printTestAllDevicesUnicastSummary(t *testing.T, results []*DeviceTestResult) {
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

// runUnicastConnectivityTest is a unified helper for connectivity testing
// If devices is nil/empty, connects all hosts without device specification (QA mode)
// If devices are provided, tests connectivity across all devices (AllDevices mode)
func runUnicastConnectivityTest(t *testing.T, hosts []string, devices []*Device) {
	if len(devices) == 0 {
		// QA mode: Connect all hosts without device specification
		t.Log("Running in QA mode - connecting all hosts without device specification")
		hostIPMap, hostDeviceMap, err := connectHosts(t, hosts, nil)
		require.NoError(t, err, "Failed to connect hosts")

		err = testAllToAllConnectivity(t, hostIPMap, hostDeviceMap, false) // false = use simple ping
		require.NoError(t, err, "Connectivity test failed")
		return
	}

	// QA-AllDevices mode: First host stays connected, iterate through devices
	t.Log("Running in AllDevices mode - testing connectivity across specified devices")
	require.True(t, len(hosts) >= 2, "AllDevices mode requires at least 2 hosts")

	firstHost := hosts[0]
	remainingHosts := hosts[1:]

	// Connect first host to first working device
	var firstHostIP string
	var firstHostDevice *Device

	for i, device := range devices {
		if i > 0 {
			t.Logf("Waiting 30 seconds before next connection attempt...")
			time.Sleep(30 * time.Second)
		}

		t.Logf("Attempting to connect %s to device %s", firstHost, device.Code)
		hostIPMap, _, err := connectHosts(t, []string{firstHost}, device)
		if err != nil {
			t.Logf("Failed to connect to device %s: %v", device.Code, err)
			// Try to disconnect to clean up
			if client, err := getQAClient(firstHost); err == nil {
				disconnectOnError(client)
			}
			continue
		}

		firstHostIP = hostIPMap[firstHost]
		firstHostDevice = device
		t.Logf("First host %s successfully connected to device %s with IP %s",
			firstHost, device.Code, firstHostIP)
		break
	}

	require.NotEmpty(t, firstHostIP, "Failed to connect first host to any device")

	// Ensure we disconnect first host at the end
	defer func() {
		t.Logf("Disconnecting first host %s", firstHost)
		if client, err := getQAClient(firstHost); err == nil {
			disconnectOnError(client)
		}
	}()

	// Test each device with remaining hosts
	var results []*DeviceTestResult
	var resultsMutex sync.Mutex

	for i, device := range devices {
		device := device // capture loop variable
		t.Run(fmt.Sprintf("device_%s", device.Code), func(t *testing.T) {
			t.Logf("Testing device %s %d/%d", device.Code, i+1, len(devices))
			result := testDeviceConnectivity(t, device, remainingHosts, firstHost, firstHostIP, firstHostDevice)

			resultsMutex.Lock()
			results = append(results, result)
			resultsMutex.Unlock()

			if !result.Success {
				t.Errorf("Device %s failed: %s", device.Code, result.Error)
			}
			time.Sleep(30 * time.Second) // wait 30s between devices to avoid race in client diconnect/connect
		})
	}

	// Print summary
	printTestAllDevicesUnicastSummary(t, results)
}

// connectHosts connects the specified hosts, optionally to a specific device
// Returns maps of host->IP and host->Device
func connectHosts(t *testing.T, hosts []string, device *Device) (map[string]string, map[string]*Device, error) {
	hostIPMap := make(map[string]string)
	hostDeviceMap := make(map[string]*Device)
	ctx := context.Background()

	for _, host := range hosts {
		client, err := getQAClient(host)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create QA client for %s: %w", host, err)
		}

		ensureDisconnected(t, ctx, client, host)

		// Create connection request
		req := &pb.ConnectUnicastRequest{
			Mode: pb.ConnectUnicastRequest_IBRL,
		}
		if device != nil {
			req.DeviceCode = device.Code
		}

		// Connect with timeout
		connCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		result, err := client.ConnectUnicast(connCtx, req)
		cancel()

		if err != nil {
			return nil, nil, fmt.Errorf("failed to connect %s: %w", host, err)
		}
		if !result.GetSuccess() {
			return nil, nil, fmt.Errorf("connection failed for %s: %s", host, result.GetOutput())
		}

		// Get IP address
		statusCtx, statusCancel := context.WithTimeout(ctx, 60*time.Second)
		status, err := client.GetStatus(statusCtx, &emptypb.Empty{})
		statusCancel()

		if err != nil {
			return nil, nil, fmt.Errorf("failed to get status for %s: %w", host, err)
		}

		if len(status.Status) == 0 {
			return nil, nil, fmt.Errorf("no status entries returned for %s", host)
		}

		s := status.Status[0]
		if s.DoubleZeroIp == "" {
			return nil, nil, fmt.Errorf("failed to get IP for %s", host)
		}

		hostIPMap[host] = s.DoubleZeroIp

		// If we're connecting to a specific device, store it
		if device != nil {
			hostDeviceMap[host] = device
		} else {
			// In QA mode, we need to find which device we connected to for exchange comparison
			connectedDevice := findDeviceByHostIP(t, s.DoubleZeroIp)
			if connectedDevice != nil {
				hostDeviceMap[host] = connectedDevice
			}
		}

		t.Logf("Host %s connected to device %s with IP %s", host, s.CurrentDevice, s.DoubleZeroIp)
	}

	return hostIPMap, hostDeviceMap, nil
}

// testDeviceConnectivity tests connectivity for a specific device
func testDeviceConnectivity(t *testing.T, device *Device, hosts []string, additionalHost string, additionalIP string, additionalHostDevice *Device) *DeviceTestResult {
	result := &DeviceTestResult{
		Device:  device,
		Success: true,
		Error:   "",
	}

	// Ensure cleanup happens even if connection fails
	defer func() {
		// Disconnect the hosts we just connected (not the additional host)
		for _, host := range hosts {
			t.Logf("Disconnecting %s from device %s", host, device.Code)
			if client, err := getQAClient(host); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				_, _ = client.Disconnect(ctx, &emptypb.Empty{})
				cancel()
			}
		}

		// Wait a bit to ensure clean disconnect before next connection
		time.Sleep(2 * time.Second)
	}()

	// Connect all hosts to this device
	t.Logf("Connecting hosts %s to device %s", hosts, device.Code)
	hostIPMap, hostDeviceMap, err := connectHosts(t, hosts, device)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		return result
	}

	// Add the already-connected first host to the maps
	hostIPMap[additionalHost] = additionalIP
	hostDeviceMap[additionalHost] = additionalHostDevice

	// Test connectivity between all hosts
	err = testAllToAllConnectivity(t, hostIPMap, hostDeviceMap, true) // true = use retry ping
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	}

	return result
}

func testAllToAllConnectivity(t *testing.T, hostIPMap map[string]string, hostDeviceMap map[string]*Device, useRetry bool) error {
	// Build ordered lists for consistent testing
	var sortedHosts []string
	for host := range hostIPMap {
		sortedHosts = append(sortedHosts, host)
	}
	sort.Strings(sortedHosts)

	// For each host, ping all other hosts
	for _, sourceHost := range sortedHosts {
		sourceIP := hostIPMap[sourceHost]
		client, err := getQAClient(sourceHost)
		if err != nil {
			return fmt.Errorf("failed to get client for %s: %w", sourceHost, err)
		}

		for _, targetHost := range sortedHosts {
			if sourceHost == targetHost {
				continue
			}
			targetIP := hostIPMap[targetHost]

			t.Logf("Testing ping from %s (%s) to %s (%s)", sourceHost, sourceIP, targetHost, targetIP)

			// Determine if we need to use SourceIface based on exchange comparison
			sourceDevice := hostDeviceMap[sourceHost]
			targetDevice := hostDeviceMap[targetHost]
			useSourceIface := shouldUseSourceIfaceSimple(sourceDevice, targetDevice)

			if useRetry {
				// Use robust ping with retries for device testing
				err := performPingWithRetries(t, client, sourceIP, targetIP,
					sourceHost, targetHost, 3, useSourceIface)
				if err != nil {
					return err
				}
			} else {
				// Use simple ping for basic QA mode
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				pingReq := &pb.PingRequest{
					TargetIp: targetIP,
					SourceIp: sourceIP,
					PingType: pb.PingRequest_ICMP,
					Timeout:  10,
				}
				if useSourceIface {
					pingReq.SourceIface = "doublezero0"
					t.Logf("Sending ping request with -I doublezero0 (inter-exchange routing): target=%s, source=%s", targetIP, sourceIP)
				} else {
					t.Logf("Sending ping request WITHOUT -I doublezero0 (intra-exchange routing): target=%s, source=%s", targetIP, sourceIP)
				}
				pingResp, err := client.Ping(ctx, pingReq)
				cancel()

				if err != nil {
					return fmt.Errorf("ping from %s to %s failed: %w", sourceHost, targetHost, err)
				}

				if pingResp.PacketsSent == 0 || pingResp.PacketsReceived == 0 {
					return fmt.Errorf("ping from %s to %s failed: sent=%d, received=%d",
						sourceHost, targetHost, pingResp.PacketsSent, pingResp.PacketsReceived)
				}

				if pingResp.PacketsReceived < pingResp.PacketsSent {
					return fmt.Errorf("ping from %s to %s had loss: sent=%d, received=%d",
						sourceHost, targetHost, pingResp.PacketsSent, pingResp.PacketsReceived)
				}

				t.Logf("Successfully pinged %s from %s", targetHost, sourceHost)
			}
		}
	}

	return nil
}

func disconnectOnError(client pb.QAAgentServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = client.Disconnect(ctx, &emptypb.Empty{})
}

// performPingWithRetries executes a ping test with retry logic
func performPingWithRetries(t *testing.T, client pb.QAAgentServiceClient, sourceIP, targetIP, sourceName, targetName string, maxRetries int, useSourceIface bool) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			t.Logf("Retry attempt %d/%d after 2 second delay", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

		pingReq := &pb.PingRequest{
			TargetIp: targetIP,
			SourceIp: sourceIP,
			PingType: pb.PingRequest_ICMP,
		}
		if useSourceIface {
			pingReq.SourceIface = "doublezero0"
			t.Logf("Attempt %d: Sending ping request with -I doublezero0 (inter-exchange routing): target=%s, source=%s", attempt, targetIP, sourceIP)
		} else {
			t.Logf("Attempt %d: Sending ping request WITHOUT -I doublezero0 (intra-exchange routing): target=%s, source=%s", attempt, targetIP, sourceIP)
		}
		pingResp, err := client.Ping(pingCtx, pingReq)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("attempt %d: ping from %s to %s failed: %w", attempt, sourceName, targetName, err)
			t.Logf("Attempt %d failed: %v", attempt, err)
			continue
		}

		if pingResp.PacketsSent == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets sent from %s", attempt, sourceName)
			t.Logf("Attempt %d failed: no packets sent", attempt)
			continue
		}
		if pingResp.PacketsReceived == 0 {
			lastErr = fmt.Errorf("attempt %d: no packets received by %s (sent=%d)", attempt, sourceName, pingResp.PacketsSent)
			t.Logf("Attempt %d failed: no packets received (sent=%d)", attempt, pingResp.PacketsSent)
			continue
		}
		if pingResp.PacketsReceived < pingResp.PacketsSent {
			lastErr = fmt.Errorf("attempt %d: packet loss detected: sent=%d, received=%d", attempt, pingResp.PacketsSent, pingResp.PacketsReceived)
			t.Logf("Attempt %d failed: packet loss (sent=%d, received=%d)", attempt, pingResp.PacketsSent, pingResp.PacketsReceived)
			continue
		}

		t.Logf("Successfully pinged %s (%s) from %s (%s) (attempt %d)",
			targetName, targetIP, sourceName, sourceIP, attempt)
		lastErr = nil
		break
	}

	return lastErr
}

// findDeviceByHostIP finds the device that a host is connected to based on its IP
func findDeviceByHostIP(t *testing.T, ip string) *Device {
	ctx := context.Background()
	data, err := serviceabilityClient.GetProgramData(ctx)
	if err != nil {
		t.Logf("Warning: Failed to get program data for device lookup: %v", err)
		return nil
	}

	// Find user by IP
	var user *serviceability.User
	for i := range data.Users {
		u := &data.Users[i]
		userIP := net.IP(u.DzIp[:]).String()
		if userIP == ip {
			user = u
			break
		}
	}

	if user == nil {
		return nil
	}

	// Find the device from our global devices list
	for _, device := range devices {
		devicePubKey := base58.Encode(user.DevicePubKey[:])
		if device.PubKey == devicePubKey {
			return device
		}
	}

	return nil
}

// The intra-exchange routing policy defined in rfc6 dictates that unicast clients that are connected to the
// same exchange will communicate with each other over the internet instead of doublezero0. If they are
// connected to the same exchange, `ping -I doublezero0` will fail. This check lets us avoid that.
func shouldUseSourceIfaceSimple(sourceDevice, targetDevice *Device) bool {
	return sourceDevice.ExchangeCode != targetDevice.ExchangeCode
}
