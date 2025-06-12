//go:build e2e

package e2e_test

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/go-cmp/cmp"
	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/stretchr/testify/require"
)

const (
	// Expected link-local address to be allocated to the client during test.
	expectedLinkLocalAddr = "169.254.0.1"
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

	// Load environment variables file with docker images repos/names.
	envFilePath := ".env.local"
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		logger.Error("env file not found", "path", envFilePath)
		os.Exit(1)
	}
	if err := godotenv.Load(envFilePath); err != nil {
		logger.Error("failed to load env file", "error", err, "path", envFilePath)
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
	subnetAllocator = docker.NewSubnetAllocator("10.128.0.0/9", 24, dockerClient)

	// Run the tests.
	os.Exit(m.Run())
}

type TestDevnet struct {
	*devnet.Devnet
	log *slog.Logger
}

func NewSingleDeviceSingleClientTestDevnet(t *testing.T) *TestDevnet {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	log.Info("==> Starting test devnet with single device and client")

	// Make a working directory for the generated keypairs.
	workingDir, err := os.MkdirTemp("", "dz-devnet-"+deployID)
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(workingDir)
	})

	// Use a hardcoded program keypair for these tests, since device and account pubkeys onchain
	// are derived in the smartcontract and will change if the program keypair changes. We create
	// several devices onchain and test pubkey expectations via fixtures.
	programKeypairPath := "data/dz-program-keypair.json"

	// Generate manager keypair.
	managerKeypairPath := filepath.Join(workingDir, "dz-manager-keypair.json")
	err = solana.GenerateKeypair(managerKeypairPath)
	if err != nil {
		t.Fatal("failed to generate manager keypair")
	}

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:   deployID,
		WorkingDir: workingDir,

		Ledger: devnet.LedgerSpec{
			ContainerImage:     os.Getenv("DZ_LEDGER_IMAGE"),
			ProgramKeypairPath: programKeypairPath,
		},
		Manager: devnet.ManagerSpec{
			ContainerImage: os.Getenv("DZ_MANAGER_IMAGE"),
			KeypairPath:    managerKeypairPath,
		},
		Controller: devnet.ControllerSpec{
			ContainerImage: os.Getenv("DZ_CONTROLLER_IMAGE"),
			ExternalHost:   "localhost",
		},
		Activator: devnet.ActivatorSpec{
			ContainerImage: os.Getenv("DZ_ACTIVATOR_IMAGE"),
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	tdn := &TestDevnet{
		Devnet: dn,
		log:    logger,
	}

	tdn.Start(t)

	return tdn
}

