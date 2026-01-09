//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	devicesFlag       = flag.String("devices", "", "comma separated list of devices to run tests against")
	allocateAddrHosts = flag.String("allocate-addr-hosts", "", "comma separated list of hosts that will have `--allocate-addr` passed to `doublezero connect ibrl`")
)

const latencyThresholdMs = 50

type ClientLatencies map[string]map[string]float64

type BatchResult struct {
	Device          *qa.Device
	PacketsSent     uint32
	PacketsReceived uint32
	FailedTests     uint32
}

func (b *BatchResult) Success() bool {
	return b.FailedTests == 0 && b.PacketsSent > 0 && b.PacketsReceived > 0
}

type BatchData map[int]map[string]*BatchResult

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
	devices := test.ValidDevices(2)
	if len(devices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
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
	clientLatencies := make(ClientLatencies)
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
	log.Info(fmt.Sprintf("    (If there are multiple clients with <%dms latency for that device, assign the device to the client with the fewest devices", latencyThresholdMs))
	log.Info("    Otherwise, associate each device with the client with the lowest latency)")

	log.Info("Assign devices to clients based on latency")
	batchData := assignDevicesToClients(devices, clients, clientLatencies, allocateAddrHostsSet, test.ShuffleDevices)

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

		var clientsToConnect []*qa.Client
		for _, client := range clients {
			if assignment, ok := batch[client.Host]; ok {
				// Connect if: first batch, device changed, or client is not currently up
				if batchNum == 0 {
					clientsToConnect = append(clientsToConnect, client)
				} else if prev, ok := batchData[batchNum-1][client.Host]; !ok || prev.Device.Code != assignment.Device.Code {
					clientsToConnect = append(clientsToConnect, client)
				} else {
					// Same device as previous batch - check if client is still connected
					status, err := client.GetUserStatus(ctx)
					if err != nil || status.SessionStatus != qa.UserStatusUp {
						clientsToConnect = append(clientsToConnect, client)
					}
				}
			}
		}

		t.Run(fmt.Sprintf("batch_%d", batchNum+1), func(t *testing.T) {
			connectedClients := connectClientsAndWaitForRoutes(t, ctx, log, clientsToConnect, clients, batch)
			if len(connectedClients) < 2 {
				t.Fatalf("fewer than 2 clients connected (%d), cannot run connectivity tests", len(connectedClients))
			}
			runConnectivitySubtests(t, log, connectedClients, batch, &resultsMu)
		})

		printTestReportTable(log, BatchData{batchNum: batch}, clientLatencies, true)
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

// assignDevicesToClients() considers latency between each client and device to assign devices to clients:
// If multiple clients have < latencyThresholdMs latency, the device goes to the client with fewest devices.
// Otherwise, the device goes to the client with the lowest latency.
// Allocate-addr clients have no intra-exchange routing, so they must not share exchanges with any other client.
// After assignment, shuffles each client's list, then pads all lists to match the longest so every client has an entry for every batch.
func assignDevicesToClients(devices []*qa.Device, clients []*qa.Client, clientLatencies ClientLatencies, allocateAddrHosts map[string]struct{}, shuffle func([]*qa.Device)) BatchData {
	clientDevices := make(map[string][]*qa.Device)
	// Track exchange usage to enforce allocate-addr isolation
	allocateAddrExchanges := make(map[string]string)    // exchange -> allocate-addr client hostname
	nonAllocateAddrExchanges := make(map[string]string) // exchange -> non-allocate-addr client hostname

	for _, device := range devices {
		var lowLatencyClients []string
		var bestClientHostname string
		bestLatency := math.MaxFloat64

		for _, client := range clients {
			_, isAllocateAddr := allocateAddrHosts[client.Host]

			// Enforce device.exchange isolation for allocate-addr clients
			if isAllocateAddr {
				// Don't connect an allocate-addr client to an exchange already used by another client
				if existingClient, exists := allocateAddrExchanges[device.ExchangeCode]; exists && existingClient != client.Host {
					continue
				}
				if _, exists := nonAllocateAddrExchanges[device.ExchangeCode]; exists {
					continue
				}
			} else {
				// Don't connect a non-allocate-addr client to an exchange already used by another client
				if _, exists := allocateAddrExchanges[device.ExchangeCode]; exists {
					continue
				}
			}

			latencyMs, ok := clientLatencies[client.Host][device.Code]
			if !ok {
				continue
			}

			if latencyMs < latencyThresholdMs {
				lowLatencyClients = append(lowLatencyClients, client.Host)
			}

			if latencyMs < bestLatency {
				bestLatency = latencyMs
				bestClientHostname = client.Host
			}
		}

		var assignedClientHostname string
		if len(lowLatencyClients) > 1 {
			// Multiple clients qualify - assign to client with fewest devices.
			assignedClientHostname = lowLatencyClients[0]
			minDevices := len(clientDevices[assignedClientHostname])
			for _, clientName := range lowLatencyClients[1:] {
				if len(clientDevices[clientName]) < minDevices {
					assignedClientHostname = clientName
					minDevices = len(clientDevices[clientName])
				}
			}
		} else if bestClientHostname != "" {
			assignedClientHostname = bestClientHostname
		}

		if assignedClientHostname != "" {
			clientDevices[assignedClientHostname] = append(clientDevices[assignedClientHostname], device)
			// Track exchange usage
			if _, isAllocateAddr := allocateAddrHosts[assignedClientHostname]; isAllocateAddr {
				allocateAddrExchanges[device.ExchangeCode] = assignedClientHostname
			} else {
				nonAllocateAddrExchanges[device.ExchangeCode] = assignedClientHostname
			}
		}
	}

	// Shuffle each client's device list for randomized test order.
	for clientHost := range clientDevices {
		shuffle(clientDevices[clientHost])
	}

	// Pad all lists to match the longest so every client has an entry for every batch.
	maxBatches := 0
	for _, assigned := range clientDevices {
		maxBatches = max(maxBatches, len(assigned))
	}
	for clientHost := range clientDevices {
		assigned := clientDevices[clientHost]
		if len(assigned) > 0 && len(assigned) < maxBatches {
			lastDevice := assigned[len(assigned)-1]
			for len(clientDevices[clientHost]) < maxBatches {
				clientDevices[clientHost] = append(clientDevices[clientHost], lastDevice)
			}
		}
	}

	// Convert to BatchData
	batchData := make(BatchData)
	for batchNum := 0; batchNum < maxBatches; batchNum++ {
		batchData[batchNum] = make(map[string]*BatchResult)
		for clientHost, devices := range clientDevices {
			batchData[batchNum][clientHost] = &BatchResult{Device: devices[batchNum]}
		}
	}
	return batchData
}

// Print a table of each client's device assignments and latencies, plus test results if showResults is true.
func printTestReportTable(log *slog.Logger, batchData BatchData, clientLatencies ClientLatencies, showResults bool) {
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
	batch map[string]*BatchResult,
) []*qa.Client {
	// Connect only clients whose device changed from previous batch
	for _, c := range clientsToConnect {
		device := batch[c.Host].Device
		err := c.ConnectUserUnicast_NoWait(ctx, device.Code)
		if err != nil {
			log.Error("Failed to start connection", "client", c.Host, "device", device.Code, "error", err)
			t.Errorf("failed to connect client %s to device %s: %v", c.Host, device.Code, err)
		}
	}

	// Wait for status up for clients that reconnected
	for _, c := range clientsToConnect {
		err := c.WaitForStatusUp(ctx)
		if err != nil {
			log.Error("Client failed to reach status up", "client", c.Host, "error", err)
			t.Errorf("failed to wait for status for client %s: %v", c.Host, err)
		}
	}

	// Build list of all clients that are connected (both newly connected and previously connected)
	var connectedClients []*qa.Client
	for _, c := range allClients {
		if _, ok := batch[c.Host]; !ok {
			continue
		}
		status, err := c.GetUserStatus(ctx)
		if err != nil {
			log.Error("Failed to get user status", "client", c.Host, "error", err)
			continue
		}
		if status.SessionStatus != qa.UserStatusUp {
			log.Warn("Client not connected", "client", c.Host, "status", status.SessionStatus)
			continue
		}
		connectedClients = append(connectedClients, c)
	}

	// Wait for routes between all connected clients
	for _, c := range connectedClients {
		err := c.WaitForRoutes(ctx, qa.MapFilter(connectedClients, func(other *qa.Client) (net.IP, bool) {
			if other.Host == c.Host || batch[other.Host].Device.ExchangeCode == batch[c.Host].Device.ExchangeCode {
				return nil, false
			}
			return other.DoublezeroOrPublicIP(), true
		}))
		if err != nil {
			log.Error("Failed to wait for routes", "client", c.Host, "error", err)
			t.Errorf("failed to wait for routes on client %s: %v", c.Host, err)
		}
	}

	return connectedClients
}

func runConnectivitySubtests(
	t *testing.T,
	outerLog *slog.Logger,
	clients []*qa.Client,
	batch map[string]*BatchResult,
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
						assert.NoError(t, err, "failed to test connectivity")
						mu.Lock()
						failedTests++
						mu.Unlock()
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
