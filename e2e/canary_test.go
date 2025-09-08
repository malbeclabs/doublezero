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

func TestConectivityUnicastToAllDevices(t *testing.T) {
	if len(devices) == 0 {
		t.Skip("No devices found on-chain")
	}

	// Loop through each device and test connectivity
	for _, device := range devices {
		t.Run(fmt.Sprintf("device_%s", device.Code), func(t *testing.T) {
			if !strings.HasPrefix(device.Code, "chi-dn-dzd") {
				t.Skipf("Skipping device %s as it does not match required code prefix", device.Code)
			}
			// Check if device has capacity for users
			if device.MaxUsers > 0 && device.UsersCount >= device.MaxUsers {
				t.Skipf("Device %s is at capacity (%d/%d users)", device.Code, device.UsersCount, device.MaxUsers)
			}

			// Connect to this specific device using device code
			testConnectivityToDevice(t, device)
		})
	}
}

func testConnectivityToDevice(t *testing.T, device *Device) {
	// Ensure we have at least 2 hosts for connectivity testing
	if len(hostList) < 2 {
		t.Skip("Need at least 2 hosts for connectivity testing")
	}

	cleanup := unicastCleanupFunc(t, hostList)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect all hosts to the specific device
	for _, host := range hostList {
		if !t.Run("connect_to_device_"+device.Code+"_from_"+host, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			// Connect with device code specified
			req := &pb.ConnectUnicastRequest{
				Mode:       pb.ConnectUnicastRequest_IBRL,
				DeviceCode: device.Code,
			}
			result, err := client.ConnectUnicast(ctx, req)
			log.Printf("ConnectUnicast result from host %s to device %s: %+v", host, device.Code, result)
			require.NoError(t, err, "ConnectUnicast to device %s failed", device.Code)

			if result.GetSuccess() == false || result.GetReturnCode() != 0 {
				require.Fail(t, "ConnectUnicast to device failed", "Device: %s, Output: %s", device.Code, result.GetOutput())
			}
		}) {
			t.Fatalf("Failed to connect to device %s from host %s", device.Code, host)
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
				t.Logf("Host %s connected to device %s with IP %s", host, device.Code, status.DoubleZeroIp)
				break
			}
		}
		require.NotEmpty(t, hostToIP[host], "No local address found for host %s on device %s", host, device.Code)
	}

	// Run connectivity checks between all hosts on this device
	for _, host := range hostList {
		t.Run("connectivity_check_on_device_"+device.Code+"_from_"+host, func(t *testing.T) {
			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			localAddr := hostToIP[host]

			// Ping all other hosts on this device
			peers := []string{}
			for peerHost, peerIP := range hostToIP {
				if peerHost == host {
					continue
				}
				peers = append(peers, peerIP)
			}

			require.NotEmpty(t, peers, "No peers found for connectivity check on device %s", device.Code)

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
					require.NoError(t, err, "Ping failed for %s on device %s", peer, device.Code)

					if pingResp.PacketsSent == 0 || pingResp.PacketsReceived == 0 {
						require.Fail(t, "Ping to %s on device %s failed: Sent=%d, Received=%d",
							peer, device.Code, pingResp.PacketsSent, pingResp.PacketsReceived)
					}

					if pingResp.PacketsReceived < pingResp.PacketsSent {
						require.Fail(t, "Ping to %s on device %s had loss: Sent=%d, Received=%d",
							peer, device.Code, pingResp.PacketsSent, pingResp.PacketsReceived)
					}

					t.Logf("Successfully pinged %s from %s on device %s", peer, localAddr, device.Code)
				})
			}
		})
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
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			client, err := getQAClient(host)
			require.NoError(t, err, "Failed to create QA client")

			_, err = client.Disconnect(ctx, &emptypb.Empty{})
			require.NoError(t, err, "Disconnect failed")
		})
	}
}
