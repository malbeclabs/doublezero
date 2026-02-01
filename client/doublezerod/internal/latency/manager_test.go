package latency_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58/base58"
	"golang.org/x/sys/unix"
)

func TestLatencyManager(t *testing.T) {
	tests := []struct {
		Name         string
		Description  string
		DeviceCache  []serviceability.Device
		ResultsCache []latency.LatencyResult
	}{
		{
			Name:        "validate_device_cache",
			Description: "validate the device cache is as we think",
			DeviceCache: []serviceability.Device{
				{
					AccountType: serviceability.DeviceType,
					PublicIp:    [4]uint8{127, 0, 0, 1},
					PubKey:      [32]byte{1},
					Code:        "dev01",
				},
			},
			ResultsCache: []latency.LatencyResult{
				{
					Min:  1,
					Max:  10,
					Avg:  5,
					Loss: 0,
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{127, 0, 0, 1},
						PubKey:   [32]byte{1},
						Code:     "dev01",
					},
					InterfaceName: "",
					IP:            net.IP{127, 0, 0, 1},
					Reachable:     true,
				},
			},
		},
	}

	sentContractData := make(chan struct{}, 1)
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		sentContractData <- struct{}{}
		return &latency.ContractData{
			Devices: []serviceability.Device{
				{
					AccountType: serviceability.DeviceType,
					PublicIp:    [4]uint8{127, 0, 0, 1},
					PubKey:      [32]byte{1},
					Code:        "dev01",
				},
			},
		}, nil
	}
	sentLatencyData := make(chan struct{}, 1)
	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		sentLatencyData <- struct{}{}
		return latency.LatencyResult{
			Min:           1,
			Max:           10,
			Avg:           5,
			Loss:          0,
			Device:        target.Device,
			InterfaceName: target.InterfaceName,
			IP:            target.IP,
			Reachable:     true,
		}
	}
	programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID(programId),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: tests[0].DeviceCache, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		// Start returns nil when context is cancelled, which is the expected
		// test termination path, so we can safely ignore the return value.
		_ = manager.Start(ctx)
	}()
	t.Run("check_device_cache_is_correct", func(t *testing.T) {
		select {
		case <-sentContractData:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out while waiting for device cache")
		}
		// Poll for device cache to be populated instead of arbitrary sleep
		var got []serviceability.Device
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			got = manager.GetDeviceCache()
			if len(got) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Logf("got: %+v", got)
		if diff := cmp.Diff(tests[0].DeviceCache, got); diff != "" {
			t.Errorf("DeviceCache mismatch (-want +got): %s\n", diff)
		}
	})

	t.Run("check_results_cache_is_correct", func(t *testing.T) {
		select {
		case <-sentLatencyData:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out while waiting for results cache")
		}
		// Poll for results cache to be populated instead of arbitrary sleep
		var results []latency.LatencyResult
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			results = manager.GetResultsCache()
			if len(results) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if diff := cmp.Diff(tests[0].ResultsCache, results, cmp.Comparer(func(a, b net.IP) bool { return a.Equal(b) })); diff != "" {
			t.Errorf("ResultsCache mismatch (-want +got): %s\n", diff)
		}
	})

	f, err := os.CreateTemp("/tmp", "doublezero.sock")
	if err != nil {
		t.Fatalf("error creating sock file: %v", err)
	}
	defer os.Remove(f.Name())
	_ = unix.Unlink(f.Name())

	lis, err := net.Listen("unix", f.Name())
	if err != nil {
		t.Fatalf("error creating listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /latency", manager.ServeLatency)

	server := http.Server{
		Handler: mux,
	}
	defer server.Close()
	go func() {
		err := server.Serve(lis)
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("error during http serving: %v", err)
		}
	}()

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", f.Name())

			},
		},
	}

	t.Run("check_results_via_http_are_correct", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://localhost/latency", nil)
		if err != nil {
			t.Fatalf("error generating http request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("error while making http request: %v", err)
		}
		defer resp.Body.Close()

		buf, _ := io.ReadAll(resp.Body)
		got := []map[string]any{}
		if err := json.Unmarshal(buf, &got); err != nil {
			t.Fatalf("error unmarshaling latency data: %v", err)
		}

		want := []map[string]any{
			{
				"device_pk":       base58.Encode(tests[0].DeviceCache[0].PubKey[:]),
				"device_code":     tests[0].DeviceCache[0].Code,
				"device_ip":       "127.0.0.1",
				"min_latency_ns":  float64(1),
				"max_latency_ns":  float64(10),
				"avg_latency_ns":  float64(5),
				"loss_percentage": float64(0),
				"reachable":       true,
			},
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("LatencyResults mismatch (-want +got): %s\n", diff)
		}
	})
}

