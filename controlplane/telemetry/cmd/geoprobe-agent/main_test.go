package main

import (
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
)

func makeTestOffset(pubkey [32]byte, rttNs uint64) *geoprobe.LocationOffset {
	return &geoprobe.LocationOffset{
		Pubkey:          pubkey,
		MeasurementSlot: 12345,
		MeasuredRttNs:   rttNs,
		Lat:             52.3676,
		Lng:             4.9041,
		RttNs:           rttNs,
		NumReferences:   0,
		References:      []geoprobe.LocationOffset{},
	}
}

func TestOffsetCache_PutAndGet(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	pubkey := [32]byte{1}
	offset := makeTestOffset(pubkey, 1000)

	cache.Put(offset)

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset, got nil")
	}
	if got.RttNs != 1000 {
		t.Errorf("expected RttNs=1000, got %d", got.RttNs)
	}
	if got.Lat != 52.3676 {
		t.Errorf("expected Lat=52.3676, got %f", got.Lat)
	}
}

func TestOffsetCache_GetMissing(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	got := cache.Get([32]byte{99})
	if got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestOffsetCache_GetExpired(t *testing.T) {
	cache := newOffsetCache(1 * time.Millisecond)

	pubkey := [32]byte{1}
	offset := makeTestOffset(pubkey, 1000)
	cache.Put(offset)

	time.Sleep(5 * time.Millisecond)

	got := cache.Get(pubkey)
	if got != nil {
		t.Errorf("expected nil for expired entry, got %+v", got)
	}
}

func TestOffsetCache_PutReplaces(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	pubkey := [32]byte{1}

	cache.Put(makeTestOffset(pubkey, 1000))
	cache.Put(makeTestOffset(pubkey, 2000))

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset, got nil")
	}
	if got.RttNs != 2000 {
		t.Errorf("expected RttNs=2000, got %d", got.RttNs)
	}
}

func TestOffsetCache_GetBest(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	cache.Put(makeTestOffset([32]byte{1}, 5000))
	cache.Put(makeTestOffset([32]byte{2}, 1000))
	cache.Put(makeTestOffset([32]byte{3}, 3000))

	best := cache.GetBest()
	if best == nil {
		t.Fatal("expected best offset, got nil")
	}
	if best.RttNs != 1000 {
		t.Errorf("expected best RttNs=1000, got %d", best.RttNs)
	}
	if best.Pubkey != [32]byte{2} {
		t.Errorf("expected pubkey {2}, got %v", best.Pubkey)
	}
}

func TestOffsetCache_GetBestSkipsExpired(t *testing.T) {
	cache := newOffsetCache(50 * time.Millisecond)

	// Add the "best" one first.
	cache.Put(makeTestOffset([32]byte{1}, 100))

	time.Sleep(60 * time.Millisecond)

	// Add a worse one that's still valid.
	cache.Put(makeTestOffset([32]byte{2}, 5000))

	best := cache.GetBest()
	if best == nil {
		t.Fatal("expected best offset, got nil")
	}
	if best.RttNs != 5000 {
		t.Errorf("expected best RttNs=5000 (expired best skipped), got %d", best.RttNs)
	}
}

func TestOffsetCache_GetBestEmpty(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	best := cache.GetBest()
	if best != nil {
		t.Errorf("expected nil for empty cache, got %+v", best)
	}
}

func TestOffsetCache_Evict(t *testing.T) {
	cache := newOffsetCache(1 * time.Millisecond)

	cache.Put(makeTestOffset([32]byte{1}, 1000))
	cache.Put(makeTestOffset([32]byte{2}, 2000))

	time.Sleep(5 * time.Millisecond)

	evicted := cache.Evict()
	if evicted != 2 {
		t.Errorf("expected 2 evicted, got %d", evicted)
	}

	// Verify entries are gone.
	if got := cache.Get([32]byte{1}); got != nil {
		t.Errorf("expected nil after eviction, got %+v", got)
	}
	if got := cache.Get([32]byte{2}); got != nil {
		t.Errorf("expected nil after eviction, got %+v", got)
	}
}

func TestOffsetCache_EvictPartial(t *testing.T) {
	cache := newOffsetCache(50 * time.Millisecond)

	cache.Put(makeTestOffset([32]byte{1}, 1000))

	time.Sleep(60 * time.Millisecond)

	cache.Put(makeTestOffset([32]byte{2}, 2000))

	evicted := cache.Evict()
	if evicted != 1 {
		t.Errorf("expected 1 evicted, got %d", evicted)
	}

	if got := cache.Get([32]byte{1}); got != nil {
		t.Error("expected key 1 evicted")
	}
	if got := cache.Get([32]byte{2}); got == nil {
		t.Error("expected key 2 still present")
	}
}

func TestOffsetCache_ConcurrentAccess(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pubkey := [32]byte{byte(i)}
			cache.Put(makeTestOffset(pubkey, uint64(i*1000)))
			cache.Get(pubkey)
			cache.GetBest()
			cache.Evict()
		}(i)
	}
	wg.Wait()
}

func TestParseParentDZDs_Empty(t *testing.T) {
	parents, err := parseParentDZDs("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 0 {
		t.Errorf("expected 0 parents, got %d", len(parents))
	}
}

func TestParseParentDZDs_Valid(t *testing.T) {
	kp1 := solana.NewWallet()
	kp2 := solana.NewWallet()

	input := kp1.PublicKey().String() + "@10.0.0.1:8923," + kp2.PublicKey().String() + "@10.0.0.2:8923"

	parents, err := parseParentDZDs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(parents))
	}

	if parents[0].address.Host != "10.0.0.1" || parents[0].address.Port != 8923 {
		t.Errorf("unexpected first parent address: %+v", parents[0].address)
	}
	if parents[1].address.Host != "10.0.0.2" || parents[1].address.Port != 8923 {
		t.Errorf("unexpected second parent address: %+v", parents[1].address)
	}

	// Verify pubkeys match.
	var expectedPK1 [32]byte
	pk1 := kp1.PublicKey()
	copy(expectedPK1[:], pk1[:])
	if parents[0].pubkey != expectedPK1 {
		t.Errorf("unexpected first parent pubkey")
	}
}

func TestParseParentDZDs_MissingAt(t *testing.T) {
	_, err := parseParentDZDs("invalid-no-at-sign")
	if err == nil {
		t.Fatal("expected error for missing @ sign")
	}
}

func TestParseParentDZDs_InvalidPubkey(t *testing.T) {
	_, err := parseParentDZDs("not-a-pubkey@10.0.0.1:8923")
	if err == nil {
		t.Fatal("expected error for invalid pubkey")
	}
}

func TestParseParentDZDs_Dedup(t *testing.T) {
	kp := solana.NewWallet()
	input := kp.PublicKey().String() + "@10.0.0.1:8923," + kp.PublicKey().String() + "@10.0.0.1:8923"

	parents, err := parseParentDZDs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 1 {
		t.Errorf("expected 1 parent after dedup, got %d", len(parents))
	}
}
