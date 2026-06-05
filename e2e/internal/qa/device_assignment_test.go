package qa

import (
	"fmt"
	"net"
	"reflect"
	"testing"
)

func TestAssignDevicesToClients(t *testing.T) {
	noShuffle := func(devices []*Device) {}

	t.Run("assigns to client with fewest devices when multiple have low latency", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
			{Code: "dev2", ExchangeCode: "ex2"},
			{Code: "dev3", ExchangeCode: "ex3"},
		}
		clients := []*Client{
			{Host: "client1"},
			{Host: "client2"},
		}
		// Both clients have low latency (<25ms) to all devices
		latencies := ClientLatencies{
			"client1": {"dev1": 10, "dev2": 10, "dev3": 10},
			"client2": {"dev1": 10, "dev2": 10, "dev3": 10},
		}

		result := AssignDevicesToClients(devices, clients, latencies, nil, noShuffle)

		// Should distribute devices between clients
		client1Devices := 0
		client2Devices := 0
		for _, batch := range result {
			if _, ok := batch["client1"]; ok {
				client1Devices++
			}
			if _, ok := batch["client2"]; ok {
				client2Devices++
			}
		}
		// With 3 devices and 2 clients, distribution should be 2-1 or 1-2
		if client1Devices == 0 || client2Devices == 0 {
			t.Errorf("expected devices distributed between clients, got client1=%d, client2=%d", client1Devices, client2Devices)
		}
	})

	t.Run("assigns to client with lowest latency when none below threshold", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
		}
		clients := []*Client{
			{Host: "client1"},
			{Host: "client2"},
		}
		// Both clients have high latency (>25ms), client2 is closer
		latencies := ClientLatencies{
			"client1": {"dev1": 100},
			"client2": {"dev1": 60},
		}

		result := AssignDevicesToClients(devices, clients, latencies, nil, noShuffle)

		if len(result) != 1 {
			t.Fatalf("expected 1 batch, got %d", len(result))
		}
		if result[0]["client2"] == nil || result[0]["client2"].Device.Code != "dev1" {
			t.Error("expected dev1 assigned to client2 (lowest latency)")
		}
	})

	t.Run("assigns to single low-latency client even when another has higher latency", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
		}
		clients := []*Client{
			{Host: "client1"},
			{Host: "client2"},
		}
		// Only client1 has low latency (<25ms), client2 has high latency
		latencies := ClientLatencies{
			"client1": {"dev1": 10},
			"client2": {"dev1": 100},
		}

		result := AssignDevicesToClients(devices, clients, latencies, nil, noShuffle)

		if len(result) != 1 {
			t.Fatalf("expected 1 batch, got %d", len(result))
		}
		if result[0]["client1"] == nil || result[0]["client1"].Device.Code != "dev1" {
			t.Error("expected dev1 assigned to client1 (only low-latency client)")
		}
	})

	t.Run("pads device lists to equal length", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
			{Code: "dev2", ExchangeCode: "ex2"},
			{Code: "dev3", ExchangeCode: "ex3"},
		}
		clients := []*Client{
			{Host: "client1"},
			{Host: "client2"},
		}
		// client1 close to all devices, client2 only close to dev3
		latencies := ClientLatencies{
			"client1": {"dev1": 10, "dev2": 10, "dev3": 100},
			"client2": {"dev1": 100, "dev2": 100, "dev3": 10},
		}

		result := AssignDevicesToClients(devices, clients, latencies, nil, noShuffle)

		// Every batch should have both clients
		for batchNum, batch := range result {
			if batch["client1"] == nil {
				t.Errorf("batch %d missing client1", batchNum)
			}
			if batch["client2"] == nil {
				t.Errorf("batch %d missing client2", batchNum)
			}
		}
	})

	t.Run("allocate-addr clients isolated from all other clients", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
			{Code: "dev2", ExchangeCode: "ex1"}, // same exchange as dev1
			{Code: "dev3", ExchangeCode: "ex2"},
		}
		clients := []*Client{
			{Host: "client1"}, // allocate-addr
			{Host: "client2"}, // regular
		}
		latencies := ClientLatencies{
			"client1": {"dev1": 10, "dev2": 10, "dev3": 10},
			"client2": {"dev1": 10, "dev2": 10, "dev3": 10},
		}
		allocateAddrHosts := map[string]struct{}{"client1": {}}

		result := AssignDevicesToClients(devices, clients, latencies, allocateAddrHosts, noShuffle)

		// Verify no exchange overlap between client1 and client2
		client1Exchanges := make(map[string]bool)
		client2Exchanges := make(map[string]bool)
		for _, batch := range result {
			if a := batch["client1"]; a != nil {
				client1Exchanges[a.Device.ExchangeCode] = true
			}
			if a := batch["client2"]; a != nil {
				client2Exchanges[a.Device.ExchangeCode] = true
			}
		}
		for ex := range client1Exchanges {
			if client2Exchanges[ex] {
				t.Errorf("exchange %s shared between allocate-addr client1 and client2", ex)
			}
		}
	})

	t.Run("two allocate-addr clients get separate exchanges", func(t *testing.T) {
		devices := []*Device{
			{Code: "dev1", ExchangeCode: "ex1"},
			{Code: "dev2", ExchangeCode: "ex2"},
		}
		clients := []*Client{
			{Host: "client1"},
			{Host: "client2"},
		}
		latencies := ClientLatencies{
			"client1": {"dev1": 10, "dev2": 10},
			"client2": {"dev1": 10, "dev2": 10},
		}
		allocateAddrHosts := map[string]struct{}{"client1": {}, "client2": {}}

		result := AssignDevicesToClients(devices, clients, latencies, allocateAddrHosts, noShuffle)

		// Each client should have a different exchange
		client1Exchanges := make(map[string]bool)
		client2Exchanges := make(map[string]bool)
		for _, batch := range result {
			if a := batch["client1"]; a != nil {
				client1Exchanges[a.Device.ExchangeCode] = true
			}
			if a := batch["client2"]; a != nil {
				client2Exchanges[a.Device.ExchangeCode] = true
			}
		}
		for ex := range client1Exchanges {
			if client2Exchanges[ex] {
				t.Errorf("exchange %s shared between two allocate-addr clients", ex)
			}
		}
	})
}

