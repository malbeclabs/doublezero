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
// onchain probe creation → telemetry-agent discovery → TWAMP measurement → offset delivery →
// composite offset forwarding to a target.
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

	// Compute the geoprobe target container's CYOA IP (host ID 40).
	targetHostID := uint32(40)
	targetIP, err := netutil.DeriveIPFromCIDR(dn.CYOANetwork.SubnetCIDR, targetHostID)
	require.NoError(t, err)
	targetIPStr := targetIP.To4().String()
	log.Debug("==> Geoprobe target CYOA IP", "ip", targetIPStr)

	// Start the geoprobe target container and process before the agent so it's
	// ready to receive probes and offsets.
	log.Debug("==> Starting geoprobe target container")
	targetContainerID := startGeoprobeTargetContainer(t, log, dn, targetIPStr)
	startGeoprobeTarget(t, targetContainerID)
	log.Debug("==> Geoprobe target started", "containerID", targetContainerID)

	// Generate keypairs for both agent and target-sender before starting either,
	// so the agent can be configured with --allowed-pubkeys for the sender.
	log.Debug("==> Starting geoprobe container")
	geoprobeContainerID := startGeoprobeContainer(t, log, dn, geoprobeIPStr)
	log.Debug("==> Geoprobe container started", "containerID", geoprobeContainerID)

	agentKeypairPath := "/tmp/geoprobe-keypair.json"
	agentPubkey := generateKeypair(t, geoprobeContainerID, agentKeypairPath)
	log.Debug("==> Agent keypair generated", "pubkey", agentPubkey)

	senderKeypairPath := "/tmp/target-sender-keypair.json"
	senderPubkey := generateKeypair(t, targetContainerID, senderKeypairPath)
	log.Debug("==> Target-sender keypair generated", "pubkey", senderPubkey)

	// Start agent with the target as an additional target and the sender's pubkey allowed.
	log.Debug("==> Starting geoprobe agent")
	startGeoprobeAgent(t, dn, geoprobeContainerID, agentKeypairPath, geoprobeAccountPK,
		dn.Manager.GeolocationProgramID, dn.Manager.ServiceabilityProgramID,
		&geoprobeAgentOpts{
			additionalTargets: fmt.Sprintf("%s:%d:%d", targetIPStr, 8923, 8925),
			probeInterval:     5 * time.Second,
			allowedPubkeys:    []string{senderPubkey},
		})
	log.Debug("==> Geoprobe agent started")

	// --- Outbound flow ---
	// Wait for dz1's telemetry agent to discover the geoprobe and successfully probe it.
	log.Debug("==> Waiting for geoprobe discovery and successful measurement")
	waitForGeoprobeSuccess(t, dz1, geoprobeIPStr, 180*time.Second)
	log.Debug("==> Geoprobe discovery and measurement verified")

	// Wait for the agent to forward a composite offset to the target.
	log.Debug("==> Waiting for geoprobe target to receive composite offset")
	waitForTargetOffsetReceived(t, targetContainerID, 120*time.Second)
	log.Debug("==> Geoprobe target received valid composite offset")

	// --- Inbound flow ---
	// Run target-sender from the target container to send signed TWAMP probes to the agent.
	// The agent should reply with cached DZD offsets embedded in signed replies.
	log.Debug("==> Running target-sender for inbound probing")
	runTargetSender(t, targetContainerID, geoprobeIPStr, agentPubkey, senderKeypairPath)

	log.Debug("==> Waiting for successful inbound probe with offsets")
	waitForInboundProbeSuccess(t, targetContainerID, 120*time.Second)
	log.Debug("==> Inbound probing verified")
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
func createGeoprobeOnchain(t *testing.T, dn *devnet.Devnet, code, exchangePK, publicIP, signingKeypair string) string {
	t.Helper()
	output, err := dn.Manager.Exec(t.Context(), []string{
		"doublezero-geolocation", "probe", "create",
		"--code", code,
		"--exchange", exchangePK,
		"--public-ip", publicIP,
		"--port", "8923",
		"--signing-keypair", signingKeypair,
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

type geoprobeAgentOpts struct {
	additionalTargets string
	probeInterval     time.Duration
	allowedPubkeys    []string
}

// generateKeypair creates a Solana keypair inside a container and returns its base58 pubkey.
func generateKeypair(t *testing.T, containerID, path string) string {
	t.Helper()

	_, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
		"solana-keygen", "new", "--no-bip39-passphrase", "-o", path, "--force",
	})
	require.NoError(t, err)

	output, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
		"solana-keygen", "pubkey", path,
	})
	require.NoError(t, err)
	pubkey := strings.TrimSpace(string(output))
	require.NotEmpty(t, pubkey)
	return pubkey
}

