//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	"github.com/malbeclabs/doublezero/e2e/internal/logging"
	"github.com/malbeclabs/doublezero/e2e/internal/netutil"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// TestE2E_GeoprobeDiscovery verifies the full geoprobe flow:
// onchain probe creation → telemetry-agent discovery → TWAMP measurement → offset delivery.
func TestE2E_GeoprobeDiscovery(t *testing.T) {
	t.Parallel()

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	minBalanceSOL := 3.0
	topUpSOL := 5.0
	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),
		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		DeviceTunnelNet: "192.168.99.0/24",
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		Funder: devnet.FunderSpec{
			Verbose:       true,
			MinBalanceSOL: minBalanceSOL,
			TopUpSOL:      topUpSOL,
			Interval:      3 * time.Second,
		},
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Debug("==> Starting containernet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	// Dump diagnostic info on failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var buf strings.Builder
		fmt.Fprintf(&buf, "\n=== GEOPROBE DIAGNOSTIC DUMP (deploy=%s) ===\n", deployID)
		for code, device := range dn.Devices {
			for _, cmd := range []struct {
				label   string
				command []string
			}{
				{"doublezero-telemetry log (last 200 lines)", []string{"tail", "-200", "/var/log/agents-latest/doublezero-telemetry"}},
				{"Launcher log (last 50 lines)", []string{"bash", "-c", "tail -50 /var/log/agents/Launcher-*"}},
			} {
				output, err := device.Exec(ctx, cmd.command)
				if err != nil {
					fmt.Fprintf(&buf, "\n--- Device %s: %s (ERROR: %v)\n", code, cmd.label, err)
				} else {
					fmt.Fprintf(&buf, "\n--- Device %s: %s\n%s", code, cmd.label, string(output))
				}
			}
		}
		fmt.Fprintf(&buf, "\n=== GEOPROBE DIAGNOSTIC DUMP END ===\n")
		fmt.Fprint(os.Stderr, buf.String())
	})

	// Create a link network for device-to-device connectivity.
	linkNetwork := devnet.NewMiscNetwork(dn, log, "ams-dz01:ams-dz02")
	_, err = linkNetwork.CreateIfNotExists(t.Context())
	require.NoError(t, err)

	// Add 2 devices in parallel at the same exchange (xams).
	var (
		dz1TelemetryKeypairPK solana.PublicKey
		dz2TelemetryKeypairPK solana.PublicKey
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		telemetryKeypair := solana.NewWallet().PrivateKey
		telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
		telemetryKeypairPath := t.TempDir() + "/ams-dz01-telemetry-keypair.json"
		require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
		dz1TelemetryKeypairPK = telemetryKeypair.PublicKey()

		_, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:                         "ams-dz01",
			Location:                     "ams",
			Exchange:                     "xams",
			MetricsPublisherPK:           dz1TelemetryKeypairPK.String(),
			CYOANetworkIPHostID:          8,
			CYOANetworkAllocatablePrefix: 29,
			Telemetry: devnet.DeviceTelemetrySpec{
				Enabled:              true,
				KeypairPath:          telemetryKeypairPath,
				TWAMPListenPort:      862,
				ProbeInterval:        2 * time.Second,
				SubmissionInterval:   5 * time.Second,
				PeersRefreshInterval: 5 * time.Second,
				Verbose:              true,
			},
			AdditionalNetworks: []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
			},
		})
		require.NoError(t, err)
		requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), dz1TelemetryKeypairPK, minBalanceSOL, "dz1 telemetry publisher")
	}()
	go func() {
		defer wg.Done()
		telemetryKeypair := solana.NewWallet().PrivateKey
		telemetryKeypairJSON, _ := json.Marshal(telemetryKeypair[:])
		telemetryKeypairPath := t.TempDir() + "/ams-dz02-telemetry-keypair.json"
		require.NoError(t, os.WriteFile(telemetryKeypairPath, telemetryKeypairJSON, 0600))
		dz2TelemetryKeypairPK = telemetryKeypair.PublicKey()

		_, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
			Code:                         "ams-dz02",
			Location:                     "ams",
			Exchange:                     "xams",
			MetricsPublisherPK:           dz2TelemetryKeypairPK.String(),
			CYOANetworkIPHostID:          16,
			CYOANetworkAllocatablePrefix: 29,
			Telemetry: devnet.DeviceTelemetrySpec{
				Enabled:              true,
				KeypairPath:          telemetryKeypairPath,
				TWAMPListenPort:      862,
				ProbeInterval:        2 * time.Second,
				SubmissionInterval:   5 * time.Second,
				PeersRefreshInterval: 5 * time.Second,
				Verbose:              true,
			},
			AdditionalNetworks: []string{linkNetwork.Name},
			Interfaces: map[string]string{
				"Ethernet2": "physical",
			},
		})
		require.NoError(t, err)
		requireEventuallyFunded(t, log, dn.Ledger.GetRPCClient(), dz2TelemetryKeypairPK, minBalanceSOL, "dz2 telemetry publisher")
	}()
	wg.Wait()

	_ = dz2TelemetryKeypairPK // dz2 exists to form a link pair; not directly used below.

	dz1 := dn.Devices["ams-dz01"]
	require.NotNil(t, dz1)

	// Compute the geoprobe container's CYOA IP (host ID 32).
	geoprobeHostID := uint32(32)
	geoprobeIP, err := netutil.DeriveIPFromCIDR(dn.CYOANetwork.SubnetCIDR, geoprobeHostID)
	require.NoError(t, err)
	geoprobeIPStr := geoprobeIP.To4().String()
	log.Debug("==> Geoprobe CYOA IP", "ip", geoprobeIPStr)

	// Get the exchange PK for xams from onchain.
	exchangePK := getExchangePK(t, dn, "xams")
	log.Debug("==> Exchange PK", "exchange", "xams", "pk", exchangePK)

	// Get dz1's device PK from onchain.
	dz1DevicePK := dz1.ID
	require.NotEmpty(t, dz1DevicePK)
	log.Debug("==> DZ1 device PK", "pk", dz1DevicePK)

	// Create geoprobe onchain.
	log.Debug("==> Creating geoprobe onchain")
	geoprobeAccountPK := createGeoprobeOnchain(t, dn, "geoprobe1", exchangePK, geoprobeIPStr, dz1TelemetryKeypairPK.String())
	log.Debug("==> Geoprobe created", "accountPK", geoprobeAccountPK)

	// Add dz1 as parent of the geoprobe.
	log.Debug("==> Adding dz1 as geoprobe parent")
	addGeoprobeParent(t, dn, "geoprobe1", dz1DevicePK)

	// Start the geoprobe container.
	log.Debug("==> Starting geoprobe container")
	geoprobeContainerID := startGeoprobeContainer(t, log, dn, geoprobeIPStr)
	log.Debug("==> Geoprobe container started", "containerID", geoprobeContainerID)

	// Generate keypair and start agent inside the container.
	log.Debug("==> Starting geoprobe agent")
	startGeoprobeAgent(t, dn, geoprobeContainerID, geoprobeAccountPK, dn.Manager.GeolocationProgramID, dn.Manager.ServiceabilityProgramID)
	log.Debug("==> Geoprobe agent started")

	// Wait for dz1's telemetry agent to discover the geoprobe and successfully probe it.
	log.Debug("==> Waiting for geoprobe discovery and successful measurement")
	waitForGeoprobeSuccess(t, dz1, geoprobeIPStr, 180*time.Second)
	log.Debug("==> Geoprobe discovery and measurement verified")
}

