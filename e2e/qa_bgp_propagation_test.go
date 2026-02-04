//go:build qa

package e2e

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/require"
)

// TestQA_BGPPropagationTiming measures BGP route propagation time between different device pairs.
// This test helps diagnose why some device combinations have slower route propagation.
//
// Run with:
//   go test -tags=qa -v -timeout=10m ./e2e -run "TestQA_BGPPropagationTiming" --args -hosts=fra-tn-qa01,sgp-tn-qa01 -env=testnet
func TestQA_BGPPropagationTiming(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()

	// This test requires exactly fra and sgp hosts
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	var fraClient, sgpClient *qa.Client
	for _, c := range clients {
		switch {
		case strings.Contains(c.Host, "fra"):
			fraClient = c
		case strings.Contains(c.Host, "sgp"):
			sgpClient = c
		}
	}
	require.NotNil(t, fraClient, "fra client not found - run with -hosts=fra-tn-qa01,sgp-tn-qa01")
	require.NotNil(t, sgpClient, "sgp client not found - run with -hosts=fra-tn-qa01,sgp-tn-qa01")

	sgpIP := sgpClient.PublicIP()
	log.Info("Test setup", "fraHost", fraClient.Host, "sgpHost", sgpClient.Host, "sgpIP", sgpIP)

	// Clean up on exit
	t.Cleanup(func() {
		_ = fraClient.DisconnectUser(context.Background(), false, false)
		_ = sgpClient.DisconnectUser(context.Background(), false, false)
	})

	// Test 1: Healthy device (fra-dz001)
	t.Run("healthy_device_fra-dz001", func(t *testing.T) {
		duration := measureRoutePropagation(t, ctx, log, fraClient, sgpClient, "fra-dz001", "sin-dz001", sgpIP)
		log.Info("Route propagation time with HEALTHY device", "device", "fra-dz001", "duration", duration)
	})

	// Disconnect both before next test
	log.Info("Disconnecting both clients before next test")
	err = fraClient.DisconnectUser(ctx, true, false)
	require.NoError(t, err, "failed to disconnect fra")
	err = sgpClient.DisconnectUser(ctx, true, false)
	require.NoError(t, err, "failed to disconnect sgp")

	// Wait a bit for BGP to settle
	time.Sleep(5 * time.Second)

	// Test 2: Unhealthy device (fra-dz-001-x)
	t.Run("unhealthy_device_fra-dz-001-x", func(t *testing.T) {
		duration := measureRoutePropagation(t, ctx, log, fraClient, sgpClient, "fra-dz-001-x", "sin-dz001", sgpIP)
		log.Info("Route propagation time with UNHEALTHY device", "device", "fra-dz-001-x", "duration", duration)
	})
}

func measureRoutePropagation(t *testing.T, ctx context.Context, log *slog.Logger, fraClient, sgpClient *qa.Client, fraDevice, sgpDevice string, targetIP net.IP) time.Duration {
	// Ensure both are disconnected first
	_ = fraClient.DisconnectUser(ctx, true, false)
	_ = sgpClient.DisconnectUser(ctx, true, false)

	// Connect fra to specified device
	log.Info("Connecting fra", "device", fraDevice)
	err := fraClient.ConnectUserUnicast(ctx, fraDevice, true)
	require.NoError(t, err, "failed to connect fra to %s", fraDevice)

	// Verify fra is connected to the right device
	status, err := fraClient.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get fra status")
	require.Equal(t, fraDevice, status.CurrentDevice, "fra connected to wrong device")
	log.Info("Fra connected", "device", status.CurrentDevice)

	// Verify route doesn't exist yet
	routes, err := fraClient.GetInstalledRoutes(ctx)
	require.NoError(t, err, "failed to get routes")
	for _, r := range routes {
		require.NotEqual(t, targetIP.String(), r.DstIp, "route to sgp already exists before sgp connected")
	}

	// Start timing
	startTime := time.Now()

	// Connect sgp
	log.Info("Connecting sgp", "device", sgpDevice)
	err = sgpClient.ConnectUserUnicast(ctx, sgpDevice, true)
	require.NoError(t, err, "failed to connect sgp to %s", sgpDevice)

	// Start a timer goroutine to show progress
	timerDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timerDone:
				return
			case <-ticker.C:
				elapsed := time.Since(startTime).Round(time.Second)
				log.Info("Still waiting for route...", "elapsed", elapsed, "fraDevice", fraDevice)
			}
		}
	}()

	// Wait for route to appear on fra
	log.Info("Waiting for route to appear on fra", "targetIP", targetIP)
	err = fraClient.WaitForRoutes(ctx, []net.IP{targetIP})
	close(timerDone)

	if err != nil {
		// Log the failure but capture how long we waited
		duration := time.Since(startTime)
		log.Info("Route propagation FAILED/TIMEOUT", "device", fraDevice, "duration", duration, "error", err)
		t.Fatalf("route propagation failed after %v: %v", duration, err)
	}

	duration := time.Since(startTime)
	log.Info("Route appeared", "duration", duration)

	return duration
}