func TestDetermineClientsToConnect(t *testing.T) {
	dev1 := &Device{Code: "dev1", ExchangeCode: "ex1"}
	dev2 := &Device{Code: "dev2", ExchangeCode: "ex2"}
	client1 := &Client{Host: "client1"}
	client2 := &Client{Host: "client2"}
	clients := []*Client{client1, client2}

	t.Run("first batch connects all clients in batch", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
		}
		getStatus := func(hostname string) (string, error) {
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(0, batchData, clients, getStatus)

		if len(result) != 2 {
			t.Errorf("expected 2 clients, got %d", len(result))
		}
	})

	t.Run("device change triggers reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
			1: {"client1": {Device: dev2}, "client2": {Device: dev2}}, // client1 changed device
		}
		getStatus := func(hostname string) (string, error) {
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1 to reconnect, got %s", result[0].Host)
		}
	})

	t.Run("same device but not up triggers reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
			1: {"client1": {Device: dev1}, "client2": {Device: dev2}}, // same devices
		}
		getStatus := func(hostname string) (string, error) {
			if hostname == "client1" {
				return "disconnected", nil // client1 is not up
			}
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1 to reconnect, got %s", result[0].Host)
		}
	})

	t.Run("status error triggers reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
			1: {"client1": {Device: dev1}, "client2": {Device: dev2}},
		}
		getStatus := func(hostname string) (string, error) {
			if hostname == "client2" {
				return "", fmt.Errorf("connection refused")
			}
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client2" {
			t.Errorf("expected client2 to reconnect, got %s", result[0].Host)
		}
	})

	t.Run("pending status triggers reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
			1: {"client1": {Device: dev1}, "client2": {Device: dev2}},
		}
		getStatus := func(hostname string) (string, error) {
			if hostname == "client1" {
				return "pending", nil
			}
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1 to reconnect, got %s", result[0].Host)
		}
	})

	t.Run("client not in batch is skipped", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}}, // client2 not in batch
		}
		getStatus := func(hostname string) (string, error) {
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(0, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1, got %s", result[0].Host)
		}
	})

	t.Run("client not in previous batch triggers reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}},                            // only client1
			1: {"client1": {Device: dev1}, "client2": {Device: dev2}}, // client2 added
		}
		getStatus := func(hostname string) (string, error) {
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client2" {
			t.Errorf("expected client2 to connect, got %s", result[0].Host)
		}
	})

	t.Run("same device and up does not reconnect", func(t *testing.T) {
		batchData := BatchData{
			0: {"client1": {Device: dev1}, "client2": {Device: dev2}},
			1: {"client1": {Device: dev1}, "client2": {Device: dev2}},
		}
		getStatus := func(hostname string) (string, error) {
			return UserStatusUp, nil
		}

		result := DetermineClientsToConnect(1, batchData, clients, getStatus)

		if len(result) != 0 {
			t.Errorf("expected 0 clients to reconnect, got %d", len(result))
		}
	})
}

