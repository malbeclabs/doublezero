//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	devicesFlag           = flag.String("devices", "", "comma separated list of devices to run tests against")
	allocateAddrHosts     = flag.String("allocate-addr-hosts", "", "comma separated list of hosts that will have `--allocate-addr` passed to `doublezero connect ibrl`")
	skipCapacityCheckFlag = flag.Bool("skip-capacity-check", false, "skip device capacity checks (use when running with QA identity that bypasses on-chain max_users)")
)

func TestQA_AllDevices_UnicastConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-devices test in short mode")
	}

	startTime := time.Now()
	log := newTestLogger(t)
	ctx := t.Context()

	// Record which clients should use allocate-addr
	allocateAddrHostsSet := make(map[string]struct{})
	if *allocateAddrHosts != "" {
		for _, host := range strings.Split(*allocateAddrHosts, ",") {
			allocateAddrHostsSet[strings.TrimSpace(host)] = struct{}{}
		}
	}

	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, allocateAddrHostsSet)
	require.NoError(t, err, "failed to create test")

	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 2, "At least 2 clients are required for connectivity testing")

	// Filter devices to only include those with sufficient capacity and skip test devices
	// When using a QA identity (--skip-capacity-check), all devices are included regardless of capacity
	devices := test.ValidDevices(2, *skipCapacityCheckFlag)
	if len(devices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
	}

	// Filter out transit devices - they don't participate in unicast connectivity tests
	devices = slices.DeleteFunc(devices, func(d *qa.Device) bool {
		if d.DeviceType == serviceability.DeviceDeviceTypeTransit {
			log.Info("Skipping transit device", "device", d.Code)
			return true
		}
		return false
	})

	// Filter out devices that are not actively calling the controller
	if grafanaCfg := qa.GrafanaConfigFromEnv(); grafanaCfg != nil {
		activeDevices, err := qa.GetDevicesWithActiveConfigAgents(ctx, grafanaCfg)
		if err != nil {
			log.Warn("Failed to query Grafana for active devices, proceeding with all devices", "error", err)
		} else {
			log.Info("Filtering devices by controller activity", "activeDeviceCount", len(activeDevices))
			devices = slices.DeleteFunc(devices, func(d *qa.Device) bool {
				if !activeDevices[d.Code] {
					log.Info("Skipping device not calling controller", "device", d.Code)
					return true
				}
				return false
			})
		}
	} else {
		log.Info("No Grafana config found, including all devices regardless of controller activity")
	}

	// If devices flag is provided, filter devices to only include those in the list.
	if *devicesFlag != "" {
		deviceCodes := make(map[string]struct{})
		for _, code := range strings.Split(*devicesFlag, ",") {
			deviceCodes[strings.TrimSpace(code)] = struct{}{}
		}
		devices = slices.DeleteFunc(devices, func(d *qa.Device) bool {
			_, ok := deviceCodes[d.Code]
			return !ok
		})
	}

	log.Info("Collect `doublezero latency` for each client")
	clientLatencies := make(qa.ClientLatencies)
	for _, client := range clients {
		latencies, err := client.GetLatency(ctx)
		require.NoError(t, err, "failed to get latency from client %s", client.Host)

		clientLatencies[client.Host] = make(map[string]float64)
		for _, l := range latencies {
			if l.Reachable {
				clientLatencies[client.Host][l.DeviceCode] = float64(l.AvgLatencyNs) / 1_000_000.0 // ns -> ms
			}
		}
	}

	log.Info("Create a list of devices for each client.")
	log.Info(fmt.Sprintf("    (If there are multiple clients with <%dms latency for that device, assign the device to the client with the fewest devices", qa.LatencyThresholdMs))
	log.Info("    Otherwise, associate each device with the client with the lowest latency)")

	log.Info("Assign devices to clients based on latency")
	batchData := qa.AssignDevicesToClients(devices, clients, clientLatencies, allocateAddrHostsSet, test.ShuffleDevices)

	batchCount := len(batchData)
	if batchCount == 0 {
		t.Skip("No devices assigned to any client")
	}

	log.Info("Client device assignments:")
	printTestReportTable(log, batchData, clientLatencies, false)

	log.Info("Planning to test",
		"deviceCount", len(devices),
		"clientCount", len(clients),
		"totalBatches", batchCount,
	)

	var resultsMu sync.Mutex

	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, client := range clients {
			wg.Add(1)
			go func(client *qa.Client) {
				defer wg.Done()
				err := client.DisconnectUser(context.Background(), true, true)
				assert.NoError(t, err, "failed to disconnect user")
			}(client)
		}
		wg.Wait()
	})

	for batchNum := 0; batchNum < batchCount; batchNum++ {
		batch := batchData[batchNum]

		// DetermineClientsToConnect identifies clients that need to reconnect for this batch.
		// Clients keep their connection if assigned to the same device as the previous batch
		// and still have status "up". This avoids unnecessary reconnection overhead.
		getStatus := func(hostname string) (string, error) {
			for _, c := range clients {
				if c.Host == hostname {
					status, err := c.GetUserStatus(ctx)
					if err != nil {
						return "", err
					}
					return status.SessionStatus, nil
				}
			}
			return "", fmt.Errorf("client %s not found", hostname)
		}
		clientsToConnect := qa.DetermineClientsToConnect(batchNum, batchData, clients, getStatus)

		t.Run(fmt.Sprintf("batch_%d", batchNum+1), func(t *testing.T) {
			connectedClients := connectClientsAndWaitForRoutes(t, ctx, log, clientsToConnect, clients, batch)
			if len(connectedClients) < 2 {
				t.Fatalf("fewer than 2 clients connected (%d), cannot run connectivity tests", len(connectedClients))
			}
			runConnectivitySubtests(t, log, connectedClients, batch, &resultsMu)
		})

		printTestReportTable(log, qa.BatchData{batchNum: batch}, clientLatencies, true)
	}

	log.Info("Test results:")
	printTestReportTable(log, batchData, clientLatencies, true)

	var totalSent, totalReceived uint32
	batchesWithLoss := 0
	deviceResults := make(map[string]*qa.DeviceTestResult)
	for _, batch := range batchData {
		for _, assignment := range batch {
			totalSent += assignment.PacketsSent
			totalReceived += assignment.PacketsReceived
			if assignment.PacketsReceived < assignment.PacketsSent {
				batchesWithLoss++
			}

			if _, seen := deviceResults[assignment.Device.Code]; !seen {
				deviceResults[assignment.Device.Code] = &qa.DeviceTestResult{
					DeviceCode:   assignment.Device.Code,
					DevicePubkey: assignment.Device.PubKey,
					Success:      true,
				}
			}
			if !assignment.Success() {
				deviceResults[assignment.Device.Code].Success = false
			}
		}
	}
	log.Info("Test summary", "packetsReceived", totalReceived, "packetsSent", totalSent, "batchesWithLoss", batchesWithLoss, "totalBatches", batchCount)

	results := make([]qa.DeviceTestResult, 0, len(deviceResults))
	for _, result := range deviceResults {
		results = append(results, *result)
	}
	if err := qa.PublishMetrics(ctx, log, qa.MetricsConfigFromEnv(), envArg, results, time.Since(startTime)); err != nil {
		log.Error("Failed to publish metrics", "error", err)
	}
}

