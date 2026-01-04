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

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	devicesFlag = flag.String("devices", "", "comma separated list of devices to run tests against")
)

const latencyThresholdMs = 50

type ClientLatencies map[string]map[string]float64

type BatchAssignment struct {
	Device          *qa.Device
	PacketsSent     uint32
	PacketsReceived uint32
}
type BatchData map[int]map[string]*BatchAssignment

func TestQA_AllDevices_UnicastConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-devices test in short mode")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
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
	batchData := assignDevicesToClientsByLatency(devices, clients, clientLatencies, test.ShuffleDevices)

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

	log.Info("Step 3. Run connectivity tests for each batch")

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

		clientToDevice := make(map[*qa.Client]*qa.Device)
		var clientsToConnect []*qa.Client
		for _, client := range clients {
			if assignment, ok := batch[client.Host]; ok {
				clientToDevice[client] = assignment.Device
				// Only connect if device changed from previous batch
				if batchNum == 0 {
					clientsToConnect = append(clientsToConnect, client)
				} else if prev, ok := batchData[batchNum-1][client.Host]; !ok || prev.Device.Code != assignment.Device.Code {
					clientsToConnect = append(clientsToConnect, client)
				}
			}
		}

		batchDeviceCodes := make([]string, 0, len(clientToDevice))
		for _, d := range clientToDevice {
			batchDeviceCodes = append(batchDeviceCodes, d.Code)
		}

		t.Run(fmt.Sprintf("batch_%d", batchNum+1), func(t *testing.T) {
			log.Info("Testing batch", "batch", batchNum+1, "devices", strings.Join(batchDeviceCodes, ","))

			connectedClients := connectClientsAndWaitForRoutes(t, ctx, log, clientsToConnect, clients, clientToDevice)
			if len(connectedClients) < 2 {
				log.Warn("Fewer than 2 clients connected, skipping connectivity tests for this batch")
				return
			}
			runConnectivitySubtests(t, log, connectedClients, clientToDevice, batch, &resultsMu)
		})

		printTestReportTable(log, BatchData{batchNum: batch}, clientLatencies, true)
	}

	log.Info("Test results:")
	printTestReportTable(log, batchData, clientLatencies, true)

	var totalSent, totalReceived uint32
	batchesWithLoss := 0
	for _, batch := range batchData {
		for _, assignment := range batch {
			totalSent += assignment.PacketsSent
			totalReceived += assignment.PacketsReceived
			if assignment.PacketsReceived < assignment.PacketsSent {
				batchesWithLoss++
			}
		}
	}
	log.Info("Test summary", "packetsReceived", totalReceived, "packetsSent", totalSent, "batchesWithLoss", batchesWithLoss, "totalBatches", batchCount)
}

// If multiple clients have < latencyThresholdMs latency, the device goes to the client with fewest devices.
// Otherwise, the device goes to the client with the lowest latency.
// After assignment, shuffles each client's list, then pads all lists to match the longest so every client has an entry for every batch.
func assignDevicesToClientsByLatency(devices []*qa.Device, clients []*qa.Client, clientLatencies ClientLatencies, shuffle func([]*qa.Device)) BatchData {
	clientDevices := make(map[string][]*qa.Device)

	for _, device := range devices {
		var lowLatencyClients []string
		var bestClientHostname string
		bestLatency := math.MaxFloat64

		for _, client := range clients {
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
		batchData[batchNum] = make(map[string]*BatchAssignment)
		for clientHost, devices := range clientDevices {
			batchData[batchNum][clientHost] = &BatchAssignment{Device: devices[batchNum]}
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
				cell := fmt.Sprintf("%s (%.1fms)", assignment.Device.Code, latencyMs)
				if showResults {
					// Extra padding for emoji display width (emoji renders as 2 chars)
					cell += "100/100 ✅."
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
				cell = fmt.Sprintf("%s (%.1fms)", assignment.Device.Code, latencyMs)
				if showResults {
					if assignment.PacketsSent > 0 && assignment.PacketsReceived == assignment.PacketsSent {
						cell += fmt.Sprintf(" %d/%d ✅", assignment.PacketsReceived, assignment.PacketsSent)
					} else {
						cell += fmt.Sprintf(" %d/%d ❌", assignment.PacketsReceived, assignment.PacketsSent)
					}
				}
			}
			sb.WriteString(fmt.Sprintf(" %-*s |", colWidths[clientName], cell))
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
	clientToDevice map[*qa.Client]*qa.Device,
) []*qa.Client {
	// Connect only clients whose device changed from previous batch
	for _, c := range clientsToConnect {
		targetDevice := clientToDevice[c]
		err := c.ConnectUserUnicast_NoWait(ctx, targetDevice.Code)
		if err != nil {
			log.Error("Failed to start connection", "client", c.Host, "device", targetDevice.Code, "error", err)
			t.Errorf("failed to connect client %s to device %s: %v", c.Host, targetDevice.Code, err)
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
		if _, ok := clientToDevice[c]; !ok {
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
			if other.Host == c.Host || clientToDevice[other].ExchangeCode == clientToDevice[c].ExchangeCode {
				return nil, false
			}
			return other.PublicIP(), true
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
	clientToDevice map[*qa.Client]*qa.Device,
	batch map[string]*BatchAssignment,
	resultsMu *sync.Mutex,
) {
	for _, client := range clients {
		device := clientToDevice[client]
		srcClient := client

		t.Run(fmt.Sprintf("device_%s__from_%s", device.Code, srcClient.Host), func(t *testing.T) {
			t.Parallel()

			log := newTestLogger(t)
			srcClient.SetLogger(log)
			t.Cleanup(func() {
				srcClient.SetLogger(outerLog)
			})
			subCtx := t.Context()

			var totalSent, totalReceived uint32
			var wg sync.WaitGroup
			var mu sync.Mutex
			for _, target := range clients {
				if target.Host == srcClient.Host {
					continue
				}

				wg.Add(1)
				go func(src, target *qa.Client) {
					defer wg.Done()
					result, err := src.TestUnicastConnectivity(t, subCtx, target)
					if err != nil {
						log.Error("Connectivity test failed", "error", err, "source", src.Host, "target", target.Host, "sourceDevice", clientToDevice[src].Code, "targetDevice", clientToDevice[target].Code)
						assert.NoError(t, err, "failed to test connectivity")
						return
					}
					mu.Lock()
					totalSent += result.PacketsSent
					totalReceived += result.PacketsReceived
					mu.Unlock()
				}(srcClient, target)
			}
			wg.Wait()

			resultsMu.Lock()
			batch[srcClient.Host].PacketsSent += totalSent
			batch[srcClient.Host].PacketsReceived += totalReceived
			resultsMu.Unlock()
		})
	}
}