func TestFilterStatusUpClients(t *testing.T) {
	dev1 := &Device{Code: "dev1", ExchangeCode: "ex1"}
	dev2 := &Device{Code: "dev2", ExchangeCode: "ex2"}
	client1 := &Client{Host: "client1"}
	client2 := &Client{Host: "client2"}
	client3 := &Client{Host: "client3"}
	clients := []*Client{client1, client2, client3}

	t.Run("filters to clients in batch with status up", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			"client2": {Device: dev2},
			// client3 not in batch
		}
		statuses := map[string]string{
			"client1": UserStatusUp,
			"client2": UserStatusUp,
		}

		result := FilterStatusUpClients(clients, batch, statuses)

		if len(result) != 2 {
			t.Errorf("expected 2 clients, got %d", len(result))
		}
	})

	t.Run("excludes clients not in batch", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			// client2, client3 not in batch
		}
		statuses := map[string]string{
			"client1": UserStatusUp,
			"client2": UserStatusUp,
			"client3": UserStatusUp,
		}

		result := FilterStatusUpClients(clients, batch, statuses)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1, got %s", result[0].Host)
		}
	})

	t.Run("excludes clients with status not up", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			"client2": {Device: dev2},
		}
		statuses := map[string]string{
			"client1": UserStatusUp,
			"client2": "disconnected",
		}

		result := FilterStatusUpClients(clients, batch, statuses)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1, got %s", result[0].Host)
		}
	})

	t.Run("excludes clients with missing status", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			"client2": {Device: dev2},
		}
		statuses := map[string]string{
			"client1": UserStatusUp,
			// client2 status missing (e.g., GetUserStatus failed)
		}

		result := FilterStatusUpClients(clients, batch, statuses)

		if len(result) != 1 {
			t.Fatalf("expected 1 client, got %d", len(result))
		}
		if result[0].Host != "client1" {
			t.Errorf("expected client1, got %s", result[0].Host)
		}
	})
}