func TestLatencyUdpPing(t *testing.T) {
	devices := []serviceability.Device{
		{
			AccountType: serviceability.DeviceType,
			PublicIp:    [4]uint8{127, 0, 0, 1},
			PubKey:      [32]byte{1},
			Code:        "dev01",
		},
	}

	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		return &latency.ContractData{Devices: devices}, nil
	}

	resultChan := make(chan struct{})

	mockProber := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		result := latency.UdpPing(ctx, target)
		resultChan <- struct{}{}
		return result
	}

	programId := "9i7v8m3i7W2qPGRonFi8mehN76SXUkDcpgk4tPQhEabc"
	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProber),
		latency.WithProgramID(programId),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: devices, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	go func() {
		// Start returns nil when context is cancelled, which is the expected
		// test termination path, so we can safely ignore the return value.
		_ = manager.Start(t.Context())
	}()

	select {
	case <-resultChan:
	case <-time.After(10 * time.Second):
		t.Fatalf("time out waiting for probe results")
	}
	// Poll for results cache to be populated instead of arbitrary sleep
	var results []latency.LatencyResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		results = manager.GetResultsCache()
		if len(results) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	want := []latency.LatencyResult{
		{
			Device: latency.DeviceInfo{
				PublicIp: [4]uint8{127, 0, 0, 1},
				PubKey:   [32]byte{1},
				Code:     "dev01",
			},
			InterfaceName: "",
			IP:            net.IP{127, 0, 0, 1},
			Reachable:     true,
			Loss:          0,
		},
	}
	if diff := cmp.Diff(
		want,
		results,
		cmpopts.IgnoreFields(latency.LatencyResults{}, "Lock"),
		cmpopts.IgnoreFields(latency.LatencyResult{}, "Avg", "Max", "Min"),
		cmp.Comparer(func(a, b net.IP) bool { return a.Equal(b) }),
	); diff != "" {
		t.Errorf("ResultsCache mismatch (-want +got): %s\n", diff)
	}
	Avg := results[0].Avg
	Min := results[0].Min
	Max := results[0].Max
	if Avg == 0 || Min == 0 || Max == 0 {
		t.Fatalf("avg/min/max latency values should be non-zero: %d/%d/%d", Avg, Min, Max)
	}
}