// getExchangePK retrieves the onchain PK for an exchange by its code.
func getExchangePK(t *testing.T, dn *devnet.Devnet, exchangeCode string) string {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{
		"doublezero", "exchange", "get", "--code", exchangeCode, "--json",
	})
	require.NoError(t, err, "exchange get failed: %s", string(output))

	var result struct {
		Account string `json:"account"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.NotEmpty(t, result.Account, "exchange account PK should not be empty")
	return result.Account
}

// createGeoprobeOnchain creates a geoprobe account via the geolocation CLI on the manager.
// Returns the geoprobe account PK (base58).
func createGeoprobeOnchain(t *testing.T, dn *devnet.Devnet, code, exchangePK, publicIP, metricsPublisherPK string) string {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{
		"doublezero-geolocation", "probe", "create",
		"--code", code,
		"--exchange", exchangePK,
		"--public-ip", publicIP,
		"--port", "8923",
		"--metrics-publisher", metricsPublisherPK,
	})
	require.NoError(t, err, "probe create failed: %s", string(output))

	// Parse the geoprobe account PK from the output.
	// Expected: "Account: <base58>"
	outputStr := string(output)
	for _, line := range strings.Split(outputStr, "\n") {
		if strings.HasPrefix(line, "Account:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}

	// Fallback: look up via probe get.
	return getGeoprobePK(t, dn, code)
}

// getGeoprobePK looks up a geoprobe's onchain PK by code.
func getGeoprobePK(t *testing.T, dn *devnet.Devnet, code string) string {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{
		"doublezero-geolocation", "probe", "get", "--probe", code, "--json",
	})
	require.NoError(t, err, "probe get failed: %s", string(output))

	var result struct {
		Account string `json:"account"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.NotEmpty(t, result.Account, "geoprobe account PK should not be empty")
	return result.Account
}

