//go:build e2e && stress

package e2e_test

// TestE2E_DeviceStress stress tests the system by spawning many client containers
// that all connect to a single device container.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

type DeviceStressConfig struct {
	NumClients int
}

// clientMetrics tracks metrics for a single client
type clientMetrics struct {
	client      *devnet.Client
	createdAt   time.Time
	connectedAt time.Time
}

func TestE2E_DeviceStress(t *testing.T) {
	t.Parallel()

	// Skip individual client airdrops to avoid rate limits
	os.Setenv("SKIP_CLIENT_AIRDROP", "true")
	defer os.Unsetenv("SKIP_CLIENT_AIRDROP")

	config := DeviceStressConfig{
		NumClients: 4,
	}

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := newTestLoggerForTest(t)

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

	log.Debug("==> Starting devnet")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)

	// Fund manager account
	fundManagerAccount(t, dn, config.NumClients, log)

	// Set up the device
	device := setupDevice(t, dn, log)

	// Wait for device to exist onchain and be activated
	log.Debug("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		// Check that device exists and is activated
		if len(data.Devices) == 0 {
			return false
		}
		// Check device is actually activated (not just created)
		for _, d := range data.Devices {
			if d.Code == device.Spec.Code && d.Status == serviceability.DeviceStatusActivated {
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second)
	log.Debug("--> Device exists and is activated onchain", "deviceCode", device.Spec.Code)

	// Add second device in different exchange to allow client-to-client communication
	log.Debug("==> Adding second device")
	device2Spec := devnet.DeviceSpec{
		ContainerImage:      os.Getenv("DZ_DEVICE_IMAGE"),
		Code:                "dz2-" + random.ShortID(),
		Location:            "lax",
		Exchange:            "xlax",
		CYOANetworkIPHostID: 3,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
	}

	device2, err := dn.AddDevice(t.Context(), device2Spec)
	require.NoError(t, err)

	log.Debug("--> Device2 setup complete", "deviceID", device2.ID)

	// Wait for both devices to be activated
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		if len(data.Devices) != 2 {
			return false
		}
		activatedCount := 0
		for _, d := range data.Devices {
			if (d.Code == device.Spec.Code || d.Code == device2.Spec.Code) && d.Status == serviceability.DeviceStatusActivated {
				activatedCount++
			}
		}
		return activatedCount == 2
	}, 60*time.Second, 2*time.Second)
	log.Debug("--> Both devices are activated onchain")

	// Give it a moment for controller to pick up the change
	time.Sleep(5 * time.Second)

	// Process clients sequentially - complete all steps for each client before moving to next
	// Alternate clients between the two devices to allow communication across exchanges
	clients := make([]*clientMetrics, 0, config.NumClients)
	devices := []*devnet.Device{device, device2}

	for i := 0; i < config.NumClients; i++ {
		log.Debug(fmt.Sprintf("\n=== Processing client %d/%d ===", i+1, config.NumClients))

		// Create client
		log.Debug(fmt.Sprintf("Creating client %d", i+1))
		keypairPath := fmt.Sprintf("/tmp/device-stress-client-%d.json", i)

		// Generate keypair file
		keypairJSON, err := solana.GenerateKeypairJSON()
		require.NoError(t, err)
		err = os.WriteFile(keypairPath, keypairJSON, 0600)
		require.NoError(t, err)

		clientSpec := devnet.ClientSpec{
			ContainerImage:      os.Getenv("DZ_CLIENT_IMAGE"),
			KeypairPath:         keypairPath,
			CYOANetworkIPHostID: uint32(100 + i),
		}

		client, err := dn.AddClient(t.Context(), clientSpec)
		require.NoError(t, err)

		cm := &clientMetrics{
			client:    client,
			createdAt: time.Now(),
		}
		clients = append(clients, cm)

		log.Debug(fmt.Sprintf("--> Created client %d", i+1),
			"pubkey", client.Pubkey, "cyoaIP", client.CYOANetworkIP)

		// Set access pass
		cmd := fmt.Sprintf("doublezero access-pass set --accesspass-type prepaid --client-ip %s --user-payer %s --last-access-epoch 99999",
			client.CYOANetworkIP, client.Pubkey)
		log.Debug(fmt.Sprintf("Setting access pass for client %d with command '%s'", i+1, cmd))
		cmdOutput, err2 := dn.Manager.Exec(t.Context(), []string{
			"bash", "-c",
			cmd,
		})
		log.Debug("Set access pass output", "output", string(cmdOutput))
		require.NoError(t, err2)

		// Alternate clients between devices to enable cross-exchange communication
		selectedDevice := devices[i%2]
		log.Debug(fmt.Sprintf("Client %d will connect to device %s (exchange: %s)", i+1, selectedDevice.Spec.Code, selectedDevice.Spec.Exchange))
		connectClientWithRetry(t, i, client, selectedDevice, log)

		cm.connectedAt = time.Now()
		log.Debug(fmt.Sprintf("✅ Client %d connected successfully to device %s", i+1, selectedDevice.Spec.Code))

		err = client.WaitForTunnelUp(t.Context(), 60*time.Second)
		require.NoError(t, err)

		// Ping test
		log.Debug(fmt.Sprintf("Testing connectivity from client %d to other clients with fping", i+1))
		if i >= 2 {
			cmdOutput, err = client.Exec(t.Context(), []string{
				"bash", "-c", "ip --json r list dev doublezero0 proto bgp  | jq -r '.[].dst' | xargs fping -I doublezero0 2>&1",
			})
			if err != nil {
				log.Error("Failed to ping all other clients with error ", "error", err.Error(), "output", string(cmdOutput))
			}
			require.NoError(t, err)
		}
	}
}

