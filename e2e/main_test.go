//go:build e2e

package e2e_test

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/go-cmp/cmp"
	"github.com/lmittmann/tint"
	controller "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

const (
	// Expected link-local address to be allocated to the client during test.
	expectedLinkLocalAddr = "169.254.0.1"

	// Subnet CIDR prefix.
	// Provides the full last octet range for devices and clients (2-254) for testing.
	subnetCIDRPrefix = 24
)

var (
	verbose         bool
	debug           bool
	logger          *slog.Logger
	subnetAllocator *docker.SubnetAllocator
	dockerClient    *client.Client
)

// testWriter wraps testing.T to implement io.Writer for slog output.
// This ensures logs only appear on test failure (standard go test behavior).
type testWriter struct {
	t  *testing.T
	mu sync.Mutex
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Logf("%s", p)
	return len(p), nil
}

// TestMain is the entry point for the test suite. It runs before all tests and is used to
// initialize the logger and subnet allocator.
func TestMain(m *testing.M) {
	flag.Parse()
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}
	if os.Getenv("DZ_E2E_DEBUG") != "" {
		debug = true
	}

	// Initialize a logger.
	logger = newTestLogger(verbose, debug)
	if debug {
		logger.Debug("==> Running with debug logging")
	}

	// Set the default logger for testcontainers.
	logging.SetTestcontainersLogger(logger)

	// Initialize a docker client.
	var err error
	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("failed to create docker client", "error", err)
		os.Exit(1)
	}

	// Initialize a subnet allocator.
	subnetAllocator = docker.NewSubnetAllocator("9.128.0.0/9", subnetCIDRPrefix, dockerClient)

	// Build the container images unless the "no build" environment variable is set.
	if os.Getenv("DZ_E2E_NO_BUILD") == "" {
		workspaceDir, err := devnet.WorkspaceDir()
		if err != nil {
			logger.Error("failed to find workspace directory", "error", err)
			os.Exit(1)
		}
		err = devnet.LoadContainerImagesEnvFile(logger, workspaceDir)
		if err != nil {
			logger.Error("failed to load env file", "error", err)
			os.Exit(1)
		}
		err = devnet.BuildContainerImages(context.Background(), logger, workspaceDir, debug)
		if err != nil {
			logger.Error("failed to build container images", "error", err)
			os.Exit(1)
		}
	}

	// Run the tests.
	os.Exit(m.Run())
}

type TestDevnet struct {
	*devnet.Devnet
	log *slog.Logger
}

func NewSingleDeviceSingleClientTestDevnet(t *testing.T) (*TestDevnet, *devnet.Device, *devnet.Client) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	log.Debug("==> Starting test devnet with single device and client")

	// Use a hardcoded serviceability program keypair for these tests, since device and account
	// pubkeys onchain are derived in the smartcontract and will change if the serviceability
	// program keypair changes. We create several devices onchain and test pubkey expectations
	// via fixtures.
	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	tdn := &TestDevnet{
		Devnet: dn,
		log:    logger,
	}

	device, client := tdn.Start(t)

	return tdn, device, client
}