// addGeoprobeParent adds a device as a parent of the geoprobe.
func addGeoprobeParent(t *testing.T, dn *devnet.Devnet, code, devicePK string) {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{
		"doublezero-geolocation", "probe", "add-parent",
		"--code", code,
		"--device", devicePK,
	})
	require.NoError(t, err, "probe add-parent failed: %s", string(output))
}

// startGeoprobeContainer creates and starts a generic Linux container from the geoprobe image,
// connected to both the default and CYOA networks with a specific CYOA IP.
func startGeoprobeContainer(t *testing.T, log *slog.Logger, dn *devnet.Devnet, cyoaIP string) string {
	t.Helper()

	geoprobeImage := os.Getenv("DZ_GEOPROBE_IMAGE")
	require.NotEmpty(t, geoprobeImage, "DZ_GEOPROBE_IMAGE must be set")

	// Start with only the default network. We'll attach to CYOA manually with a specific IP.
	req := testcontainers.ContainerRequest{
		Image: geoprobeImage,
		Name:  dn.Spec.DeployID + "-geoprobe",
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = "geoprobe"
		},
		Networks: []string{dn.DefaultNetwork.Name},
		Resources: dockercontainer.Resources{
			NanoCPUs: 1_000_000_000,
			Memory:   512 * 1024 * 1024,
		},
	}

	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Logger:           logging.NewTestcontainersAdapter(log),
	})
	require.NoError(t, err)

	containerID := container.GetContainerID()

	// Connect to CYOA network with the specific IP.
	err = dockerClient.NetworkConnect(t.Context(), dn.CYOANetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: cyoaIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: cyoaIP,
		},
	})
	require.NoError(t, err, "failed to connect geoprobe to CYOA network with IP %s", cyoaIP)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if t.Failed() {
			output, err := docker.Exec(ctx, dockerClient, containerID, []string{
				"bash", "-c", "cat /tmp/geoprobe-agent.log 2>/dev/null || echo 'no log file'",
			})
			if err == nil {
				fmt.Fprintf(os.Stderr, "\n--- Geoprobe agent log ---\n%s\n", string(output))
			}
		}
		_ = container.Terminate(ctx)
	})

	return containerID
}

// startGeoprobeAgent generates a keypair inside the container and starts the geoprobe-agent.
func startGeoprobeAgent(t *testing.T, dn *devnet.Devnet, containerID, geoprobeAccountPK, geolocationProgramID, serviceabilityProgramID string) {
	t.Helper()

	// Generate a keypair inside the container.
	_, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
		"solana-keygen", "new", "--no-bip39-passphrase", "-o", "/tmp/geoprobe-keypair.json", "--force",
	})
	require.NoError(t, err)

	// Start geoprobe-agent in the background.
	cmd := fmt.Sprintf(
		"nohup doublezero-geoprobe-agent "+
			"-ledger-rpc-url %s "+
			"-keypair /tmp/geoprobe-keypair.json "+
			"-geoprobe-pubkey %s "+
			"-geolocation-program-id %s "+
			"-serviceability-program-id %s "+
			"-twamp-listen-port 8925 "+
			"-udp-listen-port 8923 "+
			"-verbose "+
			"> /tmp/geoprobe-agent.log 2>&1 &",
		dn.Ledger.InternalRPCURL,
		geoprobeAccountPK,
		geolocationProgramID,
		serviceabilityProgramID,
	)
	_, err = docker.Exec(t.Context(), dockerClient, containerID, []string{"bash", "-c", cmd})
	require.NoError(t, err)

	// Verify the agent started.
	require.Eventually(t, func() bool {
		output, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
			"bash", "-c", "pgrep -f doublezero-geoprobe-agent",
		})
		return err == nil && len(strings.TrimSpace(string(output))) > 0
	}, 10*time.Second, 1*time.Second, "geoprobe-agent process should be running")
}

// waitForGeoprobeSuccess polls the telemetry-agent log on a device until it shows
// successful geoprobe discovery and measurement.
func waitForGeoprobeSuccess(t *testing.T, device *devnet.Device, geoprobeIP string, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		output, err := device.Exec(t.Context(), []string{
			"tail", "-300", "/var/log/agents-latest/doublezero-telemetry",
		})
		if err != nil {
			return false
		}
		logStr := string(output)

		hasProbeSuccess := strings.Contains(logStr, "Probe succeeded") && strings.Contains(logStr, geoprobeIP)
		hasOffsetDelivery := strings.Contains(logStr, "sent offset to probe") && strings.Contains(logStr, geoprobeIP)

		return hasProbeSuccess && hasOffsetDelivery
	}, timeout, 5*time.Second, "Expected telemetry log to show 'Probe succeeded' and 'sent offset to probe' for geoprobe IP %s", geoprobeIP)
}