// Print a table of each client's device assignments and latencies, plus test results if showResults is true.
func printTestReportTable(log *slog.Logger, batchData qa.BatchData, clientLatencies qa.ClientLatencies, showResults bool) {
	batchNums := slices.Sorted(maps.Keys(batchData))
	if len(batchNums) == 0 {
		return
	}

	clientNames := slices.Sorted(maps.Keys(batchData[batchNums[0]]))
	colWidths := make(map[string]int)
	for _, clientName := range clientNames {
		colWidths[clientName] = len(clientName)
		for _, batchNum := range batchNums {
			if assignment, ok := batchData[batchNum][clientName]; ok {
				latencyMs := clientLatencies[clientName][assignment.Device.Code]
				var cell string
				if showResults {
					if assignment.Success() {
						cell = fmt.Sprintf("%s %d/%d ✅", assignment.Device.Code, assignment.PacketsReceived, assignment.PacketsSent)
					} else {
						cell = fmt.Sprintf("%s %d/%d ❌", assignment.Device.Code, assignment.PacketsReceived, assignment.PacketsSent)
					}
				} else {
					cell = fmt.Sprintf("%s (%.1fms) ⏳", assignment.Device.Code, latencyMs)
				}
				colWidths[clientName] = max(colWidths[clientName], len(cell))
			}
		}
	}

	batchColWidth := 1
	// Calculate batch column width based on max batch number.
	batchColWidth = len(fmt.Sprintf("Batch %d", batchNums[len(batchNums)-1]+1))

	var sb strings.Builder
	sb.WriteString("\n")

	// Header row
	sb.WriteString(fmt.Sprintf("| %-*s |", batchColWidth, ""))
	for _, clientName := range clientNames {
		sb.WriteString(fmt.Sprintf(" %-*s |", colWidths[clientName], clientName))
	}
	sb.WriteString("\n")

	// Separator row
	sb.WriteString("|")
	sb.WriteString(strings.Repeat("-", batchColWidth+2))
	sb.WriteString("|")
	for _, clientName := range clientNames {
		sb.WriteString(strings.Repeat("-", colWidths[clientName]+2))
		sb.WriteString("|")
	}
	sb.WriteString("\n")

	// Data rows
	for _, batchNum := range batchNums {
		sb.WriteString(fmt.Sprintf("| Batch %-*d |", batchColWidth-6, batchNum+1))
		for _, clientName := range clientNames {
			cell := ""
			if assignment, ok := batchData[batchNum][clientName]; ok {
				latencyMs := clientLatencies[clientName][assignment.Device.Code]
				if showResults {
					if assignment.Success() {
						cell = fmt.Sprintf("%s %d/%d ✅", assignment.Device.Code, assignment.PacketsReceived, assignment.PacketsSent)
					} else {
						cell = fmt.Sprintf("%s %d/%d ❌", assignment.Device.Code, assignment.PacketsReceived, assignment.PacketsSent)
					}
				} else {
					cell = fmt.Sprintf("%s (%.1fms) 🕒", assignment.Device.Code, latencyMs)
				}
			}
			sb.WriteString(fmt.Sprintf(" %-*s |", colWidths[clientName]-1, cell))
		}
		sb.WriteString("\n")
	}

	log.Info(sb.String())
}