func TestComputeRouteTargets(t *testing.T) {
	dev1 := &Device{Code: "dev1", ExchangeCode: "ex1"}
	dev2 := &Device{Code: "dev2", ExchangeCode: "ex2"}
	dev3 := &Device{Code: "dev3", ExchangeCode: "ex1"} // same exchange as dev1
	client1 := &Client{Host: "client1"}
	client2 := &Client{Host: "client2"}
	client3 := &Client{Host: "client3"}

	getIP := func(c *Client) net.IP {
		switch c.Host {
		case "client1":
			return net.ParseIP("10.0.0.1")
		case "client2":
			return net.ParseIP("10.0.0.2")
		case "client3":
			return net.ParseIP("10.0.0.3")
		}
		return nil
	}

	t.Run("returns IPs of clients in different exchanges", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1}, // ex1
			"client2": {Device: dev2}, // ex2
		}
		connectedClients := []*Client{client1, client2}

		result := ComputeRouteTargets(client1, connectedClients, batch, getIP)

		if len(result) != 1 {
			t.Fatalf("expected 1 target, got %d", len(result))
		}
		if !result[0].Equal(net.ParseIP("10.0.0.2")) {
			t.Errorf("expected 10.0.0.2, got %s", result[0])
		}
	})

	t.Run("excludes self", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			"client2": {Device: dev2},
		}
		connectedClients := []*Client{client1, client2}

		result := ComputeRouteTargets(client1, connectedClients, batch, getIP)

		for _, ip := range result {
			if ip.Equal(net.ParseIP("10.0.0.1")) {
				t.Error("should not include self IP")
			}
		}
	})

	t.Run("excludes clients in same exchange", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1}, // ex1
			"client2": {Device: dev2}, // ex2
			"client3": {Device: dev3}, // ex1 (same as client1)
		}
		connectedClients := []*Client{client1, client2, client3}

		result := ComputeRouteTargets(client1, connectedClients, batch, getIP)

		if len(result) != 1 {
			t.Fatalf("expected 1 target (only client2), got %d", len(result))
		}
		if !result[0].Equal(net.ParseIP("10.0.0.2")) {
			t.Errorf("expected 10.0.0.2, got %s", result[0])
		}
	})

	t.Run("handles nil IP from getter", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
			"client2": {Device: dev2},
		}
		connectedClients := []*Client{client1, client2}
		nilGetter := func(c *Client) net.IP { return nil }

		result := ComputeRouteTargets(client1, connectedClients, batch, nilGetter)

		if len(result) != 0 {
			t.Errorf("expected 0 targets when getter returns nil, got %d", len(result))
		}
	})

	t.Run("returns empty for single client", func(t *testing.T) {
		batch := map[string]*BatchResult{
			"client1": {Device: dev1},
		}
		connectedClients := []*Client{client1}

		result := ComputeRouteTargets(client1, connectedClients, batch, getIP)

		if len(result) != 0 {
			t.Errorf("expected 0 targets for single client, got %d", len(result))
		}
	})
}

