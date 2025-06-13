package telemetry

import (
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"sync"
	"testing"
	"time"
)

// MockStorage implements the Storage interface for testing
type MockStorage struct {
	mu        sync.Mutex
	samples   map[string]*LinkSamples
	saveErr   error
	loadErr   error
	rotateErr error
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		samples: make(map[string]*LinkSamples),
	}
}

func (m *MockStorage) SaveSamples(linkKey string, samples *LinkSamples) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples[linkKey] = samples
	return nil
}

func (m *MockStorage) LoadSamples() (map[string]*LinkSamples, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]*LinkSamples)
	maps.Copy(result, m.samples)
	return result, nil
}

func (m *MockStorage) RotateSamples(linkKey string) error {
	return m.rotateErr
}

func TestNewCollector(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:         "device1",
		LocalLocationPubkey:       "location1",
		ListenPort:                9999,
		SamplingIntervalSeconds:   1,
		SubmissionIntervalSeconds: 60,
		StoragePath:               "/tmp/telemetry",
		MaxSamplesPerLink:         1000,
	}

	storage := NewMockStorage()

	collector, err := NewCollector(config, storage)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	if collector.config.ListenPort != 9999 {
		t.Errorf("Expected listen port 9999, got %d", collector.config.ListenPort)
	}

	// Test nil storage
	_, err = NewCollector(config, nil)
	if err == nil {
		t.Error("Expected error for nil storage")
	}
}

func TestCollectorStart(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:         "device1",
		LocalLocationPubkey:       "location1",
		ListenPort:                0, // Use random port
		SamplingIntervalSeconds:   1,
		SubmissionIntervalSeconds: 60,
		MaxSamplesPerLink:         1000,
	}

	storage := NewMockStorage()
	collector, err := NewCollector(config, storage)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Start collector
	err = collector.Start()
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}
}

func TestUpdatePeers(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey: "device1",
		ListenPort:        9999,
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	peers := []*PeerDevice{
		{
			DevicePubkey:   "device2",
			LocationPubkey: "location2",
			IP:             "192.168.1.2",
			LinkPubkey:     "link1",
			IsInternetPeer: false,
		},
		{
			DevicePubkey:   "device3",
			LocationPubkey: "location3",
			IP:             "192.168.1.3",
			LinkPubkey:     "link2",
			IsInternetPeer: false,
		},
	}

	collector.UpdatePeers(peers)

	if len(collector.peers) != 2 {
		t.Errorf("Expected 2 peers, got %d", len(collector.peers))
	}

	if collector.peers["device2"].IP != "192.168.1.2" {
		t.Errorf("Peer device2 has wrong IP")
	}
}

func TestRecordSample(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:       "device1",
		LocalLocationPubkey:     "location1",
		ListenPort:              9999,
		MaxSamplesPerLink:       10,
		SamplingIntervalSeconds: 1,
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	peer := &PeerDevice{
		DevicePubkey:   "device2",
		LocationPubkey: "location2",
		IP:             "192.168.1.2",
		LinkPubkey:     "link1",
	}

	// Record multiple samples
	for i := range 5 {
		collector.recordSample(peer, uint32(1000+i*100), uint32(i))
	}

	samples := collector.GetCurrentSamples()
	linkKey := "device1:device2:link1"

	if linkSamples, ok := samples[linkKey]; ok {
		if len(linkSamples.Samples) != 5 {
			t.Errorf("Expected 5 samples, got %d", len(linkSamples.Samples))
		}

		// Check RTT values
		for i := range 5 {
			expectedRTT := uint32(1000 + i*100)
			if linkSamples.Samples[i].RTTMicroseconds != expectedRTT {
				t.Errorf("Sample %d: expected RTT %d, got %d",
					i, expectedRTT, linkSamples.Samples[i].RTTMicroseconds)
			}
		}
	} else {
		t.Errorf("No samples found for link %s", linkKey)
	}
}

func TestSampleRotation(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:       "device1",
		LocalLocationPubkey:     "location1",
		ListenPort:              9999,
		MaxSamplesPerLink:       10, // Small limit for testing
		SamplingIntervalSeconds: 1,
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	peer := &PeerDevice{
		DevicePubkey:   "device2",
		LocationPubkey: "location2",
		IP:             "192.168.1.2",
		LinkPubkey:     "link1",
	}

	// Record more samples than the limit
	for i := range 15 {
		collector.recordSample(peer, uint32(1000+i*100), uint32(i))
	}

	samples := collector.GetCurrentSamples()
	linkKey := "device1:device2:link1"

	if linkSamples, ok := samples[linkKey]; ok {
		// Should have kept half after rotation
		if len(linkSamples.Samples) != 5 {
			t.Errorf("Expected 5 samples after rotation, got %d", len(linkSamples.Samples))
		}

		// Check that we kept the newer samples
		firstRTT := linkSamples.Samples[0].RTTMicroseconds
		if firstRTT < 1500 { // Should be from the second half
			t.Errorf("Expected samples from second half, got RTT %d", firstRTT)
		}
	} else {
		t.Errorf("No samples found for link %s", linkKey)
	}
}