func (dn *TestDevnet) Start(t *testing.T) (*devnet.Device, *devnet.Client) {
	ctx := t.Context()

	err := dn.Devnet.Start(ctx, nil)
	require.NoError(t, err)

	// Create a dummy device first to maintain same ordering of devices as before.
	err = dn.CreateDeviceOnchain(ctx, "la2-dz01", "lax", "xlax", "207.45.216.134", []string{"207.45.216.136/30", "200.12.12.12/29"}, "mgmt")
	require.NoError(t, err)

	// Add a device to the devnet and onchain.
	device, err := dn.AddDevice(ctx, devnet.DeviceSpec{
		Code:     "ny5-dz01",
		Location: "ewr",
		Exchange: "xewr",
		// .8/29 has network address .8, allocatable up to .14, and broadcast .15
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)

	// Add the other devices and links onchain.
	dn.log.Debug("==> Creating other devices and links onchain")
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.80/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.96/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.112/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.120/29" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt --desired-status activated
		doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29" --mgmt-vrf mgmt --desired-status activated

		# TODO: When the controller supports dzd metadata, this will have to be updated to reflect actual interfaces
		doublezero device interface create ny5-dz01 "Ethernet2" -w
		doublezero device interface create ny5-dz01 "Vlan4001" -w
		doublezero device interface create ny5-dz01 "Ethernet4" -w      # For testing link.delay_override_ms
		doublezero device interface create ny5-dz01 "Ethernet5" -w      # For testing link.status=soft-drained
		doublezero device interface create ny5-dz01 "Ethernet6" -w      # For testing link.status=soft-drained
		doublezero device interface create la2-dz01 "Ethernet2" -w
		doublezero device interface create la2-dz01 "Ethernet3" -w
		doublezero device interface create la2-dz01 "Ethernet4" -w      # For testing link.delay_override_ms
		doublezero device interface create la2-dz01 "Ethernet5" -w	# For testing link.status=soft-drained
		doublezero device interface create la2-dz01 "Ethernet6" -w      # For testing link.status=hard-drained
		doublezero device interface create ld4-dz01 "Vlan4001" -w
		doublezero device interface create ld4-dz01 "Ethernet3" -w
		doublezero device interface create ld4-dz01 "Ethernet4" -w
		doublezero device interface create frk-dz01 "Ethernet2" -w
		doublezero device interface create frk-dz01 "Ethernet3" -w
		doublezero device interface create sg1-dz01 "Ethernet2" -w
		doublezero device interface create sg1-dz01 "Ethernet3" -w
		doublezero device interface create ty2-dz01 "Ethernet2" -w
		doublezero device interface create ty2-dz01 "Ethernet3" -w
		doublezero device interface create pit-dzd01 "Ethernet2" -w
		doublezero device interface create pit-dzd01 "Ethernet3" -w
		doublezero device interface create ams-dz001 "Ethernet2" -w
		doublezero device interface create ams-dz001 "Ethernet3" -w

		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create la2-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ld4-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create frk-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create sg1-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ty2-dz01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create pit-dzd01 "Loopback255" --loopback-type vpnv4 -w
		doublezero device interface create ams-dz001 "Loopback255" --loopback-type vpnv4 -w

		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create la2-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ld4-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create frk-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create sg1-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ty2-dz01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create pit-dzd01 "Loopback256" --loopback-type ipv4 -w
		doublezero device interface create ams-dz001 "Loopback256" --loopback-type ipv4 -w

		doublezero device update --pubkey ld4-dz01 --max-users 128
		doublezero device update --pubkey frk-dz01 --max-users 128
		doublezero device update --pubkey sg1-dz01 --max-users 128
		doublezero device update --pubkey ty2-dz01 --max-users 128
		doublezero device update --pubkey pit-dzd01 --max-users 128
		doublezero device update --pubkey ams-dz001 --max-users 128

		doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ny5-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Vlan4001 --side-z ld4-dz01 --side-z-interface Vlan4001 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w
		doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet3 --side-z frk-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10 --desired-status activated -w
		doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet4 --side-z sg1-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9 --desired-status activated -w
		doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Ethernet3 --side-z ty2-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7 --desired-status activated -w
		doublezero link create wan --code "ty2-dz01:la2-dz01" --contributor co01 --side-a ty2-dz01 --side-a-interface Ethernet3 --side-z la2-dz01 --side-z-interface Ethernet3 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10 --desired-status activated -w

		# For testing link.delay_override_ms:
		doublezero link create wan --code "ny5-dz01:la2-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet4 --side-z la2-dz01 --side-z-interface Ethernet4 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w

		# For testing link.status=soft-drained:
		doublezero link create wan --code "ny5-dz01_e5:la2-dz01_e5" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet5 --side-z la2-dz01 --side-z-interface Ethernet5 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3 --desired-status activated -w

		# For testing link.status=hard-drained:
		doublezero link create wan --code "ny5-dz01_e6:la2-dz01_e6" --contributor co01 --side-a ny5-dz01 --side-a-interface Ethernet6 --side-z la2-dz01 --side-z-interface Ethernet6 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 8 --jitter-ms 3 --desired-status activated -w

		doublezero link update --pubkey "ny5-dz01:la2-dz01" --delay-override-ms 500
		doublezero link update --pubkey "ny5-dz01_e5:la2-dz01_e5" --status=soft-drained
		doublezero link update --pubkey "ny5-dz01_e6:la2-dz01_e6" --status=hard-drained
	`})
	require.NoError(t, err)

	// Add a client to the devnet.
	client, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Set access pass for the client.
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Add null routes to test latency selection to ny5-dz01.
	_, err = client.Exec(ctx, []string{"bash", "-c", `
		ip rule add priority 1 from ` + client.CYOANetworkIP + `/` + strconv.Itoa(dn.Spec.CYOANetwork.CIDRPrefix) + ` to all table main
		ip route add 207.45.216.134/32 dev lo proto static scope host
		ip route add 195.219.120.72/32 dev lo proto static scope host
		ip route add 195.219.220.88/32 dev lo proto static scope host
		ip route add 180.87.102.104/32 dev lo proto static scope host
		ip route add 180.87.154.112/32 dev lo proto static scope host
		ip route add 204.16.241.243/32 dev lo proto static scope host
		ip route add 195.219.138.50/32 dev lo proto static scope host
	`})
	require.NoError(t, err)

	// Wait for latency results.
	err = client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second)
	require.NoError(t, err)

	// Verify device has published telemetry to InfluxDB.
	if dn.InfluxDB != nil && dn.InfluxDB.InternalURL != "" {
		dn.log.Debug("==> Verifying device telemetry in InfluxDB", "device", device.Spec.Code, "pubkey", device.ID)
		require.Eventually(t, func() bool {
			hasData, err := dn.InfluxDB.HasDeviceData(ctx, device.ID)
			if err != nil {
				dn.log.Debug("Failed to query InfluxDB for device data", "error", err)
				return false
			}
			return hasData
		}, 60*time.Second, 3*time.Second, "device %s did not publish telemetry to InfluxDB", device.Spec.Code)
		dn.log.Debug("--> Device telemetry verified in InfluxDB")
	}

	// Verify device has getconfig metrics in Prometheus.
	if dn.Prometheus != nil && dn.Prometheus.InternalURL != "" {
		dn.log.Debug("==> Verifying device metrics in Prometheus", "device", device.Spec.Code, "pubkey", device.ID)
		require.Eventually(t, func() bool {
			hasMetrics, err := dn.Prometheus.HasDeviceMetrics(ctx, device.ID)
			if err != nil {
				dn.log.Debug("Failed to query Prometheus for device metrics", "error", err)
				return false
			}
			return hasMetrics
		}, 60*time.Second, 5*time.Second, "device %s did not have getconfig metrics in Prometheus", device.Spec.Code)
		dn.log.Debug("--> Device metrics verified in Prometheus")
	}

	return device, client
}

func (dn *TestDevnet) DisconnectUserTunnel(t *testing.T, client *devnet.Client) {
	dn.log.Debug("==> Disconnecting user tunnel")

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Debug("--> User tunnel disconnected")
}

func (dn *TestDevnet) GetDevicePubkeyOnchain(t *testing.T, deviceCode string) string {
	output, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero device get --code " + deviceCode})
	require.NoError(t, err)

	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "account: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "account: "))
		}
	}

	return ""
}

func (dn *TestDevnet) CreateMulticastGroupOnchain(t *testing.T, client *devnet.Client, multicastGroupCode string) {
	dn.log.Debug("==> Creating multicast group onchain")

	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -e
		doublezero multicast group create --code ` + multicastGroupCode + ` --max-bandwidth 10Gbps --owner me -w
		doublezero multicast group allowlist publisher add --code ` + multicastGroupCode + ` --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code ` + multicastGroupCode + ` --user-payer me --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist publisher add --code ` + multicastGroupCode + ` --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code ` + multicastGroupCode + ` --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)
}

func (dn *TestDevnet) WaitForAgentConfigMatchViaController(t *testing.T, deviceAgentPubkey string, config string) error {
	deadline := time.Now().Add(30 * time.Second)
	var diff string
	var got *controller.ConfigResponse
	for time.Now().Before(deadline) {
		var err error
		got, err = dn.Controller.GetAgentConfig(t.Context(), deviceAgentPubkey)
		if err != nil {
			return fmt.Errorf("error while fetching config: %w", err)
		}
		diff = cmp.Diff(got.Config, config)
		if diff == "" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println(got.Config)
	return fmt.Errorf("output mismatch: +(want), -(got): %s", diff)
}

func (dn *TestDevnet) WaitForUserActivation(t *testing.T) error {
	client, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err, "error getting serviceability client")

	condition := func() (bool, error) {
		data, err := client.GetProgramData(t.Context())
		if err != nil {
			return false, err
		}
		for _, user := range data.Users {
			if user.Status != serviceability.UserStatusActivated {
				return false, nil
			}
		}
		return true, nil
	}
	return poll.Until(t.Context(), condition, 90*time.Second, 5*time.Second)
}

func (dn *TestDevnet) ConnectIBRLUserTunnel(t *testing.T, client *devnet.Client) {
	dn.log.Debug("==> Connecting IBRL user tunnel")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Debug("--> IBRL user tunnel connected")
}

// ConnectUserTunnelWithAllocatedIP connects a user tunnel with an allocated IP.
func (dn *TestDevnet) ConnectUserTunnelWithAllocatedIP(t *testing.T, client *devnet.Client) {
	dn.log.Debug("==> Connecting user tunnel with allocated IP")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP + " --allocate-addr"})
	require.NoError(t, err)

	dn.log.Debug("--> User tunnel with allocated IP connected")
}

func (dn *TestDevnet) ConnectMulticastPublisher(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Connecting multicast publisher", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher " + groupArgs + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to connect multicast publisher")

	dn.log.Debug("--> Multicast publisher connected")
}

func (dn *TestDevnet) ConnectMulticastPublisherSkipAccessPass(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Connecting multicast publisher", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher " + groupArgs + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to connect multicast publisher")

	dn.log.Debug("--> Multicast publisher connected")
}

// DisconnectMulticastPublisher disconnects a multicast publisher from a multicast group.
func (dn *TestDevnet) DisconnectMulticastPublisher(t *testing.T, client *devnet.Client) {
	dn.log.Debug("==> Disconnecting multicast publisher", "clientIP", client.CYOANetworkIP)

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to disconnect multicast publisher")

	dn.log.Debug("--> Multicast publisher disconnected")
}

func (dn *TestDevnet) ConnectMulticastSubscriber(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Connecting multicast subscriber", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber " + groupArgs + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Debug("--> Multicast subscriber connected")
}

func (dn *TestDevnet) ConnectMulticastSubscriberSkipAccessPass(t *testing.T, client *devnet.Client, multicastGroupCodes ...string) {
	dn.log.Debug("==> Connecting multicast subscriber", "clientIP", client.CYOANetworkIP, "groups", multicastGroupCodes)

	groupArgs := strings.Join(multicastGroupCodes, " ")
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber " + groupArgs + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Debug("--> Multicast subscriber connected")
}

func (dn *TestDevnet) DisconnectMulticastSubscriber(t *testing.T, client *devnet.Client) {
	dn.log.Debug("==> Disconnecting multicast subscriber", "clientIP", client.CYOANetworkIP)

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type prepaid --client-ip " + client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Debug("--> Multicast subscriber disconnected")
}

// nextAllocatableIP returns the next available IPv4 address within a subnet,
// given a starting IP (assumed to be the network address), the subnet prefix length,
// and a set of already allocated IPs. It skips the network and broadcast addresses,
// and searches linearly for the first unallocated host address.
func nextAllocatableIP(ip string, allocatablePrefix int, allocated map[string]bool) (string, error) {
	network := net.ParseIP(ip).To4()
	if network == nil {
		return "", errors.New("only IPv4 is supported")
	}

	networkInt := binary.BigEndian.Uint32(network)
	start := networkInt                              // first usable
	end := networkInt + (1 << allocatablePrefix) - 2 // last usable

	for ipInt := start; ipInt <= end; ipInt++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, ipInt)
		if !allocated[ip.String()] {
			return ip.To4().String(), nil
		}
	}

	return "", errors.New("no allocatable IPs remaining")
}

// newTestLogger creates a logger for TestMain setup (before any tests run).
// This writes to stdout since there's no *testing.T available yet.
// With debug=true, shows DEBUG level logs; otherwise shows INFO level.
func newTestLogger(verbose, debug bool) *slog.Logger {
	logWriter := os.Stdout
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(tint.NewHandler(logWriter, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.DateTime,
	}))
	return logger
}

// GetDeviceLinkInterfaceIPs fetches link data from the ledger and returns a map of
// interface name -> IP address for the specified device. This is used to get the
// dynamically allocated link IPs for fixtures.
func (dn *TestDevnet) GetDeviceLinkInterfaceIPs(t *testing.T, deviceCode string) (map[string]string, error) {
	client, err := dn.Ledger.GetServiceabilityClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get serviceability client: %w", err)
	}

	data, err := client.GetProgramData(t.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}

	// Find the device by code to get its pubkey
	var devicePubkey [32]byte
	var deviceFound bool
	for _, dev := range data.Devices {
		if dev.Code == deviceCode {
			devicePubkey = dev.PubKey
			deviceFound = true
			break
		}
	}
	if !deviceFound {
		return nil, fmt.Errorf("device %s not found in program data", deviceCode)
	}

	result := make(map[string]string)

	for _, link := range data.Links {
		// Check if this link involves our device
		var ifaceName string
		var isSideA bool

		if link.SideAPubKey == devicePubkey {
			ifaceName = link.SideAIfaceName
			isSideA = true
		} else if link.SideZPubKey == devicePubkey {
			ifaceName = link.SideZIfaceName
			isSideA = false
		} else {
			continue // Link doesn't involve this device
		}

		// Extract IP from TunnelNet [5]uint8 (4 bytes IP + 1 byte prefix)
		// For a /31, Side A gets the first IP (even), Side Z gets the second IP (odd)
		baseIP := net.IP(link.TunnelNet[:4]).To4()
		if baseIP == nil {
			continue
		}

		var ip net.IP
		if isSideA {
			// Side A gets the base IP (first of the /31)
			ip = baseIP
		} else {
			// Side Z gets base IP + 1 (second of the /31)
			ipInt := binary.BigEndian.Uint32(baseIP)
			ipBytes := make(net.IP, 4)
			binary.BigEndian.PutUint32(ipBytes, ipInt+1)
			ip = ipBytes
		}

		prefixLen := link.TunnelNet[4]
		result[ifaceName] = fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	}

	return result, nil
}

// newTestLoggerForTest creates a logger for individual test runs.
// Logs are written to t.Log() so they only appear on test failure (unless -v is passed).
// With DZ_E2E_DEBUG=1, shows DEBUG level logs; otherwise shows INFO level.
func newTestLoggerForTest(t *testing.T) *slog.Logger {
	w := &testWriter{t: t}
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(w, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.DateTime,
	}))
}
