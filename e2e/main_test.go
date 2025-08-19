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
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/go-cmp/cmp"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
	logger          *slog.Logger
	subnetAllocator *docker.SubnetAllocator
	dockerClient    *client.Client
)

// TestMain is the entry point for the test suite. It runs before all tests and is used to
// initialize the logger and subnet allocator.
func TestMain(m *testing.M) {
	flag.Parse()
	verbose := false
	if vFlag := flag.Lookup("test.v"); vFlag != nil && vFlag.Value.String() == "true" {
		verbose = true
	}

	// Initialize a logger.
	logger = newTestLogger(verbose)
	if verbose {
		logger.Debug("==> Running with verbose logging")
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
	subnetAllocator = docker.NewSubnetAllocator("10.128.0.0/9", subnetCIDRPrefix, dockerClient)

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
		err = devnet.BuildContainerImages(context.Background(), logger, workspaceDir, verbose)
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
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with single device and client")

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
	dn.log.Info("==> Creating other devices and links onchain")
	dn.Manager.Exec(ctx, []string{"bash", "-c", `
		set -euo pipefail

		echo "==> Populate device information onchain"
		doublezero device create --code ld4-dz01 --contributor co01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29" --mgmt-vrf mgmt
		doublezero device create --code frk-dz01 --contributor co01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29" --mgmt-vrf mgmt
		doublezero device create --code sg1-dz01 --contributor co01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29" --mgmt-vrf mgmt
		doublezero device create --code ty2-dz01 --contributor co01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29" --mgmt-vrf mgmt
		doublezero device create --code pit-dzd01 --contributor co01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32" --mgmt-vrf mgmt
		doublezero device create --code ams-dz001 --contributor co01 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29" --mgmt-vrf mgmt
		echo "--> Device information onchain:"
		doublezero device list

		echo "==> Populate device interface information onchain"
		# TODO: When the controller supports dzd metadata, this will have to be updated to reflect actual interfaces
		doublezero device interface create ny5-dz01 "Ethernet2" physical
		doublezero device interface create ny5-dz01 "Vlan4001" physical
		doublezero device interface create la2-dz01 "Ethernet2" physical
		doublezero device interface create la2-dz01 "Ethernet3" physical
		doublezero device interface create ld4-dz01 "Vlan4001" physical
		doublezero device interface create ld4-dz01 "Ethernet3" physical
		doublezero device interface create ld4-dz01 "Ethernet4" physical
		doublezero device interface create frk-dz01 "Ethernet2" physical
		doublezero device interface create frk-dz01 "Ethernet3" physical
		doublezero device interface create sg1-dz01 "Ethernet2" physical
		doublezero device interface create sg1-dz01 "Ethernet3" physical
		doublezero device interface create ty2-dz01 "Ethernet2" physical
		doublezero device interface create ty2-dz01 "Ethernet3" physical
		doublezero device interface create pit-dzd01 "Ethernet2" physical
		doublezero device interface create pit-dzd01 "Ethernet3" physical
		doublezero device interface create ams-dz001 "Ethernet2" physical
		doublezero device interface create ams-dz001 "Ethernet3" physical

		doublezero device interface create ny5-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create la2-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create ld4-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create frk-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create sg1-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create ty2-dz01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create pit-dzd01 "Loopback255" loopback --loopback-type vpnv4
		doublezero device interface create ams-dz001 "Loopback255" loopback --loopback-type vpnv4

		doublezero device interface create ny5-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create la2-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create ld4-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create frk-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create sg1-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create ty2-dz01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create pit-dzd01 "Loopback256" loopback --loopback-type ipv4
		doublezero device interface create ams-dz001 "Loopback256" loopback --loopback-type ipv4

		echo "==> Populate link information onchain"
		doublezero link create wan --code "la2-dz01:ny5-dz01" --contributor co01 --side-a la2-dz01 --side-a-interface Ethernet2 --side-z ny5-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
		doublezero link create wan --code "ny5-dz01:ld4-dz01" --contributor co01 --side-a ny5-dz01 --side-a-interface Vlan4001 --side-z ld4-dz01 --side-z-interface Vlan4001 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
		doublezero link create wan --code "ld4-dz01:frk-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet3 --side-z frk-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
		doublezero link create wan --code "ld4-dz01:sg1-dz01" --contributor co01 --side-a ld4-dz01 --side-a-interface Ethernet4 --side-z sg1-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
		doublezero link create wan --code "sg1-dz01:ty2-dz01" --contributor co01 --side-a sg1-dz01 --side-a-interface Ethernet3 --side-z ty2-dz01 --side-z-interface Ethernet2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
		doublezero link create wan --code "ty2-dz01:la2-dz01" --contributor co01 --side-a ty2-dz01 --side-a-interface Ethernet2 --side-z la2-dz01 --side-z-interface Ethernet3 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10
		echo "--> Tunnel information onchain:"
		doublezero link list
	`})
	require.NoError(t, err)

	// Add a client to the devnet.
	client, err := dn.AddClient(ctx, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})
	require.NoError(t, err)

	// Add client to the user allowlist.
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero user allowlist add --pubkey " + client.Pubkey})
	require.NoError(t, err)

	// Set access pass for the client.
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip " + client.CYOANetworkIP + " --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)

	// Add null routes to test latency selection to ny5-dz01.
	_, err = client.Exec(ctx, []string{"bash", "-c", `
		echo "==> Adding null routes to test latency selection to ny5-dz01."
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

	return device, client
}

func (dn *TestDevnet) DisconnectUserTunnel(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Disconnecting user tunnel")

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> User tunnel disconnected")
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
	dn.log.Info("==> Creating multicast group onchain")

	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
			set -e

		echo "==> Populate multicast group information onchain"
		doublezero multicast group create --code ` + multicastGroupCode + ` --max-bandwidth 10Gbps --owner me

		echo "==> Waiting for multicast group to be activated by activator"
		sleep 5

		echo "--> Multicast group information onchain:"
		doublezero multicast group list

		echo "==> Add me to multicast group allowlist"
		doublezero multicast group allowlist publisher add --code ` + multicastGroupCode + ` --pubkey me
		doublezero multicast group allowlist subscriber add --code ` + multicastGroupCode + ` --pubkey me

		echo "==> Add client pubkey to multicast group allowlist"
		doublezero multicast group allowlist publisher add --code ` + multicastGroupCode + ` --pubkey ` + client.Pubkey + `
		doublezero multicast group allowlist subscriber add --code ` + multicastGroupCode + ` --pubkey ` + client.Pubkey + `
	`})
	require.NoError(t, err)
}