func connectClientsAndWaitForRoutes(
	t *testing.T,
	ctx context.Context,
	log *slog.Logger,
	clientsToConnect []*qa.Client,
	allClients []*qa.Client,
	batch map[string]*qa.BatchResult,
) []*qa.Client {
	// Connect only clients whose device changed from previous batch
	// Note: errors here must increment FailedTests so Success() returns false
	for _, c := range clientsToConnect {
		device := batch[c.Host].Device
		err := c.ConnectUserUnicast_NoWait(ctx, device.Code)
		if err != nil {
			log.Error("Failed to start connection", "client", c.Host, "device", device.Code, "error", err)
			batch[c.Host].FailedTests++
			if device.Status == serviceability.DeviceStatusActivated && device.MaxUsers > 0 {
				t.Errorf("failed to connect client %s to device %s: %v", c.Host, device.Code, err)
			} else {
				log.Warn("Ignoring connection failure for device not ready for users", "device", device.Code, "status", device.Status, "maxUsers", device.MaxUsers)
			}
		}
	}

	// Wait for status up for clients that reconnected
	for _, c := range clientsToConnect {
		device := batch[c.Host].Device
		err := c.WaitForStatusUp(ctx)
		if err != nil {
			log.Error("Client failed to reach status up", "client", c.Host, "error", err)
			batch[c.Host].FailedTests++
			if device.Status == serviceability.DeviceStatusActivated && device.MaxUsers > 0 {
				t.Errorf("failed to wait for status for client %s: %v", c.Host, err)
			} else {
				log.Warn("Ignoring status failure for device not ready for users", "device", device.Code, "status", device.Status, "maxUsers", device.MaxUsers)
			}
		}
	}

	// Build list of clients with status "up" (both newly connected and previously connected)
	statuses := make(map[string]string)
	for _, c := range allClients {
		if _, ok := batch[c.Host]; !ok {
			continue
		}
		status, err := c.GetUserStatus(ctx)
		if err != nil {
			log.Error("Failed to get user status", "client", c.Host, "error", err)
			continue
		}
		statuses[c.Host] = status.SessionStatus
		if !qa.IsStatusUp(status.SessionStatus) {
			log.Warn("Client not up", "client", c.Host, "status", status.SessionStatus)
		}
	}
	connectedClients := qa.FilterStatusUpClients(allClients, batch, statuses)

	// Wait for routes between all connected clients
	for _, c := range connectedClients {
		device := batch[c.Host].Device
		targets := qa.ComputeRouteTargets(c, connectedClients, batch, func(client *qa.Client) net.IP {
			return client.DoublezeroOrPublicIP()
		})
		if err := c.WaitForRoutes(ctx, targets); err != nil {
			log.Error("Failed to wait for routes", "client", c.Host, "error", err)
			batch[c.Host].FailedTests++
			if device.Status == serviceability.DeviceStatusActivated && device.MaxUsers > 0 {
				t.Errorf("failed to wait for routes on client %s: %v", c.Host, err)
			} else {
				log.Warn("Ignoring route failure for device not ready for users", "device", device.Code, "status", device.Status, "maxUsers", device.MaxUsers)
			}
		}
	}

	return connectedClients
}

