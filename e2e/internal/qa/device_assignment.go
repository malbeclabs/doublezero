package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

type GrafanaConfig struct {
	PrometheusURL string
	Username      string
	APIKey        string
}

func GrafanaConfigFromEnv() *GrafanaConfig {
	prometheusURL := os.Getenv("GRAFANA_PROMETHEUS_URL")
	user := os.Getenv("GRAFANA_PROMETHEUS_USER")
	apiKey := os.Getenv("GRAFANA_API_KEY")

	if prometheusURL == "" || apiKey == "" {
		return nil
	}

	return &GrafanaConfig{
		PrometheusURL: strings.TrimSuffix(prometheusURL, "/"),
		Username:      user,
		APIKey:        apiKey,
	}
}

func GetDevicesWithActiveConfigAgents(ctx context.Context, cfg *GrafanaConfig) (map[string]bool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("grafana config is nil")
	}

	// Query for all devices with GetConfig activity in the last 5m
	query := `sum by (device_code) (increase(controller_grpc_getconfig_requests_total[5m])) > 0`

	// The PrometheusURL already includes /api/prom, so we just append /api/v1/query
	queryURL := fmt.Sprintf("%s/api/v1/query?query=%s", cfg.PrometheusURL, url.QueryEscape(query))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// Grafana Cloud Prometheus uses Basic Auth with instance ID and API key
	req.SetBasicAuth(cfg.Username, cfg.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query grafana: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana query failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("query returned non-success status: %s", result.Status)
	}

	active := make(map[string]bool)
	for _, r := range result.Data.Result {
		if deviceCode, ok := r.Metric["device_code"]; ok && deviceCode != "" {
			active[deviceCode] = true
		}
	}

	return active, nil
}

const LatencyThresholdMs = 25

type ClientLatencies map[string]map[string]float64

type BatchResult struct {
	Device          *Device
	PacketsSent     uint32
	PacketsReceived uint32
	FailedTests     uint32
}

func (b *BatchResult) Success() bool {
	return b.FailedTests == 0 && b.PacketsSent > 0 && b.PacketsReceived > 0
}

type BatchData map[int]map[string]*BatchResult

// ClientStatusGetter returns the session status for a client hostname.
// Used to check if a client needs reconnection.
type ClientStatusGetter func(hostname string) (sessionStatus string, err error)

// DetermineClientsToConnect decides which clients need to connect for a given batch. This saves a significant
// amount of time when clients are assigned to the same device for consecutive batches.
// A client needs to connect if:
// 1. It's the first batch (batchNum == 0)
// 2. The device changed from the previous batch
// 3. The device is the same but the client is not currently "up"
func DetermineClientsToConnect(
	batchNum int,
	batchData BatchData,
	clients []*Client,
	getStatus ClientStatusGetter,
) []*Client {
	batch := batchData[batchNum]
	var clientsToConnect []*Client

	for _, client := range clients {
		assignment, inBatch := batch[client.Host]
		if !inBatch {
			continue
		}

		if batchNum == 0 {
			clientsToConnect = append(clientsToConnect, client)
			continue
		}

		prev, hadPrev := batchData[batchNum-1][client.Host]
		if !hadPrev || prev.Device.Code != assignment.Device.Code {
			clientsToConnect = append(clientsToConnect, client)
			continue
		}

		// Same device as previous batch - check if client is still connected
		status, err := getStatus(client.Host)
		if err != nil || !IsStatusUp(status) {
			clientsToConnect = append(clientsToConnect, client)
		}
	}

	return clientsToConnect
}

// FilterStatusUpClients returns clients that are in the batch and have status "up".
// statuses maps hostname to session status.
func FilterStatusUpClients(clients []*Client, batch map[string]*BatchResult, statuses map[string]string) []*Client {
	var connected []*Client
	for _, c := range clients {
		if _, inBatch := batch[c.Host]; !inBatch {
			continue
		}
		if !IsStatusUp(statuses[c.Host]) {
			continue
		}
		connected = append(connected, c)
	}
	return connected
}

// ComputeRouteTargets returns the IPs that a client should have routes to.
// Excludes clients in the same exchange (no intra-exchange routing) and self.
func ComputeRouteTargets(client *Client, connectedClients []*Client, batch map[string]*BatchResult, getIP func(*Client) net.IP) []net.IP {
	clientExchange := batch[client.Host].Device.ExchangeCode
	var targets []net.IP
	for _, other := range connectedClients {
		if other.Host == client.Host {
			continue
		}
		if batch[other.Host].Device.ExchangeCode == clientExchange {
			continue
		}
		if ip := getIP(other); ip != nil {
			targets = append(targets, ip)
		}
	}
	return targets
}

