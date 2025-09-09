//go:build canary

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/require"
)

var (
	hosts                = flag.String("hosts", "", "comma separated list of hosts to run tests against")
	port                 = flag.String("port", "7009", "port to connect to on each host")
	env                  = flag.String("env", "", "environment to run in (devnet, testnet, mainnet-beta)")
	forcePublisher       = flag.String("force-publisher", "", "host to force as publisher for multicast tests (optional)")
	useGroup             = flag.String("use-group", "", "use existing multicast group by code (optional)")
	deviceCodeStartsWith = flag.String("device-code-startswith", "", "only test devices whose code starts with this string (optional)")

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

	if len(data.Devices) == 0 {
		log.Fatal("0 devices found on-chain")
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
		if len(hostList) != 2 {
			log.Fatal("Exactly two hosts are required to run the tests")
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

func TestConnectivityUnicast_AllDevices(t *testing.T) {
	if len(devices) == 0 {
		t.Skip("No devices found on-chain")
	}

	// Ensure we have exactly 2 hosts
	if len(hostList) != 2 {
		t.Fatal("Exactly 2 hosts are required for connectivity testing")
	}

	// Filter devices based on device-code-startswith flag and capacity
	var validDevices []*Device
	for _, device := range devices {
		// Apply device code prefix filter if specified
		if *deviceCodeStartsWith != "" && !strings.HasPrefix(device.Code, *deviceCodeStartsWith) {
			t.Logf("Skipping device %s as it doesn't match the prefix %s", device.Code, *deviceCodeStartsWith)
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
		if *deviceCodeStartsWith != "" {
			t.Skipf("No valid devices found with prefix %s and sufficient capacity", *deviceCodeStartsWith)
		} else {
			t.Skip("No valid devices found with sufficient capacity")
		}
	}

	// Connect first host to the first valid device and keep it connected for the entire test
	firstDevice := validDevices[0]
	firstHost := hostList[0]
	secondHost := hostList[1]

	ctx := context.Background()

	// Connect first host to first device
	t.Logf("Connecting %s to device %s (will stay connected)", firstHost, firstDevice.Code)
	client1, err := getQAClient(firstHost)
	require.NoError(t, err, "Failed to create QA client for first host")

	req1 := &pb.ConnectUnicastRequest{
		Mode:       pb.ConnectUnicastRequest_IBRL,
		DeviceCode: firstDevice.Code,
	}
	result1, err := client1.ConnectUnicast(ctx, req1)
	require.NoError(t, err, "Failed to connect first host to first device")
	require.True(t, result1.GetSuccess(), "First host connection failed: %s", result1.GetOutput())

	// Get the IP address of the first host
	resp1, err := client1.GetStatus(ctx, &emptypb.Empty{})
	require.NoError(t, err, "Failed to get status for first host")

	var firstHostIP string
	for _, status := range resp1.Status {
		if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
			firstHostIP = status.DoubleZeroIp
			break
		}
	}
	require.NotEmpty(t, firstHostIP, "Failed to get IP for first host")
	t.Logf("First host %s connected with IP %s", firstHost, firstHostIP)

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
			err := runUnicastConnectivityTest(t, device, firstHost, firstHostIP, secondHost)
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

// runUnicastConnectivityTest runs the connectivity test and returns an error if it fails
func runUnicastConnectivityTest(t *testing.T, device *Device, firstHost string, firstHostIP string, secondHost string) error {
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

	var secondHostIP string
	for _, status := range resp2.Status {
		if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
			secondHostIP = status.DoubleZeroIp
			break
		}
	}
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