func (dn *TestDevnet) Start(t *testing.T) {
	ctx := t.Context()

	err := dn.Devnet.Start(ctx)
	require.NoError(t, err)

	// Build a device CYOA IP from the subnet CIDR.
	deviceCYOAIP, err := netutil.BuildIPInCIDR(dn.CYOANetwork.SubnetCIDR, 80)
	require.NoError(t, err)

	// Create our device and a few others onchain.
	deviceCode := "ny5-dz01"
	dn.createDevicesAndLinksOnchain(t, deviceCode, deviceCYOAIP.String())

	// Get the device agent pubkey from the onchain device list.
	deviceAgentPubkey := dn.GetDevicePubkeyOnchain(t, deviceCode)
	require.NotEmpty(t, deviceAgentPubkey, "device agent pubkey not found onchain for device %s", deviceCode)

	// Add a device to the devnet.
	_, err = dn.AddDevice(ctx, devnet.DeviceSpec{
		ContainerImage: os.Getenv("DZ_DEVICE_IMAGE"),
		Code:           deviceCode,
		Pubkey:         deviceAgentPubkey,
		CYOANetworkIP:  deviceCYOAIP.String(),
	})
	require.NoError(t, err)

	// Build a client CYOA IP from the subnet CIDR.
	clientCYOAIP, err := netutil.BuildIPInCIDR(dn.CYOANetwork.SubnetCIDR, 86)
	require.NoError(t, err)

	// Generate a new client keypair.
	clientKeypairPath := filepath.Join(dn.Spec.WorkingDir, "client-keypair.json")
	err = solana.GenerateKeypair(clientKeypairPath)
	require.NoError(t, err)

	// Add a client to the devnet.
	clientIndex, err := dn.AddClient(ctx, devnet.ClientSpec{
		ContainerImage: os.Getenv("DZ_CLIENT_IMAGE"),
		KeypairPath:    clientKeypairPath,
		CYOANetworkIP:  clientCYOAIP.String(),
	})
	require.NoError(t, err)
	client := dn.Clients[clientIndex]
	clientSpec := client.Spec()

	// Add client to the user allowlist.
	_, err = dn.Manager.Exec(ctx, []string{"bash", "-c", "doublezero user allowlist add --pubkey " + client.Pubkey})
	require.NoError(t, err)

	// Add null routes to test latency selection to ny5-dz01.
	_, err = client.Exec(ctx, []string{"bash", "-c", `
		echo "==> Adding null routes to test latency selection to ny5-dz01."
		ip rule add priority 1 from ` + clientSpec.CYOANetworkIP + `/32 to all table main
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
	dn.waitForLatencyResults(t, client, deviceAgentPubkey)
}

func (dn *TestDevnet) DisconnectUserTunnel(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Disconnecting user tunnel")

	clientSpec := client.Spec()
	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> User tunnel disconnected")
}

func (dn *TestDevnet) WaitForClientTunnelUp(t *testing.T, client *devnet.Client) {
	timeout := 90 * time.Second
	dn.log.Info("==> Waiting for client tunnel to be up (timeout " + timeout.String() + ")")

	require.Eventually(t, func() bool {
		resp, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", `
				curl -s --unix-socket /var/run/doublezerod/doublezerod.sock http://doublezero/status
			`})
		require.NoError(t, err)
		dn.log.Debug("--> Status response", "response", resp)

		for _, s := range resp {
			if session, ok := s["doublezero_status"]; ok {
				if sessionStatus, ok := session.(map[string]any)["session_status"]; ok {
					if sessionStatus == "up" {
						dn.log.Info("✅ Client tunnel is up")
						return true
					}
				}
			}
		}
		return false
	}, 90*time.Second, 2*time.Second, "timeout waiting for client tunnel to be up")
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

func (dn *TestDevnet) waitForLatencyResults(t *testing.T, client *devnet.Client, expectedAgentPubkey string) {
	start := time.Now()
	timeout := 75 * time.Second
	dn.log.Info("==> Waiting for latency results (timeout " + timeout.String() + ")")
	require.Eventually(t, func() bool {
		results, err := client.ExecReturnJSONList(t.Context(), []string{"bash", "-c", "curl -s --unix-socket /var/run/doublezerod/doublezerod.sock http://doublezero/latency"})
		dn.log.Debug("--> Latency results", "results", results)
		require.NoError(t, err)

		if len(results) > 0 {
			for _, result := range results {
				// Check to make sure ny5-dz01 is reachable
				if result["device_pk"] == expectedAgentPubkey && result["reachable"] == true {
					dn.log.Info("✅ Got expected latency results", "duration", time.Since(start))
					return true
				}
			}
		}
		return false
	}, timeout, 2*time.Second, "timeout waiting for latency results")
}

func (dn *TestDevnet) createDevicesAndLinksOnchain(t *testing.T, deviceCode string, deviceCYOAIP string) {
	dn.log.Info("==> Creating devices and links onchain", "deviceCode", deviceCode, "deviceCYOAIP", deviceCYOAIP)

	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -e

		echo "==> Populate device information onchain - DO NOT SHUFFLE THESE AS THE PUBKEYS WILL CHANGE"
		doublezero device create --code la2-dz01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/30,200.12.12.12/29"
		doublezero device create --code ` + deviceCode + ` --location ewr --exchange xewr --public-ip "` + deviceCYOAIP + `" --dz-prefixes "` + deviceCYOAIP + `/29"
		doublezero device create --code ld4-dz01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29"
		doublezero device create --code frk-dz01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"
		doublezero device create --code sg1-dz01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29"
		doublezero device create --code ty2-dz01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29"
		doublezero device create --code pit-dzd01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32"
		doublezero device create --code ams-dz001 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29"
		echo "--> Device information onchain:"
		doublezero device list

		echo "==> Populate link information onchain"
		doublezero link create --code "la2-dz01:` + deviceCode + `" --side-a la2-dz01 --side-z ` + deviceCode + ` --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
		doublezero link create --code "` + deviceCode + `:ld4-dz01" --side-a ` + deviceCode + ` --side-z ld4-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
		doublezero link create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
		doublezero link create --code "ld4-dz01:sg1-dz01" --side-a ld4-dz01 --side-z sg1-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
		doublezero link create --code "sg1-dz01:ty2-dz01" --side-a sg1-dz01 --side-z ty2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
		doublezero link create --code "ty2-dz01:la2-dz01" --side-a ty2-dz01 --side-z la2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10
		echo "--> Tunnel information onchain:"
		doublezero link list
	`})
	require.NoError(t, err)
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

func (dn *TestDevnet) ConnectIBRLUserTunnel(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Connecting IBRL user tunnel")

	clientSpec := client.Spec()

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> IBRL user tunnel connected")
}

// ConnectUserTunnelWithAllocatedIP connects a user tunnel with an allocated IP.
func (dn *TestDevnet) ConnectUserTunnelWithAllocatedIP(t *testing.T, client *devnet.Client) {
	dn.log.Info("==> Connecting user tunnel with allocated IP")

	clientSpec := client.Spec()

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl --client-ip " + clientSpec.CYOANetworkIP + " --allocate-addr"})
	require.NoError(t, err)

	dn.log.Info("--> User tunnel with allocated IP connected")
}

func (dn *TestDevnet) ConnectMulticastPublisher(t *testing.T, client *devnet.Client, multicastGroupCode string) {
	clientSpec := client.Spec()
	dn.log.Info("==> Connecting multicast publisher", "clientIP", clientSpec.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher " + multicastGroupCode + " --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err, "failed to connect multicast publisher")

	dn.log.Info("--> Multicast publisher connected")
}

// DisconnectMulticastPublisher disconnects a multicast publisher from a multicast group.
func (dn *TestDevnet) DisconnectMulticastPublisher(t *testing.T, client *devnet.Client) {
	clientSpec := client.Spec()
	dn.log.Info("==> Disconnecting multicast publisher", "clientIP", clientSpec.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err, "failed to disconnect multicast publisher")

	dn.log.Info("--> Multicast publisher disconnected")
}

func (dn *TestDevnet) ConnectMulticastSubscriber(t *testing.T, client *devnet.Client, multicastGroupCode string) {
	clientSpec := client.Spec()
	dn.log.Info("==> Connecting multicast subscriber", "clientIP", clientSpec.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast subscriber " + multicastGroupCode + " --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> Multicast subscriber connected")
}

func (dn *TestDevnet) DisconnectMulticastSubscriber(t *testing.T, client *devnet.Client) {
	clientSpec := client.Spec()
	dn.log.Info("==> Disconnecting multicast subscriber", "clientIP", clientSpec.CYOANetworkIP)

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast --client-ip " + clientSpec.CYOANetworkIP})
	require.NoError(t, err)

	dn.log.Info("--> Multicast subscriber disconnected")
}

// getNextAllocatedClientIP returns the next allocated client IP address in the subnet based on a
// given previous IP, by simply incrementing the last octet. This is a naive implementation that
// does not consider the actual subnet CIDR and will break if used for a lot of IPs.
func getNextAllocatedClientIP(previousIP string) string {
	ip := net.ParseIP(previousIP).To4()
	ip[3]++
	return ip.String()
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
		// NoColor:    !isatty.IsTerminal(logWriter.Fd()),
	}))
	return logger
}
