//go:build e2e

package e2e_test

// TestE2E_Tribble stress tests the system by spawning many client containers
// (tribbles) that all connect to a single device container. This test is designed
// to find the breaking point where the system starts to fail under load.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/stretchr/testify/require"
)

func TestE2E_Tribble(t *testing.T) {
	t.Parallel()

	const (
		numClients          = 150 // Start small to debug, increase later
		clientBatchSize     = 10  // Smaller batches for initial testing
		batchDelay          = 3 * time.Second
		connectionTimeout   = 3 * time.Minute
		latencyCheckTimeout = 5 * time.Minute
	)

	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

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

	log.Info("==> Starting devnet for tribble stress test")
	err = dn.Start(t.Context(), nil)
	require.NoError(t, err)
	log.Info("--> Devnet started")

	// Give the devnet a moment to stabilize
	time.Sleep(5 * time.Second)

	deviceCode := "tribble-dz01"
	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         deviceCode,
		Location:                     "lax",
		Exchange:                     "xlax",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 24,
		LoopbackInterfaces: map[string]string{
			"Loopback255": "vpnv4",
			"Loopback256": "ipv4",
		},
	})
	require.NoError(t, err)
	devicePK := device.ID
	log.Info("--> Device added", "deviceCode", deviceCode, "devicePK", devicePK)

	log.Info("==> Waiting for device to exist onchain")
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		return len(data.Devices) == 1
	}, 30*time.Second, 1*time.Second)
	log.Info("--> Device exists onchain")

	type clientMetrics struct {
		client           *devnet.Client
		createdAt        time.Time
		connectedAt      time.Time
		latencyCheckedAt time.Time
		connectionError  error
		latencyError     error
		allowlistError   error
	}

	clients := make([]*clientMetrics, 0, numClients)
	clientsMu := sync.Mutex{}

	var createdCount atomic.Int32
	var failedCount atomic.Int32

	log.Info("==> Starting tribble multiplication",
		slog.Int("totalClients", numClients),
		slog.Int("batchSize", clientBatchSize))

	for batch := 0; batch < (numClients+clientBatchSize-1)/clientBatchSize; batch++ {
		batchStart := batch * clientBatchSize
		batchEnd := batchStart + clientBatchSize
		if batchEnd > numClients {
			batchEnd = numClients
		}

		log.Info(fmt.Sprintf("==> Spawning batch %d/%d (clients %d-%d)",
			batch+1, (numClients+clientBatchSize-1)/clientBatchSize,
			batchStart+1, batchEnd))

		// Create clients serially within each batch to avoid container creation race condition
		for i := batchStart; i < batchEnd; i++ {
			clientIdx := i

			metrics := &clientMetrics{
				createdAt: time.Now(),
			}

			client, err := dn.AddClient(t.Context(), devnet.ClientSpec{
				CYOANetworkIPHostID: uint32(100 + clientIdx),
			})

			if err != nil {
				log.Error("Failed to create client",
					slog.Int("clientIdx", clientIdx+1),
					slog.String("error", err.Error()))
				metrics.connectionError = err
				failedCount.Add(1)
			} else {
				metrics.client = client
				createdCount.Add(1)
				log.Debug(fmt.Sprintf("Client %d created", clientIdx+1),
					slog.String("pubkey", client.Pubkey),
					slog.String("ip", client.CYOANetworkIP))
			}

			clientsMu.Lock()
			clients = append(clients, metrics)
			clientsMu.Unlock()
		}

		currentCreated := createdCount.Load()
		currentFailed := failedCount.Load()
		log.Info(fmt.Sprintf("Batch %d complete", batch+1),
			slog.Int("totalCreated", int(currentCreated)),
			slog.Int("totalFailed", int(currentFailed)))

		if batch < (numClients+clientBatchSize-1)/clientBatchSize-1 {
			log.Debug(fmt.Sprintf("Waiting %v before next batch", batchDelay))
			time.Sleep(batchDelay)
		}
	}

	successfulClients := int(createdCount.Load())
	log.Info("==> Client creation phase complete",
		slog.Int("successful", successfulClients),
		slog.Int("failed", int(failedCount.Load())))

	log.Info("==> Adding clients to user allowlist")
	var allowlistCount atomic.Int32
	var allowlistFailCount atomic.Int32

	var allowlistWg sync.WaitGroup
	for i, m := range clients {
		if m.client == nil {
			continue
		}

		allowlistWg.Add(1)
		go func(idx int, metrics *clientMetrics) {
			defer allowlistWg.Done()

			_, err := dn.Manager.Exec(t.Context(), []string{
				"doublezero", "user", "allowlist", "add", "--pubkey", metrics.client.Pubkey,
			})
			if err != nil {
				metrics.allowlistError = err
				allowlistFailCount.Add(1)
				log.Error("Failed to add client to allowlist",
					slog.Int("clientIdx", idx+1),
					slog.String("error", err.Error()))
			} else {
				allowlistCount.Add(1)
			}
		}(i, m)
	}
	allowlistWg.Wait()

	log.Info("--> Allowlist phase complete",
		slog.Int("successful", int(allowlistCount.Load())),
		slog.Int("failed", int(allowlistFailCount.Load())))

	// Set access passes for all clients
	log.Info("==> Setting access passes for clients")
	for i, m := range clients {
		if m.client == nil || m.allowlistError != nil {
			continue
		}

		_, err := dn.Manager.Exec(t.Context(), []string{
			"bash", "-c",
			fmt.Sprintf("doublezero access-pass set --accesspass-type Prepaid --client-ip %s --user-payer %s --last-access-epoch 99999",
				m.client.CYOANetworkIP, m.client.Pubkey),
		})
		if err != nil {
			log.Error("Failed to set access pass for client",
				slog.Int("clientIdx", i+1),
				slog.String("error", err.Error()))
		}
	}
	log.Info("--> Access passes set")

	log.Info("==> Initiating tribble swarm connections")
	var connectCount atomic.Int32
	var connectFailCount atomic.Int32

	// Connect clients serially to avoid overwhelming the system
	for i, m := range clients {
		if m.client == nil || m.allowlistError != nil {
			continue
		}

		clientIdx := i
		metrics := m

		ctx, cancel := context.WithTimeout(t.Context(), connectionTimeout)
		defer cancel()

		_, err := metrics.client.Exec(ctx, []string{
			"doublezero", "connect", "ibrl",
			"--client-ip", metrics.client.CYOANetworkIP,
		})

		if err != nil {
			metrics.connectionError = err
			connectFailCount.Add(1)
			log.Error("Failed to connect client",
				slog.Int("clientIdx", clientIdx+1),
				slog.String("error", err.Error()))
		} else {
			metrics.connectedAt = time.Now()
			connectCount.Add(1)
			log.Info(fmt.Sprintf("Client %d connected successfully", clientIdx+1),
				slog.Duration("connectionTime", metrics.connectedAt.Sub(metrics.createdAt)))
		}

		// Small delay between connections to avoid overwhelming the system
		time.Sleep(500 * time.Millisecond)
	}

	log.Info("==> Connection phase complete",
		slog.Int("successful", int(connectCount.Load())),
		slog.Int("failed", int(connectFailCount.Load())))

	log.Info("==> Checking latency for connected tribbles")
	var latencyCount atomic.Int32
	var latencyFailCount atomic.Int32
	var latencyWg sync.WaitGroup

	for i, m := range clients {
		if m.client == nil || m.connectionError != nil {
			continue
		}

		latencyWg.Add(1)
		clientIdx := i
		metrics := m

		go func() {
			defer latencyWg.Done()

			ctx, cancel := context.WithTimeout(t.Context(), latencyCheckTimeout)
			defer cancel()

			err := metrics.client.WaitForLatencyResults(ctx, devicePK, 60*time.Second)
			if err != nil {
				metrics.latencyError = err
				latencyFailCount.Add(1)
				log.Error("Latency check failed",
					slog.Int("clientIdx", clientIdx+1),
					slog.String("error", err.Error()))
			} else {
				metrics.latencyCheckedAt = time.Now()
				latencyCount.Add(1)
			}
		}()
	}

	latencyWg.Wait()

	var totalConnectionTime time.Duration
	connectionCount := 0
	var minConnectionTime, maxConnectionTime time.Duration

	for _, m := range clients {
		if m.client != nil && !m.connectedAt.IsZero() {
			connTime := m.connectedAt.Sub(m.createdAt)
			totalConnectionTime += connTime
			connectionCount++

			if minConnectionTime == 0 || connTime < minConnectionTime {
				minConnectionTime = connTime
			}
			if connTime > maxConnectionTime {
				maxConnectionTime = connTime
			}
		}
	}

	avgConnectionTime := time.Duration(0)
	if connectionCount > 0 {
		avgConnectionTime = totalConnectionTime / time.Duration(connectionCount)
	}

	finalSuccessful := int(latencyCount.Load())

	log.Info("=======================================================")
	log.Info("TRIBBLE STRESS TEST RESULTS")
	log.Info("=======================================================")
	log.Info(fmt.Sprintf("Configuration: %d tribbles, batch size %d", numClients, clientBatchSize))
	log.Info(fmt.Sprintf("Client Creation: %d successful, %d failed", successfulClients, int(failedCount.Load())))
	log.Info(fmt.Sprintf("Allowlist: %d successful, %d failed", int(allowlistCount.Load()), int(allowlistFailCount.Load())))
	log.Info(fmt.Sprintf("Connections: %d successful, %d failed", int(connectCount.Load()), int(connectFailCount.Load())))
	log.Info(fmt.Sprintf("Latency Checks: %d successful, %d failed", finalSuccessful, int(latencyFailCount.Load())))
	log.Info(fmt.Sprintf("Connection Times: avg=%v, min=%v, max=%v", avgConnectionTime, minConnectionTime, maxConnectionTime))
	log.Info(fmt.Sprintf("Overall Success Rate: %.1f%%", float64(finalSuccessful)*100/float64(numClients)))
	log.Info("=======================================================")

	minSuccessRate := 0.8
	actualSuccessRate := float64(finalSuccessful) / float64(numClients)
	require.GreaterOrEqual(t, actualSuccessRate, minSuccessRate,
		"Too many tribbles failed. Success rate: %.2f%% (minimum: %.2f%%)",
		actualSuccessRate*100, minSuccessRate*100)
}