func connectClientWithRetry(t *testing.T, i int, client *devnet.Client, device *devnet.Device, log *slog.Logger) {
	log.Debug(fmt.Sprintf("Connecting client %d to device %s", i+1, device.Spec.Code))

	var output []byte
	var err error
	startTime := time.Now()
	maxDuration := 3 * time.Minute
	retryInterval := 10 * time.Second

	for {
		// Add timeout to prevent hanging forever
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		output, err = client.Exec(ctx, []string{
			"doublezero", "connect", "ibrl", "-v", "--client-ip", client.CYOANetworkIP, "--device", device.Spec.Code,
		})
		cancel() // Cancel immediately after use

		if err == nil {
			break
		}

		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn("Client connection timed out after 60s",
				"clientIdx", i+1,
				"pubkey", client.Pubkey,
				"cyoaIP", client.CYOANetworkIP)
			err = fmt.Errorf("connection timed out after 60s")
		}

		elapsed := time.Since(startTime)
		if elapsed >= maxDuration {
			log.Error("Failed to connect client after max retries",
				"clientIdx", i+1,
				"pubkey", client.Pubkey,
				"cyoaIP", client.CYOANetworkIP,
				"error", err.Error(),
				"output", string(output),
				"elapsed", elapsed)
			require.NoError(t, err)
		}

		log.Warn("Client connection failed, retrying",
			"clientIdx", i+1,
			"pubkey", client.Pubkey,
			"cyoaIP", client.CYOANetworkIP,
			"error", err.Error(),
			"output", string(output),
			"elapsed", elapsed,
			"nextRetryIn", retryInterval)

		time.Sleep(retryInterval)
	}
}

// fundManagerAccount funds the manager account with enough SOL for all clients
func fundManagerAccount(t *testing.T, dn *devnet.Devnet, numClients int, log *slog.Logger) {
	log.Debug("==> Funding manager account", "numClients", numClients)
	requiredSOL := numClients + 50 // Extra buffer

	_, err := dn.Manager.Exec(t.Context(), []string{
		"solana", "airdrop", fmt.Sprintf("%d", requiredSOL),
	})
	if err != nil {
		log.Warn("Failed to airdrop full amount, trying smaller amount",
			"error", err.Error())
		// Try a smaller amount as fallback
		_, err = dn.Manager.Exec(t.Context(), []string{"solana", "airdrop", "100"})
		require.NoError(t, err)
	}
	log.Debug("--> Manager account funded")
}

// setupDevice creates and configures a device for the test
func setupDevice(t *testing.T, dn *devnet.Devnet, log *slog.Logger) *devnet.Device {
	log.Debug("==> Setting up device")

	deviceSpec := devnet.DeviceSpec{
		ContainerImage:      os.Getenv("DZ_DEVICE_IMAGE"),
		Code:                "dz-" + random.ShortID(),
		Location:            "ewr",
		Exchange:            "xewr",
		CYOANetworkIPHostID: 2,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
	}

	device, err := dn.AddDevice(t.Context(), deviceSpec)
	require.NoError(t, err)

	log.Debug("--> Device setup complete", "deviceID", device.ID)
	return device
}
