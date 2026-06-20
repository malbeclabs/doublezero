package devnet

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeSubnetFinder returns a sequence of subnets, one per call, so we can assert that
// createNetworkWithSubnet re-allocates a fresh subnet on each pool-overlap retry. It records the
// IDs it was called with so tests can verify the retry varies the allocation seed.
type fakeSubnetFinder struct {
	subnets []string
	calls   int
	seeds   []string
	err     error
}

func (f *fakeSubnetFinder) FindAvailableSubnet(_ context.Context, id string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.seeds = append(f.seeds, id)
	idx := f.calls
	f.calls++
	if idx >= len(f.subnets) {
		return f.subnets[len(f.subnets)-1], nil
	}
	return f.subnets[idx], nil
}

func TestIsPoolOverlapErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{"pool overlaps", errors.New("Error response from daemon: Pool overlaps with other one on this address space"), true},
		{"invalid pool request", errors.New("invalid pool request: Pool overlaps"), true},
		{"wrapped", fmt.Errorf("create network: %w", errors.New("Pool overlaps with other one")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPoolOverlapErr(tt.err); got != tt.want {
				t.Fatalf("isPoolOverlapErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestCreateNetworkWithSubnet_Success(t *testing.T) {
	finder := &fakeSubnetFinder{subnets: []string{"10.1.2.0/24"}}
	var used string
	got, err := createNetworkWithSubnet(context.Background(), finder, "deploy-1", func(subnet string) error {
		used = subnet
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.1.2.0/24" || used != "10.1.2.0/24" {
		t.Fatalf("got subnet %q (create saw %q), want 10.1.2.0/24", got, used)
	}
	if finder.calls != 1 {
		t.Fatalf("expected 1 allocation, got %d", finder.calls)
	}
}

func TestCreateNetworkWithSubnet_RetriesOnPoolOverlap(t *testing.T) {
	finder := &fakeSubnetFinder{subnets: []string{"10.1.0.0/24", "10.2.0.0/24", "10.3.0.0/24"}}
	overlap := errors.New("Error response from daemon: Pool overlaps with other one on this address space")

	var attempts []string
	got, err := createNetworkWithSubnet(context.Background(), finder, "deploy-1", func(subnet string) error {
		attempts = append(attempts, subnet)
		// Fail the first two attempts with a pool overlap, succeed on the third.
		if len(attempts) < 3 {
			return overlap
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.3.0.0/24" {
		t.Fatalf("got subnet %q, want 10.3.0.0/24", got)
	}
	// Each retry must re-allocate a fresh subnet, not reuse the conflicting one.
	want := []string{"10.1.0.0/24", "10.2.0.0/24", "10.3.0.0/24"}
	if len(attempts) != len(want) {
		t.Fatalf("got %d attempts, want %d", len(attempts), len(want))
	}
	for i := range want {
		if attempts[i] != want[i] {
			t.Fatalf("attempt %d = %q, want %q", i, attempts[i], want[i])
		}
	}

	// The allocator must be seeded with a distinct ID on each retry so a deterministic finder is
	// forced to pick a different subnet even when the conflicting CIDR is invisible to its docker
	// scan. Attempt 0 uses the bare ID; later attempts must differ from it and from each other.
	wantSeeds := []string{"deploy-1", "deploy-1#1", "deploy-1#2"}
	if len(finder.seeds) != len(wantSeeds) {
		t.Fatalf("got %d seeds, want %d", len(finder.seeds), len(wantSeeds))
	}
	for i := range wantSeeds {
		if finder.seeds[i] != wantSeeds[i] {
			t.Fatalf("seed %d = %q, want %q", i, finder.seeds[i], wantSeeds[i])
		}
	}
}

func TestCreateNetworkWithSubnet_GivesUpAfterMaxRetries(t *testing.T) {
	finder := &fakeSubnetFinder{subnets: []string{"10.1.0.0/24"}}
	overlap := errors.New("invalid pool request: Pool overlaps")

	_, err := createNetworkWithSubnet(context.Background(), finder, "deploy-1", func(subnet string) error {
		return overlap
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if finder.calls != poolOverlapMaxRetries {
		t.Fatalf("expected %d allocation attempts, got %d", poolOverlapMaxRetries, finder.calls)
	}
}

func TestCreateNetworkWithSubnet_NonOverlapErrorNotRetried(t *testing.T) {
	finder := &fakeSubnetFinder{subnets: []string{"10.1.0.0/24"}}
	fatal := errors.New("permission denied")

	_, err := createNetworkWithSubnet(context.Background(), finder, "deploy-1", func(subnet string) error {
		return fatal
	})
	if !errors.Is(err, fatal) {
		t.Fatalf("expected fatal error to propagate, got %v", err)
	}
	if finder.calls != 1 {
		t.Fatalf("expected no retry on non-overlap error, got %d calls", finder.calls)
	}
}

func TestCreateNetworkWithSubnet_AllocationErrorPropagates(t *testing.T) {
	allocErr := errors.New("no available subnet")
	finder := &fakeSubnetFinder{err: allocErr}

	_, err := createNetworkWithSubnet(context.Background(), finder, "deploy-1", func(subnet string) error {
		t.Fatal("create should not be called when allocation fails")
		return nil
	})
	if !errors.Is(err, allocErr) {
		t.Fatalf("expected allocation error to propagate, got %v", err)
	}
}
