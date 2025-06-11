//go:build e2e

package e2e_test

import (
	"flag"
	"fmt"
	"log/slog"
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

func newTestDevnetConfig(t *testing.T) devnet.DevnetConfig {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()

	// Make a working directory for the generated keypairs.
	workDir, err := os.MkdirTemp("", "dz-devnet-"+deployID)
	if err != nil {
		t.Fatalf("failed to create working directory: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(workDir)
	})

	// Generate program keypair.
	// programKeypairPath := filepath.Join(workDir, "dz-program-keypair.json")
	// err = solana.GenerateKeypair(programKeypairPath)
	// if err != nil {
	// 	t.Fatalf("failed to generate program keypair: %v", err)
	// }
	// TODO(snormore): Generate these instead of hardcoding them.
	programKeypairPath := "data/dz-program-keypair.json"

	// Generate manager keypair.
	managerKeypairPath := filepath.Join(workDir, "dz-manager-keypair.json")
	err = solana.GenerateKeypair(managerKeypairPath)
	if err != nil {
		t.Fatalf("failed to generate manager keypair: %v", err)
	}

	return devnet.DevnetConfig{
		Logger:          logger,
		DeployID:        deployID,
		WorkDir:         workDir,
		SubnetAllocator: subnetAllocator,
		DockerClient:    dockerClient,

		ProgramKeypairPath: programKeypairPath,
		ManagerKeypairPath: managerKeypairPath,

		LedgerImage:     os.Getenv("DZ_LEDGER_IMAGE"),
		ControllerImage: os.Getenv("DZ_CONTROLLER_IMAGE"),
		ActivatorImage:  os.Getenv("DZ_ACTIVATOR_IMAGE"),
		ManagerImage:    os.Getenv("DZ_MANAGER_IMAGE"),
		DeviceImage:     os.Getenv("DZ_DEVICE_IMAGE"),
		ClientImage:     os.Getenv("DZ_CLIENT_IMAGE"),
	}
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

func disconnectUserTunnel(t *testing.T, log *slog.Logger, client *devnet.Client) {
	log.Info("==> Disconnecting user tunnel")

	_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect --client-ip " + client.IP})
	require.NoError(t, err)

	log.Info("--> User tunnel disconnected")
}

func waitForClientTunnelUp(t *testing.T, log *slog.Logger, client *devnet.Client) {
	ctx := t.Context()

	timeout := 90 * time.Second
	log.Info("==> Waiting for client tunnel to be up (timeout " + timeout.String() + ")")
	require.Eventually(t, func() bool {
		resp, err := client.ExecReturnJSONList(ctx, []string{"bash", "-c", `
				curl -s --unix-socket /var/run/doublezerod/doublezerod.sock http://doublezero/status
			`})
		require.NoError(t, err)
		log.Debug("--> Status response", "response", resp)

		for _, s := range resp {
			if session, ok := s["doublezero_status"]; ok {
				if sessionStatus, ok := session.(map[string]any)["session_status"]; ok {
					if sessionStatus == "up" {
						log.Info("✅ Client tunnel is up")
						return true
					}
				}
			}
		}
		return false
	}, timeout, 2*time.Second, "timeout waiting for client tunnel to be up")
}

func startSingleDeviceSingleClientDevnet(t *testing.T, log *slog.Logger) (*devnet.Devnet, *devnet.Device, *devnet.Client) {
	ctx := t.Context()

	devnetConfig := newTestDevnetConfig(t)
	devnet, err := devnet.New(devnetConfig)
	require.NoError(t, err)
	t.Cleanup(devnet.Close)

	err = devnet.StartControlPlane(ctx)
	require.NoError(t, err)

	// Create device CYOA network first, get the IP, then create it onchain.
	cyoaNetwork, cyoaSubnetCIDR, err := devnet.CreateCYOANetwork(ctx, "ny5-dz01")
	require.NoError(t, err)
	deviceCYOAIP, err := netutil.BuildIPInCIDR(cyoaSubnetCIDR, 80)
	require.NoError(t, err)

	// Create our device and a few others onchain.
	deviceCode := "ny5-dz01"
	createDevicesAndLinksOnchain(t, log, devnet, deviceCode, deviceCYOAIP.String())

	// Get the device agent pubkey from the onchain device list.
	deviceAgentPubkey := getDevicePubkeyOnchain(t, devnet, deviceCode)
	if deviceAgentPubkey == "" {
		t.Fatalf("device agent pubkey not found onchain for device %s", deviceCode)
	}

	// Start our device and connect it to the CYOA network.
	device, err := devnet.StartDevice(t, deviceCode, cyoaNetwork, cyoaSubnetCIDR, deviceCYOAIP.String(), deviceAgentPubkey)
	require.NoError(t, err)

	client, err := devnet.StartClient(ctx, device)
	require.NoError(t, err)

	// Add client to the user allowlist.
	_, err = devnet.ManagerExec(ctx, []string{"bash", "-c", `
		doublezero user allowlist add --pubkey ` + client.PubkeyAddress + `
	`})
	require.NoError(t, err)

	// Add null routes to test latency selection to ny5-dz01.
	_, err = client.Exec(ctx, []string{"bash", "-c", `
		echo "==> Adding null routes to test latency selection to ny5-dz01."
		ip rule add priority 1 from ` + client.IP + `/32 to all table main
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
	waitForLatencyResults(t, log, client, deviceAgentPubkey)

	return devnet, device, client
}

func getDevicePubkeyOnchain(t *testing.T, devnet *devnet.Devnet, deviceCode string) string {
	ctx := t.Context()

	output, err := devnet.ManagerExec(ctx, []string{"bash", "-c", `
		doublezero device get --code ny5-dz01
	`})
	require.NoError(t, err)

	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "account: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "account: "))
		}
	}

	return ""
}
func waitForLatencyResults(t *testing.T, log *slog.Logger, client *devnet.Client, expectedAgentPubkey string) {
	ctx := t.Context()

	start := time.Now()
	timeout := 75 * time.Second
	log.Info("==> Waiting for latency results (timeout " + timeout.String() + ")")
	require.Eventually(t, func() bool {
		results, err := client.ExecReturnJSONList(ctx, []string{"bash", "-c", `
				curl -s --unix-socket /var/run/doublezerod/doublezerod.sock http://doublezero/latency
			`})
		log.Debug("--> Latency results", "results", results)
		require.NoError(t, err)

		if len(results) > 0 {
			for _, result := range results {
				// Check to make sure ny5-dz01 is reachable
				if result["device_pk"] == expectedAgentPubkey && result["reachable"] == true {
					log.Info("✅ Got expected latency results", "duration", time.Since(start))
					return true
				}
			}
		}
		return false
	}, timeout, 2*time.Second, "timeout waiting for latency results")
}

func createDevicesAndLinksOnchain(t *testing.T, log *slog.Logger, devnet *devnet.Devnet, deviceCode string, deviceCYOAIP string) {
	ctx := t.Context()

	log.Info("==> Creating devices and links onchain")

	_, err := devnet.ManagerExec(ctx, []string{"bash", "-c", `
		set -e

		echo "==> Populate device information onchain - DO NOT SHUFFLE THESE AS THE PUBKEYS WILL CHANGE"
		doublezero device create --code la2-dz01 --location lax --exchange xlax --public-ip "207.45.216.134" --dz-prefixes "207.45.216.136/30,200.12.12.12/29"
		doublezero device create --code ny5-dz01 --location ewr --exchange xewr --public-ip "` + deviceCYOAIP + `" --dz-prefixes "` + deviceCYOAIP + `/29"
		doublezero device create --code ld4-dz01 --location lhr --exchange xlhr --public-ip "195.219.120.72" --dz-prefixes "195.219.120.72/29"
		doublezero device create --code frk-dz01 --location fra --exchange xfra --public-ip "195.219.220.88" --dz-prefixes "195.219.220.88/29"
		doublezero device create --code sg1-dz01 --location sin --exchange xsin --public-ip "180.87.102.104" --dz-prefixes "180.87.102.104/29"
		doublezero device create --code ty2-dz01 --location tyo --exchange xtyo --public-ip "180.87.154.112" --dz-prefixes "180.87.154.112/29"
		doublezero device create --code pit-dzd01 --location pit --exchange xpit --public-ip "204.16.241.243" --dz-prefixes "204.16.243.243/32"
		doublezero device create --code ams-dz001 --location ams --exchange xams --public-ip "195.219.138.50" --dz-prefixes "195.219.138.56/29"
		echo "--> Device information onchain:"
		doublezero device list

		echo "==> Populate link information onchain"
		doublezero link create --code "la2-dz01:ny5-dz01" --side-a la2-dz01 --side-z ny5-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 3
		doublezero link create --code "ny5-dz01:ld4-dz01" --side-a ny5-dz01 --side-z ld4-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 3
		doublezero link create --code "ld4-dz01:frk-dz01" --side-a ld4-dz01 --side-z frk-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 25 --jitter-ms 10
		doublezero link create --code "ld4-dz01:sg1-dz01" --side-a ld4-dz01 --side-z sg1-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 120 --jitter-ms 9
		doublezero link create --code "sg1-dz01:ty2-dz01" --side-a sg1-dz01 --side-z ty2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 40 --jitter-ms 7
		doublezero link create --code "ty2-dz01:la2-dz01" --side-a ty2-dz01 --side-z la2-dz01 --link-type L2 --bandwidth "10 Gbps" --mtu 9000 --delay-ms 30 --jitter-ms 10
		echo "--> Tunnel information onchain:"
		doublezero link list
	`})
	require.NoError(t, err)
}

func createMulticastGroupOnchain(t *testing.T, log *slog.Logger, devnet *devnet.Devnet, client *devnet.Client, multicastGroupCode string) {
	ctx := t.Context()

	log.Info("==> Creating multicast group onchain")

	_, err := devnet.ManagerExec(ctx, []string{"bash", "-c", `
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
		doublezero multicast group allowlist publisher add --code ` + multicastGroupCode + ` --pubkey ` + client.PubkeyAddress + `
		doublezero multicast group allowlist subscriber add --code ` + multicastGroupCode + ` --pubkey ` + client.PubkeyAddress + `
	`})
	require.NoError(t, err)
}

func waitForAgentConfigMatchViaController(t *testing.T, dn *devnet.Devnet, deviceAgentPubkey string, config string) error {
	ctx := t.Context()

	deadline := time.Now().Add(30 * time.Second)
	var diff string
	for time.Now().Before(deadline) {
		got, err := dn.GetAgentConfigViaController(ctx, deviceAgentPubkey)
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