// TestQA_BGPPropagationVariance runs multiple iterations to measure variance in BGP propagation times.
//
// Run with:
//   go test -tags=qa -v -timeout=20m ./e2e -run "TestQA_BGPPropagationVariance" --args -hosts=fra-tn-qa01,sgp-tn-qa01 -env=testnet
func TestQA_BGPPropagationVariance(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()

	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	var fraClient, sgpClient *qa.Client
	for _, c := range clients {
		switch {
		case strings.Contains(c.Host, "fra"):
			fraClient = c
		case strings.Contains(c.Host, "sgp"):
			sgpClient = c
		}
	}
	require.NotNil(t, fraClient, "fra client not found")
	require.NotNil(t, sgpClient, "sgp client not found")

	sgpIP := sgpClient.PublicIP()

	t.Cleanup(func() {
		_ = fraClient.DisconnectUser(context.Background(), false, false)
		_ = sgpClient.DisconnectUser(context.Background(), false, false)
	})

	const iterations = 5
	results := make([]time.Duration, 0, iterations)

	for i := 1; i <= iterations; i++ {
		log.Info("=== ITERATION START ===", "iteration", i, "of", iterations)

		duration := measureRoutePropagationDetailed(t, ctx, log, fraClient, sgpClient, "fra-dz001", "sin-dz001", sgpIP, i)
		results = append(results, duration)

		log.Info("=== ITERATION COMPLETE ===", "iteration", i, "duration", duration)

		// Wait between iterations for BGP to settle
		if i < iterations {
			log.Info("Waiting 10s before next iteration...")
			time.Sleep(10 * time.Second)
		}
	}

	// Summary
	var total time.Duration
	var min, max time.Duration = results[0], results[0]
	for _, d := range results {
		total += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	avg := total / time.Duration(len(results))

	log.Info("=== SUMMARY ===",
		"iterations", iterations,
		"min", min,
		"max", max,
		"avg", avg,
		"results", results,
	)
}

func measureRoutePropagationDetailed(t *testing.T, ctx context.Context, log *slog.Logger, fraClient, sgpClient *qa.Client, fraDevice, sgpDevice string, targetIP net.IP, iteration int) time.Duration {
	// Disconnect both
	log.Info("[1] Disconnecting both clients")
	disconnectStart := time.Now()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = fraClient.DisconnectUser(ctx, true, false)
	}()
	go func() {
		defer wg.Done()
		_ = sgpClient.DisconnectUser(ctx, true, false)
	}()
	wg.Wait()
	log.Info("[1] Disconnect complete", "duration", time.Since(disconnectStart))

	// Connect fra
	log.Info("[2] Connecting fra", "device", fraDevice)
	fraConnectStart := time.Now()
	err := fraClient.ConnectUserUnicast(ctx, fraDevice, true)
	require.NoError(t, err, "failed to connect fra")
	fraConnectDuration := time.Since(fraConnectStart)
	log.Info("[2] Fra connected", "duration", fraConnectDuration)

	// Verify fra device
	status, err := fraClient.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get fra status")
	log.Info("[2] Fra status", "device", status.CurrentDevice, "status", status.SessionStatus)

	// Check initial routes on fra
	routes, err := fraClient.GetInstalledRoutes(ctx)
	require.NoError(t, err, "failed to get routes")
	log.Info("[2] Fra initial routes", "count", len(routes))

	// Connect sgp and start timing
	log.Info("[3] Connecting sgp (BGP propagation timing starts now)", "device", sgpDevice)
	propagationStart := time.Now()

	sgpConnectStart := time.Now()
	err = sgpClient.ConnectUserUnicast(ctx, sgpDevice, true)
	require.NoError(t, err, "failed to connect sgp")
	sgpConnectDuration := time.Since(sgpConnectStart)
	log.Info("[3] Sgp connected", "duration", sgpConnectDuration)

	// Check sgp status
	sgpStatus, err := sgpClient.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get sgp status")
	log.Info("[3] Sgp status", "device", sgpStatus.CurrentDevice, "status", sgpStatus.SessionStatus)

	// Now wait for route on fra with detailed polling
	log.Info("[4] Polling for route on fra", "targetIP", targetIP)

	pollStart := time.Now()
	pollCount := 0
	timerDone := make(chan struct{})

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timerDone:
				return
			case <-ticker.C:
				elapsed := time.Since(pollStart).Round(time.Second)
				log.Info("[4] Still polling...", "elapsed", elapsed, "pollCount", pollCount)
			}
		}
	}()

	// Custom polling to count iterations
	for {
		pollCount++
		routes, err := fraClient.GetInstalledRoutes(ctx)
		if err != nil {
			close(timerDone)
			t.Fatalf("failed to get routes: %v", err)
		}

		for _, r := range routes {
			if r.DstIp == targetIP.String() {
				close(timerDone)
				pollDuration := time.Since(pollStart)
				totalPropagation := time.Since(propagationStart)

				log.Info("[4] Route appeared!",
					"pollDuration", pollDuration,
					"pollCount", pollCount,
					"totalPropagation", totalPropagation,
					"sgpConnectDuration", sgpConnectDuration,
					"routeWaitAfterSgpUp", pollDuration,
				)

				return totalPropagation
			}
		}

		time.Sleep(1 * time.Second)

		// Timeout after 120s
		if time.Since(pollStart) > 120*time.Second {
			close(timerDone)
			t.Fatalf("route did not appear after 120s, pollCount=%d", pollCount)
		}
	}
}