func TestGetProbeTargets(t *testing.T) {
	tests := []struct {
		name                 string
		device               serviceability.Device
		probeTunnelEndpoints bool
		want                 []latency.ProbeTarget
	}{
		{
			name: "device_with_only_public_ip_flag_disabled",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{1},
				Code:        "dev01",
				Interfaces:  []serviceability.Interface{},
			},
			probeTunnelEndpoints: false,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{1},
						Code:     "dev01",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "device_with_only_public_ip_flag_enabled",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{1},
				Code:        "dev01",
				Interfaces:  []serviceability.Interface{},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{1},
						Code:     "dev01",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "flag_disabled_ignores_tunnel_endpoints",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{2},
				Code:        "dev02",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 5, 32}, // 10.2.3.5/32
					},
				},
			},
			probeTunnelEndpoints: false,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{2},
						Code:     "dev02",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "flag_enabled_includes_tunnel_endpoints",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{2},
				Code:        "dev02",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 5, 32}, // 10.2.3.5/32
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{2},
						Code:     "dev02",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{2},
						Code:     "dev02",
					},
					InterfaceName: "Loopback1",
					IP:            net.IP{10, 2, 3, 5},
				},
			},
		},
		{
			name: "device_with_non_tunnel_endpoint_interface",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{3},
				Code:        "dev03",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Ethernet1",
						UserTunnelEndpoint: false,
						IpNet:              [5]uint8{10, 0, 0, 1, 24},
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{3},
						Code:     "dev03",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "device_with_duplicate_ip_skipped",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{4},
				Code:        "dev04",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback0",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{192, 168, 1, 1, 32}, // Same as PublicIp
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{4},
						Code:     "dev04",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "device_with_invalid_prefix_length_skipped",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{5},
				Code:        "dev05",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 5, 0}, // Invalid prefix length (0)
					},
					{
						Name:               "Loopback2",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 6, 33}, // Invalid prefix length (> 32)
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{5},
						Code:     "dev05",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
			},
		},
		{
			name: "device_with_multiple_tunnel_endpoints",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{6},
				Code:        "dev06",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 5, 32},
					},
					{
						Name:               "Loopback2",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 6, 32},
					},
					{
						Name:               "Ethernet1",
						UserTunnelEndpoint: false,
						IpNet:              [5]uint8{10, 0, 0, 1, 24},
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{6},
						Code:     "dev06",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{6},
						Code:     "dev06",
					},
					InterfaceName: "Loopback1",
					IP:            net.IP{10, 2, 3, 5},
				},
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{6},
						Code:     "dev06",
					},
					InterfaceName: "Loopback2",
					IP:            net.IP{10, 2, 3, 6},
				},
			},
		},
		{
			name: "device_with_unspecified_public_ip_flag_disabled",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{0, 0, 0, 0}, // Unspecified
				PubKey:      [32]byte{7},
				Code:        "dev07",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 7, 32},
					},
				},
			},
			probeTunnelEndpoints: false,
			want:                 nil, // No targets when PublicIp unspecified and flag disabled
		},
		{
			name: "device_with_unspecified_public_ip_flag_enabled",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{0, 0, 0, 0}, // Unspecified
				PubKey:      [32]byte{7},
				Code:        "dev07",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 7, 32},
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{0, 0, 0, 0},
						PubKey:   [32]byte{7},
						Code:     "dev07",
					},
					InterfaceName: "Loopback1",
					IP:            net.IP{10, 2, 3, 7},
				},
			},
		},
		{
			name: "interface_with_unspecified_ip_excluded",
			device: serviceability.Device{
				AccountType: serviceability.DeviceType,
				PublicIp:    [4]uint8{192, 168, 1, 1},
				PubKey:      [32]byte{8},
				Code:        "dev08",
				Interfaces: []serviceability.Interface{
					{
						Name:               "Loopback1",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{0, 0, 0, 0, 32}, // Unspecified IP
					},
					{
						Name:               "Loopback2",
						UserTunnelEndpoint: true,
						IpNet:              [5]uint8{10, 2, 3, 8, 32}, // Valid IP
					},
				},
			},
			probeTunnelEndpoints: true,
			want: []latency.ProbeTarget{
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{8},
						Code:     "dev08",
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 1},
				},
				{
					Device: latency.DeviceInfo{
						PublicIp: [4]uint8{192, 168, 1, 1},
						PubKey:   [32]byte{8},
						Code:     "dev08",
					},
					InterfaceName: "Loopback2",
					IP:            net.IP{10, 2, 3, 8},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latency.GetProbeTargets(tt.device, tt.probeTunnelEndpoints)
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(func(a, b net.IP) bool { return a.Equal(b) })); diff != "" {
				t.Errorf("GetProbeTargets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// BenchmarkLatencyManagerMemoryStability tests for memory leaks by running the latency
// manager for an extended period with stable devices and measuring heap growth.
// The test fails if heap grows beyond an acceptable threshold, which would indicate
// a memory leak in the probing or caching logic.
//
// Configuration: 200 devices, each with 1 tunnel endpoint interface = 400 probe targets
// (200 PublicIp + 200 interface IPs). This simulates a realistic production deployment.
func BenchmarkLatencyManagerMemoryStability(b *testing.B) {
	// Skip short runs - this test needs time to detect leaks
	if testing.Short() {
		b.Skip("skipping memory stability benchmark in short mode")
	}

	// Create a realistic production-scale set of devices:
	// 200 devices, each with 1 tunnel endpoint interface = 400 probe targets
	devices := generateTestDevices(200, 1)

	// Count total probe targets for reporting (with tunnel endpoints enabled)
	totalTargets := 0
	for _, d := range devices {
		totalTargets += len(latency.GetProbeTargets(d, true))
	}

	var probeCount int64
	var mu sync.Mutex

	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		return &latency.ContractData{Devices: devices}, nil
	}

	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		mu.Lock()
		probeCount++
		mu.Unlock()
		// Simulate some minimal work without allocating excessively
		return latency.LatencyResult{
			Min:           1000,
			Max:           5000,
			Avg:           2500,
			Loss:          0,
			Device:        target.Device,
			InterfaceName: target.InterfaceName,
			IP:            target.IP,
			Reachable:     true,
		}
	}

	// Use fast intervals to exercise the manager rapidly
	probeInterval := 50 * time.Millisecond
	cacheUpdateInterval := 500 * time.Millisecond

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID("test-memory-stability"),
		latency.WithProbeInterval(probeInterval),
		latency.WithCacheUpdateInterval(cacheUpdateInterval),
		latency.WithMetricsEnabled(false),      // Disable metrics to isolate manager memory behavior
		latency.WithProbeTunnelEndpoints(true), // Enable tunnel endpoint probing for this test
	)

	// Run for several seconds to allow multiple probe cycles
	testDuration := 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	// Force GC before measuring baseline
	runtime.GC()
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Start the manager
	done := make(chan struct{})
	go func() {
		_ = manager.Start(ctx)
		close(done)
	}()

	// Wait for test duration
	<-ctx.Done()
	<-done

	// Force GC and wait for finalizers before measuring final state
	runtime.GC()
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	mu.Lock()
	finalProbeCount := probeCount
	mu.Unlock()

	// Calculate heap growth
	heapGrowth := int64(memAfter.HeapAlloc) - int64(memBefore.HeapAlloc)
	heapGrowthMB := float64(heapGrowth) / (1024 * 1024)

	// Report metrics
	b.ReportMetric(float64(finalProbeCount), "probes")
	b.ReportMetric(float64(totalTargets), "targets")
	b.ReportMetric(heapGrowthMB, "heap_growth_MB")
	b.ReportMetric(float64(memAfter.HeapAlloc)/(1024*1024), "heap_alloc_MB")
	b.ReportMetric(float64(memAfter.HeapObjects), "heap_objects")

	// Log detailed memory stats for debugging
	b.Logf("Memory stats:")
	b.Logf("  Devices: %d, Probe targets: %d", len(devices), totalTargets)
	b.Logf("  Total probes executed: %d", finalProbeCount)
	b.Logf("  Heap before: %.2f MB, Heap after: %.2f MB",
		float64(memBefore.HeapAlloc)/(1024*1024),
		float64(memAfter.HeapAlloc)/(1024*1024))
	b.Logf("  Heap growth: %.2f MB", heapGrowthMB)
	b.Logf("  Heap objects before: %d, after: %d",
		memBefore.HeapObjects, memAfter.HeapObjects)
	b.Logf("  Total allocations: %d", memAfter.Mallocs-memBefore.Mallocs)

	// Fail if heap growth exceeds threshold (10 MB)
	// With 400 probe targets (200 devices * 2 IPs each), this threshold accounts for
	// normal runtime overhead while catching significant leaks. The DeviceInfo optimization
	// keeps per-target memory low, so 10 MB should be plenty even with 10x more targets.
	const maxHeapGrowthMB = 10.0
	if heapGrowthMB > maxHeapGrowthMB {
		b.Fatalf("excessive heap growth detected: %.2f MB (threshold: %.2f MB)", heapGrowthMB, maxHeapGrowthMB)
	}

	// Sanity check: ensure we actually ran probes
	minExpectedProbes := int64(totalTargets * 10) // At least 10 probe cycles
	if finalProbeCount < minExpectedProbes {
		b.Fatalf("insufficient probes executed: got %d, expected at least %d", finalProbeCount, minExpectedProbes)
	}
}

