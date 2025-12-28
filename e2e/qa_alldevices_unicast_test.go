//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"math/rand/v2"
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

type deviceTestResult struct {
	DeviceCode      string
	Host            string
	PacketsSent     uint32
	PacketsReceived uint32
}

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

	// Gather latency data from each client.
	// hostLatencies: host -> deviceCode -> latencyMs
	log.Info("Collecting host-to-device latencies")
	hostLatencies := make(map[string]map[string]float64)
	for _, client := range clients {
		latencies, err := client.GetLatency(ctx)
		require.NoError(t, err, "failed to get latency from host %s", client.Host)

		hostLatencies[client.Host] = make(map[string]float64)
		for _, l := range latencies {
			if l.Reachable {
				hostLatencies[client.Host][l.DeviceCode] = float64(l.AvgLatencyNs) / 1_000_000.0 // ns -> ms
			}
		}
	}

	// Assign devices to hosts based on latency.
	log.Info("Assigning devices to hosts based on latency")
	deviceAssignments := assignDevicesToHosts(devices, clients, hostLatencies)

	// Shuffle each host's device list for randomized test order.
	for host := range deviceAssignments {
		rand.Shuffle(len(deviceAssignments[host]), func(i, j int) {
			deviceAssignments[host][i], deviceAssignments[host][j] = deviceAssignments[host][j], deviceAssignments[host][i]
		})
	}

	printTestReportTable(log, deviceAssignments, hostLatencies, nil)

	// Calculate the number of batches needed (max devices per host).
	maxBatches := 0
	for _, assigned := range deviceAssignments {
		if len(assigned) > maxBatches {
			maxBatches = len(assigned)
		}
	}

	if maxBatches == 0 {
		t.Skip("No devices assigned to any host")
	}

	log.Info("Planning to test",
		"deviceCount", len(devices),
		"clientCount", len(clients),
		"totalBatches", maxBatches,
	)

	// Track active hosts and their current device index.
	activeClients := slices.Clone(clients)
	hostDeviceIndex := make(map[string]int)
	hostLastDevice := make(map[string]*qa.Device)

	// Track test results for final report.
	// Pre-record all devices with zero packets (will be updated when tests run).
	results := make(map[string]*deviceTestResult)
	var resultsMu sync.Mutex
	for host, assigned := range deviceAssignments {
		for _, device := range assigned {
			results[device.Code] = &deviceTestResult{
				DeviceCode:      device.Code,
				Host:            host,
				PacketsSent:     0,
				PacketsReceived: 0,
			}
		}
	}

	for batchNum := 0; batchNum < maxBatches; batchNum++ {
		// Determine what each host tests this batch.
		clientToDevice := make(map[*qa.Client]*qa.Device)

		// Track hosts to remove after this batch because they have no more devices to test
		var hostsToRemove []*qa.Client

		for _, client := range activeClients {
			idx := hostDeviceIndex[client.Host]
			assigned := deviceAssignments[client.Host]

			if idx < len(assigned) {
				// Has devices left - test next one.
				clientToDevice[client] = assigned[idx]
				hostDeviceIndex[client.Host]++
				hostLastDevice[client.Host] = assigned[idx]
			} else if len(activeClients) > 2 {
				// No devices left, >=3 hosts remain - mark for removal.
				hostsToRemove = append(hostsToRemove, client)
			} else {
				// No devices left, exactly 2 hosts - keep this host connected to its last device so we can test the remaining devices on the other host
				clientToDevice[client] = hostLastDevice[client.Host]
			}
		}

		// Remove hosts that have no more devices (if >=3 hosts remained).
		for _, client := range hostsToRemove {
			log.Info("Removing host from testing (no more devices)", "host", client.Host)
			err := client.DisconnectUser(ctx, true, true)
			if err != nil {
				log.Warn("Failed to disconnect removed host", "host", client.Host, "error", err)
			}
			activeClients = slices.DeleteFunc(activeClients, func(c *qa.Client) bool {
				return c.Host == client.Host
			})
			delete(clientToDevice, client)
		}

		if len(activeClients) < 2 {
			log.Info("Fewer than 2 active hosts remain, ending test")
			break
		}

		batchDeviceCodes := make([]string, 0, len(clientToDevice))
		for _, d := range clientToDevice {
			batchDeviceCodes = append(batchDeviceCodes, d.Code)
		}

		t.Run(fmt.Sprintf("batch_%d", batchNum+1), func(t *testing.T) {
			log.Info("Testing batch", "batch", batchNum+1, "devices", strings.Join(batchDeviceCodes, ","))

			t.Cleanup(func() {
				var wg sync.WaitGroup
				for _, client := range activeClients {
					wg.Add(1)
					go func(client *qa.Client) {
						defer wg.Done()
						err := client.DisconnectUser(context.Background(), true, true)
						assert.NoError(t, err, "failed to disconnect user")
					}(client)
				}
				wg.Wait()
			})

			connectClientsAndWaitForRoutes(t, ctx, log, activeClients, clientToDevice)
			runConnectivitySubtests(t, log, activeClients, clientToDevice, results, &resultsMu)
		})
	}

	// Print final test report.
	printTestReportTable(log, deviceAssignments, hostLatencies, results)

	var totalSent, totalReceived uint32
	var devicesWithLoss int
	for _, r := range results {
		totalSent += r.PacketsSent
		totalReceived += r.PacketsReceived
		if r.PacketsReceived < r.PacketsSent {
			devicesWithLoss++
		}
	}
	log.Info("Test summary", "packetsReceived", totalReceived, "packetsSent", totalSent, "devicesWithLoss", devicesWithLoss, "totalDevices", len(results))
}

