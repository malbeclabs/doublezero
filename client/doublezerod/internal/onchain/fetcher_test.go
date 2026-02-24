package onchain

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type mockProvider struct {
	mu       sync.Mutex
	calls    int
	data     *serviceability.ProgramData
	err      error
	onCallFn func(call int) // optional hook called with call count
}

func (m *mockProvider) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.onCallFn != nil {
		m.onCallFn(m.calls)
	}
	return m.data, m.err
}

func (m *mockProvider) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestCachingFetcher_CacheHit(t *testing.T) {
	data := &serviceability.ProgramData{
		Devices: []serviceability.Device{{Code: "dev1"}},
	}
	provider := &mockProvider{data: data}
	fetcher := NewCachingFetcher(provider, 1*time.Second)

	ctx := context.Background()

	// First call fetches from provider.
	got, err := fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Devices) != 1 || got.Devices[0].Code != "dev1" {
		t.Fatalf("unexpected data: %+v", got)
	}

	// Second call within TTL should return cached data without another provider call.
	got2, err := fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got2 != got {
		t.Fatal("expected same pointer from cache")
	}
	if provider.callCount() != 1 {
		t.Fatalf("expected 1 provider call, got %d", provider.callCount())
	}
}

func TestCachingFetcher_CacheExpiry(t *testing.T) {
	data := &serviceability.ProgramData{
		Devices: []serviceability.Device{{Code: "dev1"}},
	}
	provider := &mockProvider{data: data}
	fetcher := NewCachingFetcher(provider, 10*time.Millisecond)

	ctx := context.Background()

	_, err := fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	_, err = fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.callCount() != 2 {
		t.Fatalf("expected 2 provider calls after TTL expiry, got %d", provider.callCount())
	}
}

func TestCachingFetcher_ErrorReturnsStaleData(t *testing.T) {
	data := &serviceability.ProgramData{
		Devices: []serviceability.Device{{Code: "dev1"}},
	}
	provider := &mockProvider{data: data}
	fetcher := NewCachingFetcher(provider, 10*time.Millisecond)

	ctx := context.Background()

	// Populate cache.
	got, err := fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expire cache and make provider fail.
	time.Sleep(20 * time.Millisecond)
	provider.mu.Lock()
	provider.err = errors.New("rpc error")
	provider.data = nil
	provider.mu.Unlock()

	// Should return stale cached data.
	got2, err := fetcher.GetProgramData(ctx)
	if err != nil {
		t.Fatalf("expected stale data, got error: %v", err)
	}
	if got2 != got {
		t.Fatal("expected stale cached pointer")
	}
}

func TestCachingFetcher_ErrorWithNoCache(t *testing.T) {
	provider := &mockProvider{err: errors.New("rpc error")}
	fetcher := NewCachingFetcher(provider, 1*time.Second)

	_, err := fetcher.GetProgramData(context.Background())
	if err == nil {
		t.Fatal("expected error when no cache and provider fails")
	}
}

func TestCachingFetcher_ConcurrentAccess(t *testing.T) {
	data := &serviceability.ProgramData{
		Devices: []serviceability.Device{{Code: "dev1"}},
	}
	provider := &mockProvider{data: data}
	fetcher := NewCachingFetcher(provider, 1*time.Second)

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fetcher.GetProgramData(ctx)
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}

	// With caching, should have exactly 1 provider call.
	if provider.callCount() != 1 {
		t.Fatalf("expected 1 provider call with concurrent access, got %d", provider.callCount())
	}
}