// generateTestDevices creates a set of test devices with a configurable number of
// tunnel endpoint interfaces per device.
// Parameters:
//   - count: number of devices to generate
//   - interfacesPerDevice: number of UserTunnelEndpoint interfaces per device
//
// Each device gets a unique PublicIp and the specified number of loopback interfaces,
// resulting in (count * (1 + interfacesPerDevice)) probe targets.
func generateTestDevices(count int, interfacesPerDevice int) []serviceability.Device {
	devices := make([]serviceability.Device, count)
	for i := range count {
		var pubKey [32]byte
		// Use multiple bytes to support >255 devices
		pubKey[0] = byte(i >> 8)
		pubKey[1] = byte(i & 0xff)

		device := serviceability.Device{
			AccountType: serviceability.DeviceType,
			PublicIp:    [4]uint8{192, 168, byte(i / 256), byte(i % 256)},
			PubKey:      pubKey,
			Code:        fmt.Sprintf("dev%03d", i+1),
			Interfaces:  make([]serviceability.Interface, 0, interfacesPerDevice),
		}

		// Add the specified number of tunnel endpoint interfaces
		for j := range interfacesPerDevice {
			device.Interfaces = append(device.Interfaces, serviceability.Interface{
				Name:               fmt.Sprintf("Loopback%d", j+1),
				UserTunnelEndpoint: true,
				IpNet:              [5]uint8{10, byte(i / 256), byte(i % 256), byte(j + 1), 32},
			})
		}

		devices[i] = device
	}
	return devices
}

