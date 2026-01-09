package qa

import (
	"math"
)

const LatencyThresholdMs = 25

type ClientLatencies map[string]map[string]float64

type BatchAssignment struct {
	Device          *Device
	PacketsSent     uint32
	PacketsReceived uint32
	FailedTests     uint32
}

func (b *BatchAssignment) Success() bool {
	return b.FailedTests == 0 && b.PacketsSent > 0 && b.PacketsReceived > 0
}

type BatchData map[int]map[string]*BatchAssignment

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
		batchData[batchNum] = make(map[string]*BatchAssignment)
		for clientHost, devices := range clientDevices {
			batchData[batchNum][clientHost] = &BatchAssignment{Device: devices[batchNum]}
		}
	}
	return batchData
}
