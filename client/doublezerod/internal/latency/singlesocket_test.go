package latency_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

func TestSingleSocketPing_Localhost(t *testing.T) {
	// Requires raw socket privileges (same as TestLatencyUdpPing).
	targets := []latency.ProbeTarget{
		{
			Device: latency.DeviceInfo{
				PubKey:   [32]byte{1},
				Code:     "dev01",
				PublicIp: [4]uint8{127, 0, 0, 1},
			},
			IP: net.IP{127, 0, 0, 1},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := latency.SingleSocketPing(ctx, targets)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if !r.Reachable {
		t.Fatalf("expected localhost to be reachable, got reachable=%v (may need sudo)", r.Reachable)
	}
	if r.Avg <= 0 {
		t.Errorf("expected positive avg latency, got %d", r.Avg)
	}
	if r.Min <= 0 {
		t.Errorf("expected positive min latency, got %d", r.Min)
	}
	if r.Max <= 0 {
		t.Errorf("expected positive max latency, got %d", r.Max)
	}
	if r.Min > r.Avg || r.Avg > r.Max {
		t.Errorf("expected min <= avg <= max, got %d <= %d <= %d", r.Min, r.Avg, r.Max)
	}
	if r.Loss != 0 {
		t.Errorf("expected 0%% packet loss to localhost, got %.1f%%", r.Loss)
	}
}

func TestSingleSocketPing_Unreachable(t *testing.T) {
	// 192.0.2.1 is TEST-NET-1 (RFC 5737), should be unreachable.
	targets := []latency.ProbeTarget{
		{
			Device: latency.DeviceInfo{
				PubKey:   [32]byte{2},
				Code:     "unreachable",
				PublicIp: [4]uint8{192, 0, 2, 1},
			},
			IP: net.IP{192, 0, 2, 1},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	results := latency.SingleSocketPing(ctx, targets)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Reachable {
		t.Errorf("expected unreachable target, got reachable=true")
	}
	if r.Loss != 100 {
		t.Errorf("expected 100%% packet loss, got %.1f%%", r.Loss)
	}
}

func TestSingleSocketPing_Empty(t *testing.T) {
	results := latency.SingleSocketPing(context.Background(), nil)
	if results != nil {
		t.Errorf("expected nil for empty targets, got %v", results)
	}
}

func TestLatencyManager_WithBatchProber(t *testing.T) {
	// Verify the manager uses BatchProberFunc when set, instead of per-target ProberFunc.
	var perTargetCalled atomic.Bool
	var batchCalled atomic.Bool

	mockPerTarget := func(ctx context.Context, target latency.ProbeTarget) latency.LatencyResult {
		perTargetCalled.Store(true)
		return latency.LatencyResult{Device: target.Device, IP: target.IP, Reachable: true}
	}

	mockBatch := func(ctx context.Context, targets []latency.ProbeTarget) []latency.LatencyResult {
		batchCalled.Store(true)
		results := make([]latency.LatencyResult, len(targets))
		for i, t := range targets {
			results[i] = latency.LatencyResult{
				Min: 1, Max: 10, Avg: 5, Loss: 0,
				Device: t.Device, InterfaceName: t.InterfaceName, IP: t.IP, Reachable: true,
			}
		}
		return results
	}

	manager := latency.NewLatencyManager(
		latency.WithSmartContractFunc(func(context.Context) (*latency.ContractData, error) {
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
		}),
		latency.WithProberFunc(mockPerTarget),
		latency.WithBatchProberFunc(mockBatch),
		latency.WithProbeInterval(30*time.Second),
		latency.WithCacheUpdateInterval(30*time.Second),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = manager.Start(ctx) }()

	// Wait for probe to complete
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if manager.IsProbeReady() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !manager.IsProbeReady() {
		t.Fatal("manager never became ready")
	}

	if !batchCalled.Load() {
		t.Error("expected batch prober to be called")
	}
	if perTargetCalled.Load() {
		t.Error("expected per-target prober NOT to be called when batch prober is set")
	}

	results := manager.GetResultsCache()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	want := latency.LatencyResult{
		Min: 1, Max: 10, Avg: 5, Loss: 0,
		Device: latency.DeviceInfo{
			PubKey:   [32]byte{1},
			Code:     "dev01",
			PublicIp: [4]uint8{127, 0, 0, 1},
		},
		IP:        net.IP{127, 0, 0, 1},
		Reachable: true,
	}
	if diff := cmp.Diff(want, results[0], cmp.Comparer(func(a, b net.IP) bool { return a.Equal(b) }), cmpopts.IgnoreFields(sync.RWMutex{})); diff != "" {
		t.Errorf("result mismatch (-want +got): %s", diff)
	}
}