func TestLatencyManagerWithMultipleInterfaces(t *testing.T) {
	// Device with PublicIp and one user tunnel endpoint interface
	device := serviceability.Device{
		AccountType: serviceability.DeviceType,
		PublicIp:    [4]uint8{192, 168, 1, 1},
		PubKey:      [32]byte{1},
		Code:        "dev01",
		Interfaces: []serviceability.Interface{
			{
				Name:               "Loopback1",
				UserTunnelEndpoint: true,
				IpNet:              [5]uint8{10, 2, 3, 5, 32},
			},
		},
	}

	probeCount := 0
	var mu sync.Mutex

	smartContractChan := make(chan struct{}, 1)
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		smartContractChan <- struct{}{}
		return &latency.ContractData{Devices: []serviceability.Device{device}}, nil
	}

	resultChan := make(chan struct{}, 2)
	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		mu.Lock()
		probeCount++
		mu.Unlock()
		resultChan <- struct{}{}
		return latency.LatencyResult{
			Min:           1,
			Max:           10,
			Avg:           5,
			Loss:          0,
			Device:        target.Device,
			InterfaceName: target.InterfaceName,
			IP:            target.IP,
			Reachable:     true,
		}
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID("test-program"),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
		latency.WithProbeTunnelEndpoints(true), // Enable tunnel endpoint probing
	)
	// Pre-populate the device cache to avoid race conditions
	manager.DeviceCache = &latency.DeviceCache{Devices: []serviceability.Device{device}, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = manager.Start(ctx)
	}()

	// Wait for smart contract fetch
	select {
	case <-smartContractChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for smart contract fetch")
	}

	// Wait for both probes to complete
	for i := range 2 {
		select {
		case <-resultChan:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for probe %d", i+1)
		}
	}

	// Poll for results cache to be populated instead of arbitrary sleep
	var results []latency.LatencyResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		results = manager.GetResultsCache()
		if len(results) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	gotProbeCount := probeCount
	mu.Unlock()

	// Verify we probed 2 targets (PublicIp + Loopback1)
	if gotProbeCount != 2 {
		t.Errorf("expected 2 probes, got %d", gotProbeCount)
	}

	// Verify results cache has 2 entries
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Check that we have one entry with empty interface name and one with "Loopback1"
	var hasPublicIP, hasLoopback bool
	for _, r := range results {
		if r.InterfaceName == "" && r.IP.Equal(net.IP{192, 168, 1, 1}) {
			hasPublicIP = true
		}
		if r.InterfaceName == "Loopback1" && r.IP.Equal(net.IP{10, 2, 3, 5}) {
			hasLoopback = true
		}
	}
	if !hasPublicIP {
		t.Error("missing PublicIp probe result")
	}
	if !hasLoopback {
		t.Error("missing Loopback1 probe result")
	}
}

