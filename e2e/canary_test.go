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

func TestConectivityUnicast_AllDevices(t *testing.T) {
	if len(devices) == 0 {
		t.Skip("No devices found on-chain")
	}

	// Ensure we have exactly 2 hosts
	if len(hostList) != 2 {
		t.Fatal("Exactly 2 hosts are required for connectivity testing")
	}

	// Filter devices to only include those with matching prefix
	var validDevices []*Device
	for _, device := range devices {
		// Check if device has capacity for at least 2 users
		if device.MaxUsers > 0 && device.UsersCount >= device.MaxUsers-1 {
			t.Logf("Skipping device %s as it doesn't have capacity for 2 users (%d/%d users)",
				device.Code, device.UsersCount, device.MaxUsers)
			continue
		}
		validDevices = append(validDevices, device)
	}

	if len(validDevices) == 0 {
		t.Skip("No valid devices found with chi-dn-dzd prefix and sufficient capacity")
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

	// Now loop through all valid devices, connecting second host to each
	for _, device := range validDevices {
		t.Run(fmt.Sprintf("device_%s", device.Code), func(t *testing.T) {
			testUnicastConnectivityBetweenHosts(t, device, firstHost, firstHostIP, secondHost)
		})
	}
}

func testUnicastConnectivityBetweenHosts(t *testing.T, device *Device, firstHost string, firstHostIP string, secondHost string) {
	ctx := context.Background()

	// Connect second host to the device
	t.Logf("Connecting %s to device %s", secondHost, device.Code)
	client2, err := getQAClient(secondHost)
	require.NoError(t, err, "Failed to create QA client for second host")

	req2 := &pb.ConnectUnicastRequest{
		Mode:       pb.ConnectUnicastRequest_IBRL,
		DeviceCode: device.Code,
	}
	result2, err := client2.ConnectUnicast(ctx, req2)
	require.NoError(t, err, "Failed to connect second host to device %s", device.Code)
	require.True(t, result2.GetSuccess(), "Second host connection to device %s failed: %s", device.Code, result2.GetOutput())

	// Get the IP address of the second host
	resp2, err := client2.GetStatus(ctx, &emptypb.Empty{})
	require.NoError(t, err, "Failed to get status for second host")

	var secondHostIP string
	for _, status := range resp2.Status {
		if (status.UserType == "IBRL" || status.UserType == "IBRLWithAllocatedIP") && status.DoubleZeroIp != "" {
			secondHostIP = status.DoubleZeroIp
			break
		}
	}
	require.NotEmpty(t, secondHostIP, "Failed to get IP for second host on device %s", device.Code)
	t.Logf("Second host %s connected to device %s with IP %s", secondHost, device.Code, secondHostIP)

	// Ensure we disconnect second host when done with this device
	defer func() {
		t.Logf("Disconnecting second host %s from device %s", secondHost, device.Code)
		_, _ = client2.Disconnect(context.Background(), &emptypb.Empty{})
	}()

	// Test connectivity from second host to first host
	t.Run("ping_first_host", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		pingReq := &pb.PingRequest{
			TargetIp:    firstHostIP,
			SourceIp:    secondHostIP,
			SourceIface: "doublezero0",
			PingType:    pb.PingRequest_ICMP,
		}
		pingResp, err := client2.Ping(ctx, pingReq)
		require.NoError(t, err, "Ping from second host to first host failed on device %s", device.Code)

		require.NotZero(t, pingResp.PacketsSent, "No packets sent from second host")
		require.NotZero(t, pingResp.PacketsReceived, "No packets received by second host")
		require.Equal(t, pingResp.PacketsSent, pingResp.PacketsReceived,
			"Packet loss detected: Sent=%d, Received=%d", pingResp.PacketsSent, pingResp.PacketsReceived)

		t.Logf("Successfully pinged first host (%s) from second host (%s) on device %s",
			firstHostIP, secondHostIP, device.Code)
	})

	// Test connectivity from first host to second host
	t.Run("ping_second_host", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		client1, err := getQAClient(firstHost)
		require.NoError(t, err, "Failed to create QA client for first host")

		pingReq := &pb.PingRequest{
			TargetIp:    secondHostIP,
			SourceIp:    firstHostIP,
			SourceIface: "doublezero0",
			PingType:    pb.PingRequest_ICMP,
		}
		pingResp, err := client1.Ping(ctx, pingReq)
		require.NoError(t, err, "Ping from first host to second host failed on device %s", device.Code)

		require.NotZero(t, pingResp.PacketsSent, "No packets sent from first host")
		require.NotZero(t, pingResp.PacketsReceived, "No packets received by first host")
		require.Equal(t, pingResp.PacketsSent, pingResp.PacketsReceived,
			"Packet loss detected: Sent=%d, Received=%d", pingResp.PacketsSent, pingResp.PacketsReceived)

		t.Logf("Successfully pinged second host (%s) from first host (%s) on device %s",
			secondHostIP, firstHostIP, device.Code)
	})
}