func TestComputeFailureStats(t *testing.T) {
	// pass marks a BatchResult as a successful test (Success() returns true
	// when FailedTests == 0 && PacketsSent > 0 && PacketsReceived > 0).
	pass := func(d *Device) *BatchResult {
		return &BatchResult{Device: d, PacketsSent: 10, PacketsReceived: 10}
	}
	// fail marks a BatchResult as a failed test.
	fail := func(d *Device) *BatchResult {
		return &BatchResult{Device: d, PacketsSent: 10, PacketsReceived: 0, FailedTests: 1}
	}

	dev1 := &Device{Code: "dev1", PubKey: "pk1"}
	dev2 := &Device{Code: "dev2", PubKey: "pk2"}
	dev3 := &Device{Code: "dev3", PubKey: "pk3"}

	t.Run("empty batch data", func(t *testing.T) {
		stats := ComputeFailureStats(BatchData{})
		if len(stats.DeviceResults) != 0 {
			t.Errorf("expected no device results, got %d", len(stats.DeviceResults))
		}
		if len(stats.PerHost) != 0 {
			t.Errorf("expected no per-host stats, got %d", len(stats.PerHost))
		}
		if len(stats.Retests) != 0 {
			t.Errorf("expected no retests, got %d", len(stats.Retests))
		}
	})

	t.Run("single device single host fail", func(t *testing.T) {
		batchData := BatchData{
			0: {"hostA": fail(dev1)},
		}
		stats := ComputeFailureStats(batchData)

		wantDevice := []DeviceTestResult{
			{DeviceCode: "dev1", DevicePubkey: "pk1", Success: false},
		}
		if !reflect.DeepEqual(stats.DeviceResults, wantDevice) {
			t.Errorf("device results: got %+v, want %+v", stats.DeviceResults, wantDevice)
		}
		host := stats.PerHost["hostA"]
		if host.Total != 1 || host.Failed != 1 {
			t.Errorf("per-host: got total=%d failed=%d, want 1/1", host.Total, host.Failed)
		}
		if len(stats.Retests) != 0 {
			t.Errorf("expected no retests, got %d", len(stats.Retests))
		}
	})

	t.Run("single device single host pass", func(t *testing.T) {
		batchData := BatchData{
			0: {"hostA": pass(dev1)},
		}
		stats := ComputeFailureStats(batchData)

		if len(stats.DeviceResults) != 1 || !stats.DeviceResults[0].Success {
			t.Errorf("expected one successful device, got %+v", stats.DeviceResults)
		}
		host := stats.PerHost["hostA"]
		if host.Total != 1 || host.Failed != 0 {
			t.Errorf("per-host: got total=%d failed=%d, want 1/0", host.Total, host.Failed)
		}
	})

	t.Run("device retested 11 times with 3 successes", func(t *testing.T) {
		// Mirrors the scenario from the issue: tyo-mn-qa01 tests tyo001-dz002
		// 11 times, 3 of which succeeded and 8 failed. Expected: one
		// unique device, marked success; per-host 0/1; one retest entry.
		batch := BatchData{}
		// 3 successes
		for i := 0; i < 3; i++ {
			batch[i] = map[string]*BatchResult{"hostA": pass(dev1)}
		}
		// 8 failures
		for i := 3; i < 11; i++ {
			batch[i] = map[string]*BatchResult{"hostA": fail(dev1)}
		}

		stats := ComputeFailureStats(batch)

		if len(stats.DeviceResults) != 1 || !stats.DeviceResults[0].Success {
			t.Errorf("expected single device marked success, got %+v", stats.DeviceResults)
		}
		host := stats.PerHost["hostA"]
		if host.Total != 1 || host.Failed != 0 || len(host.FailedDevices) != 0 {
			t.Errorf("per-host: got total=%d failed=%d failedDevices=%v, want 1/0/[]", host.Total, host.Failed, host.FailedDevices)
		}
		wantRetests := []DeviceRetest{
			{Host: "hostA", DeviceCode: "dev1", Attempts: 11, Successes: 3, Failures: 8},
		}
		if !reflect.DeepEqual(stats.Retests, wantRetests) {
			t.Errorf("retests: got %+v, want %+v", stats.Retests, wantRetests)
		}
	})

	t.Run("device retested 11 times with 0 successes", func(t *testing.T) {
		batch := BatchData{}
		for i := 0; i < 11; i++ {
			batch[i] = map[string]*BatchResult{"hostA": fail(dev1)}
		}

		stats := ComputeFailureStats(batch)

		if len(stats.DeviceResults) != 1 || stats.DeviceResults[0].Success {
			t.Errorf("expected single device marked failure, got %+v", stats.DeviceResults)
		}
		host := stats.PerHost["hostA"]
		if host.Total != 1 || host.Failed != 1 {
			t.Errorf("per-host: got total=%d failed=%d, want 1/1", host.Total, host.Failed)
		}
		if !reflect.DeepEqual(host.FailedDevices, []string{"dev1"}) {
			t.Errorf("failedDevices: got %v, want [dev1]", host.FailedDevices)
		}
		wantRetests := []DeviceRetest{
			{Host: "hostA", DeviceCode: "dev1", Attempts: 11, Successes: 0, Failures: 11},
		}
		if !reflect.DeepEqual(stats.Retests, wantRetests) {
			t.Errorf("retests: got %+v, want %+v", stats.Retests, wantRetests)
		}
	})

	t.Run("device succeeds on host A, fails on host B", func(t *testing.T) {
		// Overall: device succeeded somewhere, so it counts as success.
		// Per-host: A 0/1, B 1/1 (per-host dedupe is (host, device), not global).
		batchData := BatchData{
			0: {"hostA": pass(dev1), "hostB": fail(dev1)},
		}
		stats := ComputeFailureStats(batchData)

		if len(stats.DeviceResults) != 1 || !stats.DeviceResults[0].Success {
			t.Errorf("expected device marked success overall, got %+v", stats.DeviceResults)
		}
		if stats.PerHost["hostA"].Failed != 0 || stats.PerHost["hostA"].Total != 1 {
			t.Errorf("hostA per-host: got %+v, want 0/1", stats.PerHost["hostA"])
		}
		if stats.PerHost["hostB"].Failed != 1 || stats.PerHost["hostB"].Total != 1 {
			t.Errorf("hostB per-host: got %+v, want 1/1", stats.PerHost["hostB"])
		}
	})

	t.Run("multiple devices on one host with mixed outcomes", func(t *testing.T) {
		// hostA tests three distinct devices: dev1 passes, dev2 fails, dev3 passes.
		batchData := BatchData{
			0: {"hostA": pass(dev1)},
			1: {"hostA": fail(dev2)},
			2: {"hostA": pass(dev3)},
		}
		stats := ComputeFailureStats(batchData)

		wantDevices := []DeviceTestResult{
			{DeviceCode: "dev1", DevicePubkey: "pk1", Success: true},
			{DeviceCode: "dev2", DevicePubkey: "pk2", Success: false},
			{DeviceCode: "dev3", DevicePubkey: "pk3", Success: true},
		}
		if !reflect.DeepEqual(stats.DeviceResults, wantDevices) {
			t.Errorf("device results: got %+v, want %+v", stats.DeviceResults, wantDevices)
		}
		host := stats.PerHost["hostA"]
		if host.Total != 3 || host.Failed != 1 {
			t.Errorf("per-host: got total=%d failed=%d, want 3/1", host.Total, host.Failed)
		}
		if !reflect.DeepEqual(host.FailedDevices, []string{"dev2"}) {
			t.Errorf("failedDevices: got %v, want [dev2]", host.FailedDevices)
		}
		if len(stats.Retests) != 0 {
			t.Errorf("expected no retests, got %+v", stats.Retests)
		}
	})

	t.Run("multiple hosts and devices with sorted retests", func(t *testing.T) {
		// hostA: dev1 attempted twice (one pass, one fail) — should be success.
		// hostB: dev2 attempted twice, both fail — should be failure.
		// hostA also tests dev3 once, passes.
		batchData := BatchData{
			0: {"hostA": pass(dev1), "hostB": fail(dev2)},
			1: {"hostA": fail(dev1), "hostB": fail(dev2)},
			2: {"hostA": pass(dev3), "hostB": fail(dev2)},
		}
		stats := ComputeFailureStats(batchData)

		wantDevices := []DeviceTestResult{
			{DeviceCode: "dev1", DevicePubkey: "pk1", Success: true},
			{DeviceCode: "dev2", DevicePubkey: "pk2", Success: false},
			{DeviceCode: "dev3", DevicePubkey: "pk3", Success: true},
		}
		if !reflect.DeepEqual(stats.DeviceResults, wantDevices) {
			t.Errorf("device results: got %+v, want %+v", stats.DeviceResults, wantDevices)
		}
		hostA := stats.PerHost["hostA"]
		if hostA.Total != 2 || hostA.Failed != 0 {
			t.Errorf("hostA: got %+v, want total=2 failed=0", hostA)
		}
		hostB := stats.PerHost["hostB"]
		if hostB.Total != 1 || hostB.Failed != 1 {
			t.Errorf("hostB: got %+v, want total=1 failed=1", hostB)
		}
		if !reflect.DeepEqual(hostB.FailedDevices, []string{"dev2"}) {
			t.Errorf("hostB failedDevices: got %v, want [dev2]", hostB.FailedDevices)
		}
		wantRetests := []DeviceRetest{
			{Host: "hostA", DeviceCode: "dev1", Attempts: 2, Successes: 1, Failures: 1},
			{Host: "hostB", DeviceCode: "dev2", Attempts: 3, Successes: 0, Failures: 3},
		}
		if !reflect.DeepEqual(stats.Retests, wantRetests) {
			t.Errorf("retests: got %+v, want %+v", stats.Retests, wantRetests)
		}
	})
}