// AssignDevicesToClients considers latency between each client and device to assign devices to clients:
// If multiple clients have < LatencyThresholdMs latency, the device goes to the client with fewest devices.
// Otherwise, the device goes to the client with the lowest latency.
// Allocate-addr clients have no intra-exchange routing, so they must not share exchanges with any other client.
// After assignment, shuffles each client's list, then pads all lists to match the longest so every client has an entry for every batch.
func AssignDevicesToClients(devices []*Device, clients []*Client, clientLatencies ClientLatencies, allocateAddrHosts map[string]struct{}, shuffle func([]*Device)) BatchData {
	clientDevices := make(map[string][]*Device)
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

			if latencyMs < LatencyThresholdMs {
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

// HostFailureStats aggregates per-host failure information after deduping
// repeated tests of the same (host, device) pair.
type HostFailureStats struct {
	Total         int      // unique devices assigned to this host
	Failed        int      // unique devices that never succeeded on this host
	FailedDevices []string // sorted, deduped device codes
}

// DeviceRetest describes a (host, device) pair that was tested more than once.
type DeviceRetest struct {
	Host       string
	DeviceCode string
	Attempts   int
	Successes  int
	Failures   int
}

// FailureStats summarizes the outcome of a QA all-devices run after applying
// the "any success counts as success" rule:
//   - A device that succeeded at least once is treated as a success overall.
//   - A device that never succeeded counts as a single failure, regardless of
//     how many times it was retested.
//
// DeviceResults is sorted by DeviceCode. Each HostFailureStats.FailedDevices
// slice is sorted by device code. Retests is sorted by (Host, DeviceCode).
type FailureStats struct {
	DeviceResults []DeviceTestResult          // one per unique device code
	PerHost       map[string]HostFailureStats // keyed by host
	Retests       []DeviceRetest              // entries where Attempts > 1
}

// ComputeFailureStats walks batchData once and applies the "any success
// counts as success" rule per device. Repeated tests of the same
// (host, device) collapse into a single result for both the overall device
// list and per-host stats.
func ComputeFailureStats(batchData BatchData) FailureStats {
	// hostDeviceAttempts[host][code] = number of attempts
	hostDeviceAttempts := make(map[string]map[string]int)
	// hostDeviceSuccesses[host][code] = number of successful attempts
	hostDeviceSuccesses := make(map[string]map[string]int)
	deviceSucceeded := make(map[string]bool)
	devicePubkey := make(map[string]string)

	batchNums := slices.Sorted(maps.Keys(batchData))
	for _, batchNum := range batchNums {
		hosts := slices.Sorted(maps.Keys(batchData[batchNum]))
		for _, host := range hosts {
			assignment := batchData[batchNum][host]
			code := assignment.Device.Code
			if hostDeviceAttempts[host] == nil {
				hostDeviceAttempts[host] = make(map[string]int)
				hostDeviceSuccesses[host] = make(map[string]int)
			}
			hostDeviceAttempts[host][code]++
			devicePubkey[code] = assignment.Device.PubKey
			if assignment.Success() {
				hostDeviceSuccesses[host][code]++
				deviceSucceeded[code] = true
			} else if _, seen := deviceSucceeded[code]; !seen {
				deviceSucceeded[code] = false
			}
		}
	}

	deviceCodes := slices.Sorted(maps.Keys(deviceSucceeded))
	deviceResults := make([]DeviceTestResult, 0, len(deviceCodes))
	for _, code := range deviceCodes {
		deviceResults = append(deviceResults, DeviceTestResult{
			DeviceCode:   code,
			DevicePubkey: devicePubkey[code],
			Success:      deviceSucceeded[code],
		})
	}

	perHost := make(map[string]HostFailureStats, len(hostDeviceAttempts))
	for host, attempts := range hostDeviceAttempts {
		stats := HostFailureStats{Total: len(attempts)}
		codes := slices.Sorted(maps.Keys(attempts))
		for _, code := range codes {
			if hostDeviceSuccesses[host][code] == 0 {
				stats.Failed++
				stats.FailedDevices = append(stats.FailedDevices, code)
			}
		}
		perHost[host] = stats
	}

	var retests []DeviceRetest
	hosts := slices.Sorted(maps.Keys(hostDeviceAttempts))
	for _, host := range hosts {
		codes := slices.Sorted(maps.Keys(hostDeviceAttempts[host]))
		for _, code := range codes {
			n := hostDeviceAttempts[host][code]
			if n <= 1 {
				continue
			}
			successes := hostDeviceSuccesses[host][code]
			retests = append(retests, DeviceRetest{
				Host:       host,
				DeviceCode: code,
				Attempts:   n,
				Successes:  successes,
				Failures:   n - successes,
			})
		}
	}

	return FailureStats{
		DeviceResults: deviceResults,
		PerHost:       perHost,
		Retests:       retests,
	}
}
