package qa

import (
	"fmt"
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