// assignDevicesToHosts assigns each device to a host based on latency.
// If multiple hosts have <50ms latency, the device goes to the host with fewest devices.
// Otherwise, the device goes to the host with the best latency.
func assignDevicesToHosts(devices []*qa.Device, clients []*qa.Client, hostLatencies map[string]map[string]float64) map[string][]*qa.Device {
	deviceAssignments := make(map[string][]*qa.Device)

	for _, device := range devices {
		var lowLatencyHosts []string
		var bestHost string
		bestLatency := math.MaxFloat64

		for _, client := range clients {
			latencyMs, ok := hostLatencies[client.Host][device.Code]
			if !ok {
				continue
			}

			if latencyMs < latencyThresholdMs {
				lowLatencyHosts = append(lowLatencyHosts, client.Host)
			}

			if latencyMs < bestLatency {
				bestLatency = latencyMs
				bestHost = client.Host
			}
		}

		var assignedHost string
		if len(lowLatencyHosts) > 1 {
			// Multiple hosts qualify - assign to host with fewest devices.
			assignedHost = lowLatencyHosts[0]
			minDevices := len(deviceAssignments[assignedHost])
			for _, host := range lowLatencyHosts[1:] {
				if len(deviceAssignments[host]) < minDevices {
					assignedHost = host
					minDevices = len(deviceAssignments[host])
				}
			}
		} else if bestHost != "" {
			assignedHost = bestHost
		}

		if assignedHost != "" {
			deviceAssignments[assignedHost] = append(deviceAssignments[assignedHost], device)
		}
	}

	return deviceAssignments
}