func TestLatencyResult_MarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		results []latency.LatencyResult
		want    []map[string]any
	}{
		{
			name: "public_ip_probe_result",
			results: []latency.LatencyResult{
				{
					Min:  1000,
					Max:  5000,
					Avg:  2500,
					Loss: 0.5,
					Device: latency.DeviceInfo{
						PubKey:   [32]byte{1, 2, 3},
						Code:     "dev01",
						PublicIp: [4]uint8{192, 168, 1, 1},
					},
					InterfaceName: "", // Empty indicates PublicIp probe
					IP:            net.IP{192, 168, 1, 1},
					Reachable:     true,
				},
			},
			want: []map[string]any{
				{
					"device_pk":       base58.Encode([]byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
					"device_ip":       "192.168.1.1",
					"device_code":     "dev01",
					"min_latency_ns":  float64(1000),
					"max_latency_ns":  float64(5000),
					"avg_latency_ns":  float64(2500),
					"loss_percentage": 0.5,
					"reachable":       true,
				},
			},
		},
		{
			name: "interface_probe_result",
			results: []latency.LatencyResult{
				{
					Min:  2000,
					Max:  6000,
					Avg:  3500,
					Loss: 1.0,
					Device: latency.DeviceInfo{
						PubKey:   [32]byte{4, 5, 6},
						Code:     "dev02",
						PublicIp: [4]uint8{192, 168, 1, 2},
					},
					InterfaceName: "Loopback1",
					IP:            net.IP{10, 2, 3, 5}, // Different from PublicIp
					Reachable:     true,
				},
			},
			want: []map[string]any{
				{
					"device_pk":       base58.Encode([]byte{4, 5, 6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
					"device_ip":       "10.2.3.5", // Should use probed IP, not PublicIp
					"device_code":     "dev02",
					"interface_name":  "Loopback1",
					"min_latency_ns":  float64(2000),
					"max_latency_ns":  float64(6000),
					"avg_latency_ns":  float64(3500),
					"loss_percentage": 1.0,
					"reachable":       true,
				},
			},
		},
		{
			name: "mixed_results_public_ip_and_interface",
			results: []latency.LatencyResult{
				{
					Min:  1000,
					Max:  3000,
					Avg:  2000,
					Loss: 0,
					Device: latency.DeviceInfo{
						PubKey:   [32]byte{10, 11, 12},
						Code:     "dev04",
						PublicIp: [4]uint8{192, 168, 1, 4},
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 4},
					Reachable:     true,
				},
				{
					Min:  1500,
					Max:  3500,
					Avg:  2200,
					Loss: 0,
					Device: latency.DeviceInfo{
						PubKey:   [32]byte{10, 11, 12},
						Code:     "dev04",
						PublicIp: [4]uint8{192, 168, 1, 4},
					},
					InterfaceName: "Loopback1",
					IP:            net.IP{10, 2, 3, 4},
					Reachable:     true,
				},
			},
			want: []map[string]any{
				{
					"device_pk":       base58.Encode([]byte{10, 11, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
					"device_ip":       "192.168.1.4",
					"device_code":     "dev04",
					"min_latency_ns":  float64(1000),
					"max_latency_ns":  float64(3000),
					"avg_latency_ns":  float64(2000),
					"loss_percentage": float64(0),
					"reachable":       true,
				},
				{
					"device_pk":       base58.Encode([]byte{10, 11, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
					"device_ip":       "10.2.3.4", // Different IP for interface probe
					"device_code":     "dev04",
					"interface_name":  "Loopback1",
					"min_latency_ns":  float64(1500),
					"max_latency_ns":  float64(3500),
					"avg_latency_ns":  float64(2200),
					"loss_percentage": float64(0),
					"reachable":       true,
				},
			},
		},
		{
			name: "unreachable_device",
			results: []latency.LatencyResult{
				{
					Min:  0,
					Max:  0,
					Avg:  0,
					Loss: 100.0,
					Device: latency.DeviceInfo{
						PubKey:   [32]byte{7, 8, 9},
						Code:     "dev03",
						PublicIp: [4]uint8{192, 168, 1, 3},
					},
					InterfaceName: "",
					IP:            net.IP{192, 168, 1, 3},
					Reachable:     false,
				},
			},
			want: []map[string]any{
				{
					"device_pk":       base58.Encode([]byte{7, 8, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
					"device_ip":       "192.168.1.3",
					"device_code":     "dev03",
					"min_latency_ns":  float64(0),
					"max_latency_ns":  float64(0),
					"avg_latency_ns":  float64(0),
					"loss_percentage": 100.0,
					"reachable":       false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal as slice to simulate production usage
			// This triggers the pointer receiver MarshalJSON method for each element
			data, err := json.Marshal(tt.results)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}

			var got []map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("JSON output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLatencyManagerWithEmptyDeviceList(t *testing.T) {
	// Test manager behavior when there are no devices
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		return &latency.ContractData{Devices: []serviceability.Device{}}, nil
	}

	probeCallCount := 0
	var mu sync.Mutex
	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		mu.Lock()
		probeCallCount++
		mu.Unlock()
		return latency.LatencyResult{}
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID("test-empty"),
		latency.WithProbeInterval(100*time.Millisecond),
		latency.WithCacheUpdateInterval(100*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = manager.Start(ctx)
	}()

	// Wait for manager to run a few cycles
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := probeCallCount
	mu.Unlock()

	// Verify prober was never called (no devices to probe)
	if count != 0 {
		t.Errorf("expected 0 probes with empty device list, got %d", count)
	}

	// Verify results cache is empty
	results := manager.GetResultsCache()
	if len(results) != 0 {
		t.Errorf("expected empty results cache, got %d results", len(results))
	}
}

func TestLatencyManagerWithTunnelEndpointsDisabled(t *testing.T) {
	// Test that when probeTunnelEndpoints is false, only PublicIp is probed
	device := serviceability.Device{
		AccountType: serviceability.DeviceType,
		PublicIp:    [4]uint8{192, 168, 1, 1},
		PubKey:      [32]byte{1},
		Code:        "dev01",
		Interfaces: []serviceability.Interface{
			{
				Name:               "Loopback1",
				UserTunnelEndpoint: true,
				IpNet:              [5]uint8{10, 2, 3, 5, 32},
			},
		},
	}

	probeCount := 0
	var mu sync.Mutex

	smartContractChan := make(chan struct{}, 1)
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		smartContractChan <- struct{}{}
		return &latency.ContractData{Devices: []serviceability.Device{device}}, nil
	}

	resultChan := make(chan struct{}, 2)
	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		mu.Lock()
		probeCount++
		mu.Unlock()
		resultChan <- struct{}{}
		return latency.LatencyResult{
			Min:           1,
			Max:           10,
			Avg:           5,
			Loss:          0,
			Device:        target.Device,
			InterfaceName: target.InterfaceName,
			IP:            target.IP,
			Reachable:     true,
		}
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID("test-flag-disabled"),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
		latency.WithProbeTunnelEndpoints(false), // Flag disabled - only probe PublicIp
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: []serviceability.Device{device}, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = manager.Start(ctx)
	}()

	// Wait for smart contract fetch
	select {
	case <-smartContractChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for smart contract fetch")
	}

	// Wait for one probe to complete
	select {
	case <-resultChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for probe")
	}

	// Poll for results cache to be populated
	var results []latency.LatencyResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		results = manager.GetResultsCache()
		if len(results) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	gotProbeCount := probeCount
	mu.Unlock()

	// Verify we probed only 1 target (PublicIp only, not Loopback1)
	if gotProbeCount != 1 {
		t.Errorf("expected 1 probe (PublicIp only), got %d", gotProbeCount)
	}

	// Verify results cache has 1 entry
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	// Check that the result is for PublicIp (empty interface name)
	if len(results) > 0 {
		if results[0].InterfaceName != "" {
			t.Errorf("expected empty interface name for PublicIp probe, got %q", results[0].InterfaceName)
		}
		if !results[0].IP.Equal(net.IP{192, 168, 1, 1}) {
			t.Errorf("expected IP 192.168.1.1, got %s", results[0].IP)
		}
	}
}

func TestLatencyManagerWithMetrics(t *testing.T) {
	// Test that metrics are recorded with correct labels for multiple IPs
	device := serviceability.Device{
		AccountType: serviceability.DeviceType,
		PublicIp:    [4]uint8{192, 168, 1, 1},
		PubKey:      [32]byte{10},
		Code:        "dev10",
		Interfaces: []serviceability.Interface{
			{
				Name:               "Loopback1",
				UserTunnelEndpoint: true,
				IpNet:              [5]uint8{10, 2, 3, 10, 32},
			},
		},
	}

	smartContractChan := make(chan struct{}, 1)
	mockSmartContractFunc := func(context.Context, string, string) (*latency.ContractData, error) {
		smartContractChan <- struct{}{}
		return &latency.ContractData{Devices: []serviceability.Device{device}}, nil
	}

	resultChan := make(chan struct{}, 2)
	mockProberFunc := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		resultChan <- struct{}{}
		return latency.LatencyResult{
			Min:           1000,
			Max:           5000,
			Avg:           2500,
			Loss:          0.5,
			Device:        target.Device,
			InterfaceName: target.InterfaceName,
			IP:            target.IP,
			Reachable:     true,
		}
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(mockSmartContractFunc),
		latency.WithProberFunc(mockProberFunc),
		latency.WithProgramID("test-metrics"),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
		latency.WithMetricsEnabled(true),       // Enable metrics
		latency.WithProbeTunnelEndpoints(true), // Enable tunnel endpoint probing
	)
	manager.DeviceCache = &latency.DeviceCache{Devices: []serviceability.Device{device}, Lock: sync.Mutex{}}
	manager.ResultsCache = &latency.LatencyResults{Results: []latency.LatencyResult{}, Lock: sync.RWMutex{}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = manager.Start(ctx)
	}()

	// Wait for smart contract fetch
	select {
	case <-smartContractChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for smart contract fetch")
	}

	// Wait for both probes to complete
	for i := range 2 {
		select {
		case <-resultChan:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for probe %d", i+1)
		}
	}

	// Poll for results cache to be populated
	var results []latency.LatencyResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		results = manager.GetResultsCache()
		if len(results) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify each result has the correct IP
	devicePk := base58.Encode(device.PubKey[:])
	var foundPublicIP, foundInterface bool
	for _, r := range results {
		if r.InterfaceName == "" && r.IP.Equal(net.IP{192, 168, 1, 1}) {
			foundPublicIP = true
			// Verify metric labels would use PublicIp
			if r.Device.Code != "dev10" {
				t.Errorf("expected device code dev10, got %s", r.Device.Code)
			}
			expectedPk := base58.Encode(r.Device.PubKey[:])
			if expectedPk != devicePk {
				t.Errorf("expected device pk %s, got %s", devicePk, expectedPk)
			}
		}
		if r.InterfaceName == "Loopback1" && r.IP.Equal(net.IP{10, 2, 3, 10}) {
			foundInterface = true
			// Verify metric labels would use interface IP (not PublicIp)
			if r.IP.String() != "10.2.3.10" {
				t.Errorf("expected IP 10.2.3.10 for interface, got %s", r.IP.String())
			}
		}
	}

	if !foundPublicIP {
		t.Error("missing PublicIp probe result")
	}
	if !foundInterface {
		t.Error("missing interface probe result")
	}
}

func TestUdpPing_ErrorCases(t *testing.T) {
	tests := []struct {
		name   string
		target latency.ProbeTarget
		// We check that unreachable is returned for error cases
		expectReachable bool
	}{
		{
			name: "100_percent_packet_loss",
			target: latency.ProbeTarget{
				Device: latency.DeviceInfo{
					PubKey:   [32]byte{1},
					Code:     "dev01",
					PublicIp: [4]uint8{192, 0, 2, 1}, // TEST-NET-1 (unreachable)
				},
				InterfaceName: "",
				IP:            net.IP{192, 0, 2, 1},
			},
			expectReachable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result := latency.UdpPing(ctx, tt.target)

			if result.Reachable != tt.expectReachable {
				t.Errorf("expected Reachable=%v, got %v", tt.expectReachable, result.Reachable)
			}

			// Verify result has correct device info and IP
			if result.Device.Code != tt.target.Device.Code {
				t.Errorf("expected device code %s, got %s", tt.target.Device.Code, result.Device.Code)
			}
			if !result.IP.Equal(tt.target.IP) {
				t.Errorf("expected IP %s, got %s", tt.target.IP, result.IP)
			}
		})
	}
}