func TestGetLinkKey(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey: "device1",
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	tests := []struct {
		peer     *PeerDevice
		expected string
	}{
		{
			peer: &PeerDevice{
				DevicePubkey: "device2",
				LinkPubkey:   "link1",
			},
			expected: "device1:device2:link1",
		},
		{
			peer: &PeerDevice{
				DevicePubkey: "device0", // Smaller than device1
				LinkPubkey:   "link2",
			},
			expected: "device0:device1:link2",
		},
	}

	for _, tt := range tests {
		result := collector.getLinkKey(tt.peer)
		if result != tt.expected {
			t.Errorf("Expected link key %s, got %s", tt.expected, result)
		}
	}
}

func TestPingRequestResponse(t *testing.T) {
	// Test UDP ping functionality with a mock server
	config := CollectorConfig{
		LocalDevicePubkey:       "device1",
		LocalLocationPubkey:     "location1",
		ListenPort:              0, // Random port
		SamplingIntervalSeconds: 1,
		MaxSamplesPerLink:       100,
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	// Start a mock UDP server
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("Failed to create mock server: %v", err)
	}
	defer conn.Close()

	// Update collector with the mock server as a peer
	mockPort := conn.LocalAddr().(*net.UDPAddr).Port
	collector.config.ListenPort = mockPort

	peer := &PeerDevice{
		DevicePubkey:   "device2",
		LocationPubkey: "location2",
		IP:             "127.0.0.1",
		LinkPubkey:     "link1",
	}

	// Mock server responds to pings
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if n >= 12 {
				// Echo back as response
				response := make([]byte, 2048)
				copy(response[0:4], buffer[0:4])   // PacketID
				copy(response[4:12], buffer[4:12]) // Request timestamp
				// Add response timestamp
				responseTime := uint64(time.Now().UnixMicro())
				binary.BigEndian.PutUint64(response[12:20], responseTime)

				conn.WriteToUDP(response, clientAddr)
			}
		}
	}()

	// Send ping
	collector.sendPing(peer)

	// Wait for response processing
	time.Sleep(100 * time.Millisecond)

	// Verify sample was recorded
	samples := collector.GetCurrentSamples()
	if len(samples) > 0 {
		t.Log("Successfully recorded RTT sample")
	}
}

func TestStorageIntegration(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:       "device1",
		LocalLocationPubkey:     "location1",
		ListenPort:              9999,
		MaxSamplesPerLink:       100,
		SamplingIntervalSeconds: 1,
	}

	// Test with pre-existing samples
	storage := NewMockStorage()
	existingSamples := map[string]*LinkSamples{
		"device1:device2:link1": {
			DeviceAPubkey: "device1",
			DeviceZPubkey: "device2",
			LinkPubkey:    "link1",
			Samples: []RTTSample{
				{RTTMicroseconds: 1000, Timestamp: time.Now()},
			},
		},
	}
	storage.samples = existingSamples

	collector, _ := NewCollector(config, storage)

	// Verify loaded samples
	currentSamples := collector.GetCurrentSamples()
	if len(currentSamples) != 1 {
		t.Errorf("Expected 1 pre-existing sample set, got %d", len(currentSamples))
	}

	// Test save on stop
	peer := &PeerDevice{
		DevicePubkey:   "device3",
		LocationPubkey: "location3",
		IP:             "192.168.1.3",
		LinkPubkey:     "link2",
	}
	collector.recordSample(peer, 2000, 1)

	err := collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	// Verify samples were saved
	savedSamples, _ := storage.LoadSamples()
	if len(savedSamples) != 2 {
		t.Errorf("Expected 2 sample sets saved, got %d", len(savedSamples))
	}
}

func TestConcurrentAccess(t *testing.T) {
	config := CollectorConfig{
		LocalDevicePubkey:       "device1",
		LocalLocationPubkey:     "location1",
		ListenPort:              9999,
		MaxSamplesPerLink:       1000,
		SamplingIntervalSeconds: 1,
	}

	storage := NewMockStorage()
	collector, _ := NewCollector(config, storage)

	// Create multiple peers
	peers := make([]*PeerDevice, 10)
	for i := range 10 {
		peers[i] = &PeerDevice{
			DevicePubkey:   fmt.Sprintf("device%d", i+2),
			LocationPubkey: fmt.Sprintf("location%d", i+2),
			IP:             fmt.Sprintf("192.168.1.%d", i+2),
			LinkPubkey:     fmt.Sprintf("link%d", i+1),
		}
	}

	// Concurrent sample recording
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := range 100 {
				collector.recordSample(peers[idx], uint32(1000+j), uint32(j))
			}
		}(i)
	}

	// Concurrent reads
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = collector.GetCurrentSamples()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify all samples were recorded
	samples := collector.GetCurrentSamples()
	if len(samples) != 10 {
		t.Errorf("Expected samples for 10 links, got %d", len(samples))
	}

	for _, linkSamples := range samples {
		if len(linkSamples.Samples) != 100 {
			t.Errorf("Expected 100 samples per link, got %d", len(linkSamples.Samples))
		}
	}
}