func printTestReportTable(log *slog.Logger, deviceAssignments map[string][]*qa.Device, hostLatencies map[string]map[string]float64, results map[string]*deviceTestResult) {
	hosts := slices.Sorted(maps.Keys(deviceAssignments))
	maxDevices := 0
	colWidths := make(map[string]int)
	for _, host := range hosts {
		colWidths[host] = len(host)
		maxDevices = max(maxDevices, len(deviceAssignments[host]))
		for _, d := range deviceAssignments[host] {
			latencyMs := hostLatencies[host][d.Code]
			cell := fmt.Sprintf("%s (%.1fms)", d.Code, latencyMs)
			if results != nil {
				// Add space for " 25/25" suffix (max reasonable packet count display)
				cell += " 25/25"
			}
			colWidths[host] = max(colWidths[host], len(cell))
		}
	}

	// Calculate batch column width based on max batch number.
	batchColWidth := len(fmt.Sprintf("Batch %d", maxDevices))

	var sb strings.Builder
	sb.WriteString("\n")

	// Header row
	sb.WriteString(fmt.Sprintf("| %-*s |", batchColWidth, ""))
	for _, host := range hosts {
		sb.WriteString(fmt.Sprintf(" %-*s |", colWidths[host], host))
	}
	sb.WriteString("\n")

	// Separator row
	sb.WriteString("|")
	sb.WriteString(strings.Repeat("-", batchColWidth+2))
	sb.WriteString("|")
	for _, host := range hosts {
		sb.WriteString(strings.Repeat("-", colWidths[host]+2))
		sb.WriteString("|")
	}
	sb.WriteString("\n")

	// Data rows
	for i := 0; i < maxDevices; i++ {
		sb.WriteString(fmt.Sprintf("| Batch %-*d |", batchColWidth-6, i+1))
		for _, host := range hosts {
			assigned := deviceAssignments[host]
			cell := ""
			if i < len(assigned) {
				latencyMs := hostLatencies[host][assigned[i].Code]
				cell = fmt.Sprintf("%s (%.1fms)", assigned[i].Code, latencyMs)
				if results != nil {
					if r, ok := results[assigned[i].Code]; ok {
						cell += fmt.Sprintf(" %d/%d", r.PacketsReceived, r.PacketsSent)
					} else {
						cell += " -/-"
					}
				}
			}
			sb.WriteString(fmt.Sprintf(" %-*s |", colWidths[host], cell))
		}
		sb.WriteString("\n")
	}

	if results == nil {
		log.Info("Host device assignments:")
	} else {
		log.Info("Test Report:")
	}
	fmt.Print(sb.String())
}

func connectClientsAndWaitForRoutes(
	t *testing.T,
	ctx context.Context,
	log *slog.Logger,
	activeClients []*qa.Client,
	clientToDevice map[*qa.Client]*qa.Device,
) {
	for _, c := range activeClients {
		targetDevice := clientToDevice[c]
		err := c.ConnectUserUnicast_NoWait(ctx, targetDevice.Code)
		require.NoError(t, err, "failed to connect user %s to device %s", c.Host, targetDevice.Code)
	}

	for _, c := range activeClients {
		err := c.WaitForStatusUp(ctx)
		require.NoError(t, err, "failed to wait for status for client %s", c.Host)
	}

	for _, c := range activeClients {
		err := c.WaitForRoutes(ctx, qa.MapFilter(activeClients, func(other *qa.Client) (net.IP, bool) {
			if other.Host == c.Host || clientToDevice[other].ExchangeCode == clientToDevice[c].ExchangeCode {
				return nil, false
			}
			return other.PublicIP(), true
		}))
		require.NoError(t, err, "failed to wait for routes on client %s", c.Host)
	}
}

func runConnectivitySubtests(
	t *testing.T,
	outerLog *slog.Logger,
	activeClients []*qa.Client,
	clientToDevice map[*qa.Client]*qa.Device,
	results map[string]*deviceTestResult,
	resultsMu *sync.Mutex,
) {
	for _, client := range activeClients {
		device := clientToDevice[client]
		srcClient := client

		// Record the test attempt if not already recorded.
		resultsMu.Lock()
		if _, exists := results[device.Code]; !exists {
			results[device.Code] = &deviceTestResult{
				DeviceCode:      device.Code,
				Host:            srcClient.Host,
				PacketsSent:     0,
				PacketsReceived: 0,
			}
		}
		resultsMu.Unlock()

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
			for _, target := range activeClients {
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
			results[device.Code].PacketsSent += totalSent
			results[device.Code].PacketsReceived += totalReceived
			resultsMu.Unlock()
		})
	}
}