// startGeoprobeAgent starts the geoprobe-agent with a pre-generated keypair.
func startGeoprobeAgent(t *testing.T, dn *devnet.Devnet, containerID, keypairPath, geoprobeAccountPK, geolocationProgramID, serviceabilityProgramID string, opts *geoprobeAgentOpts) {
	t.Helper()

	args := fmt.Sprintf(
		"nohup doublezero-geoprobe-agent "+
			"-ledger-rpc-url %s "+
			"-keypair %s "+
			"-geoprobe-pubkey %s "+
			"-geolocation-program-id %s "+
			"-serviceability-program-id %s "+
			"-twamp-listen-port 8925 "+
			"-udp-listen-port 8923 "+
			"-verbose",
		dn.Ledger.InternalRPCURL,
		keypairPath,
		geoprobeAccountPK,
		geolocationProgramID,
		serviceabilityProgramID,
	)
	if opts != nil {
		if opts.additionalTargets != "" {
			args += fmt.Sprintf(" -additional-targets %s", opts.additionalTargets)
		}
		if opts.probeInterval > 0 {
			args += fmt.Sprintf(" -probe-interval %s", opts.probeInterval)
		}
		if len(opts.allowedPubkeys) > 0 {
			args += fmt.Sprintf(" -allowed-pubkeys %s", strings.Join(opts.allowedPubkeys, ","))
		}
	}
	cmd := args + " > /tmp/geoprobe-agent.log 2>&1 &"
	_, err := docker.Exec(t.Context(), dockerClient, containerID, []string{"bash", "-c", cmd})
	require.NoError(t, err)

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

// startGeoprobeTargetContainer creates and starts a container for the geoprobe-target,
// connected to both the default and CYOA networks with a specific CYOA IP.
func startGeoprobeTargetContainer(t *testing.T, log *slog.Logger, dn *devnet.Devnet, cyoaIP string) string {
	t.Helper()

	geoprobeImage := os.Getenv("DZ_GEOPROBE_IMAGE")
	require.NotEmpty(t, geoprobeImage, "DZ_GEOPROBE_IMAGE must be set")

	req := testcontainers.ContainerRequest{
		Image: geoprobeImage,
		Name:  dn.Spec.DeployID + "-geoprobe-target",
		ConfigModifier: func(cfg *dockercontainer.Config) {
			cfg.Hostname = "geoprobe-target"
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

	err = dockerClient.NetworkConnect(t.Context(), dn.CYOANetwork.Name, containerID, &network.EndpointSettings{
		IPAddress: cyoaIP,
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv4Address: cyoaIP,
		},
	})
	require.NoError(t, err, "failed to connect geoprobe-target to CYOA network with IP %s", cyoaIP)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if t.Failed() {
			output, err := docker.Exec(ctx, dockerClient, containerID, []string{
				"bash", "-c", "cat /tmp/geoprobe-target.log 2>/dev/null || echo 'no log file'",
			})
			if err == nil {
				fmt.Fprintf(os.Stderr, "\n--- Geoprobe target log ---\n%s\n", string(output))
			}
		}
		_ = container.Terminate(ctx)
	})

	return containerID
}

// startGeoprobeTarget starts the geoprobe-target process inside a container.
func startGeoprobeTarget(t *testing.T, containerID string) {
	t.Helper()

	cmd := "nohup doublezero-geoprobe-target " +
		"-twamp-port 8925 " +
		"-udp-port 8923 " +
		"> /tmp/geoprobe-target.log 2>&1 &"
	_, err := docker.Exec(t.Context(), dockerClient, containerID, []string{"bash", "-c", cmd})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		output, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
			"bash", "-c", "pgrep -f doublezero-geoprobe-target",
		})
		return err == nil && len(strings.TrimSpace(string(output))) > 0
	}, 10*time.Second, 1*time.Second, "geoprobe-target process should be running")
}

// waitForTargetOffsetReceived polls the geoprobe-target log until it shows
// a received LocationOffset with a valid signature chain.
func waitForTargetOffsetReceived(t *testing.T, containerID string, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		output, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
			"bash", "-c", "cat /tmp/geoprobe-target.log 2>/dev/null",
		})
		if err != nil {
			return false
		}
		logStr := string(output)
		return strings.Contains(logStr, "received LocationOffset") &&
			strings.Contains(logStr, "signature_valid=true")
	}, timeout, 5*time.Second, "Expected geoprobe-target to log 'received LocationOffset' with 'signature_valid=true'")
}

// runTargetSender starts geoprobe-target-sender in the target container, sending
// signed TWAMP probes to the agent's reflector. It runs with --count 3 so it
// exits after 3 probe pairs.
func runTargetSender(t *testing.T, containerID, agentIP, agentPubkey, keypairPath string) {
	t.Helper()

	cmd := fmt.Sprintf(
		"nohup doublezero-geoprobe-target-sender "+
			"-probe-ip %s "+
			"-probe-port 8924 "+
			"-probe-pk %s "+
			"-keypair %s "+
			"-count 3 "+
			"-interval 5s "+
			"-log-format json "+
			"-verbose "+
			"> /tmp/target-sender.log 2>&1 &",
		agentIP, agentPubkey, keypairPath,
	)
	_, err := docker.Exec(t.Context(), dockerClient, containerID, []string{"bash", "-c", cmd})
	require.NoError(t, err)

	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		output, err := docker.Exec(ctx, dockerClient, containerID, []string{
			"bash", "-c", "cat /tmp/target-sender.log 2>/dev/null || echo 'no log file'",
		})
		if err == nil {
			fmt.Fprintf(os.Stderr, "\n--- Target-sender log ---\n%s\n", string(output))
		}
	})
}

// waitForInboundProbeSuccess polls the target-sender log for a successful probe pair
// where reply signatures are valid and DZD offset data is present.
func waitForInboundProbeSuccess(t *testing.T, containerID string, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		output, err := docker.Exec(t.Context(), dockerClient, containerID, []string{
			"bash", "-c", "cat /tmp/target-sender.log 2>/dev/null",
		})
		if err != nil {
			return false
		}
		logStr := string(output)

		// Look for a JSON line with valid reply signatures and non-empty offsets.
		for _, line := range strings.Split(logStr, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var result struct {
				Reply1SigValid bool `json:"reply1_sig_valid"`
				Offsets        []struct {
					SigValid bool `json:"sig_valid"`
				} `json:"offsets"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				continue
			}
			if result.Error != "" || !result.Reply1SigValid {
				continue
			}
			if len(result.Offsets) > 0 && result.Offsets[0].SigValid {
				return true
			}
		}
		return false
	}, timeout, 5*time.Second, "Expected target-sender log to contain a successful probe pair with valid signatures and offsets")
}