func runConnectivitySubtests(
	t *testing.T,
	outerLog *slog.Logger,
	clients []*qa.Client,
	batch map[string]*qa.BatchResult,
	resultsMu *sync.Mutex,
) {
	for _, client := range clients {
		device := batch[client.Host].Device
		srcClient := client

		t.Run(fmt.Sprintf("device_%s__from_%s", device.Code, srcClient.Host), func(t *testing.T) {
			t.Parallel()

			log := newTestLogger(t)
			srcClient.SetLogger(log)
			t.Cleanup(func() {
				srcClient.SetLogger(outerLog)
			})
			subCtx := t.Context()

			var totalSent, totalReceived, failedTests uint32
			var wg sync.WaitGroup
			var mu sync.Mutex
			for _, target := range clients {
				if target.Host == srcClient.Host {
					continue
				}

				wg.Add(1)
				go func(src, target *qa.Client, srcDevice, dstDevice *qa.Device) {
					defer wg.Done()
					result, err := src.TestUnicastConnectivity(t, subCtx, target, srcDevice, dstDevice)
					if err != nil {
						log.Error("Connectivity test failed", "error", err, "source", src.Host, "target", target.Host, "sourceDevice", srcDevice.Code, "targetDevice", dstDevice.Code)
						mu.Lock()
						failedTests++
						mu.Unlock()
						// Only fail test if both devices are activated with max_users > 0
						srcReady := srcDevice.Status == serviceability.DeviceStatusActivated && srcDevice.MaxUsers > 0
						dstReady := dstDevice.Status == serviceability.DeviceStatusActivated && dstDevice.MaxUsers > 0
						if srcReady && dstReady {
							assert.NoError(t, err, "failed to test connectivity")
						} else {
							log.Warn("Ignoring connectivity failure involving device not ready for users",
								"sourceDevice", srcDevice.Code, "sourceStatus", srcDevice.Status, "sourceMaxUsers", srcDevice.MaxUsers,
								"targetDevice", dstDevice.Code, "targetStatus", dstDevice.Status, "targetMaxUsers", dstDevice.MaxUsers)
						}
					}
					if result != nil {
						mu.Lock()
						totalSent += result.PacketsSent
						totalReceived += result.PacketsReceived
						mu.Unlock()
					}
				}(srcClient, target, batch[srcClient.Host].Device, batch[target.Host].Device)
			}
			wg.Wait()

			resultsMu.Lock()
			batch[srcClient.Host].PacketsSent += totalSent
			batch[srcClient.Host].PacketsReceived += totalReceived
			batch[srcClient.Host].FailedTests += failedTests
			resultsMu.Unlock()
		})
	}
}