func (dn *TestDevnet) WaitForAgentConfigMatchViaController(t *testing.T, deviceAgentPubkey string, config string) error {
	deadline := time.Now().Add(30 * time.Second)
	var diff string
	for time.Now().Before(deadline) {
		got, err := dn.Controller.GetAgentConfig(t.Context(), deviceAgentPubkey)
		if err != nil {
			return fmt.Errorf("error while fetching config: %w", err)
		}
		diff = cmp.Diff(config, got.Config)
		if diff == "" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
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
	dn.log.Info("==> Connecting IBRL user tunnel")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip " + client.CYOANetworkIP + " --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> IBRL user tunnel connected")
}

// ConnectUserTunnelWithAllocatedIP connects a user tunnel with an allocated IP.
func (dn *TestDevnet) ConnectUserTunnelWithAllocatedIP(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Connecting user tunnel with allocated IP")

	// Set access pass for the client.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero access-pass set --accesspass-type Prepaid --client-ip " + client.CYOANetworkIP + " --payer " + client.Pubkey + " --last-access-epoch 99999"})
	require.NoError(t, err)

	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + client.CYOANetworkIP + " --allocate-addr"})
	require.NoError(t, err)

	dn.log.Info("--> User tunnel with allocated IP connected")
}

func (dn *TestDevnet) ConnectMulticastPublisher(t *testing.T, client *devnet.Client, multicastGroupCode string) {
	dn.log.Info("==> Connecting multicast publisher", "clientIP", client.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher " + multicastGroupCode + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to connect multicast publisher")

	dn.log.Info("--> Multicast publisher connected")
}

// DisconnectMulticastPublisher disconnects a multicast publisher from a multicast group.
func (dn *TestDevnet) DisconnectMulticastPublisher(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Disconnecting multicast publisher", "clientIP", client.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err, "failed to disconnect multicast publisher")

	dn.log.Info("--> Multicast publisher disconnected")
}

func (dn *TestDevnet) ConnectMulticastSubscriber(t *testing.T, client *devnet.Client, multicastGroupCode string) {
	dn.log.Info("==> Connecting multicast subscriber", "clientIP", client.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber " + multicastGroupCode + " --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> Multicast subscriber connected")
}

func (dn *TestDevnet) DisconnectMulticastSubscriber(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Disconnecting multicast subscriber", "clientIP", client.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + client.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> Multicast subscriber disconnected")
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
	start := networkInt + 1                          // first usable
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

func newTestLogger(verbose bool) *slog.Logger {
	logWriter := os.Stdout
	logLevel := slog.LevelDebug
	if !verbose {
		logLevel = slog.LevelInfo
	}
	logger := slog.New(tint.NewHandler(logWriter, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.DateTime,
	}))
	return logger
}
