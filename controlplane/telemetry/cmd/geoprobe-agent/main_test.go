package main

import (
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
)

func makeTestOffset(senderPubkey [32]byte, rttNs uint64) *geoprobe.LocationOffset {
	return &geoprobe.LocationOffset{
		SenderPubkey:    senderPubkey,
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
	// With two-slot cache, lower RTT stays as best.
	if got.RttNs != 1000 {
		t.Errorf("expected RttNs=1000 (best kept), got %d", got.RttNs)
	}
}

func TestOffsetCache_PutKeepsBest(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	pubkey := [32]byte{1}

	cache.Put(makeTestOffset(pubkey, 1000))
	cache.Put(makeTestOffset(pubkey, 2000))

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset, got nil")
	}
	if got.RttNs != 1000 {
		t.Errorf("expected RttNs=1000 (best kept over higher RTT), got %d", got.RttNs)
	}
}

func TestOffsetCache_PutLowerRTTReplacesBest(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	pubkey := [32]byte{1}

	cache.Put(makeTestOffset(pubkey, 2000))
	cache.Put(makeTestOffset(pubkey, 1000))

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset, got nil")
	}
	if got.RttNs != 1000 {
		t.Errorf("expected RttNs=1000 (lower RTT replaces best), got %d", got.RttNs)
	}
}

func TestOffsetCache_BackupPromotion(t *testing.T) {
	cache := newOffsetCache(20 * time.Millisecond)

	pubkey := [32]byte{1}

	// Put best (low RTT).
	cache.Put(makeTestOffset(pubkey, 500))
	// Put backup (higher RTT).
	cache.Put(makeTestOffset(pubkey, 2000))

	// Wait for best to expire.
	time.Sleep(25 * time.Millisecond)

	// Put a new value to trigger promotion of backup.
	cache.Put(makeTestOffset(pubkey, 3000))

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset after backup promotion, got nil")
	}
	// Backup (2000) should have been promoted or the new value (3000) used.
	// Either way, we should get a valid offset back.
	if got.RttNs != 2000 && got.RttNs != 3000 {
		t.Errorf("expected RttNs=2000 or 3000 after backup promotion, got %d", got.RttNs)
	}
}

func TestOffsetCache_BackupRefreshInHalfWindow(t *testing.T) {
	cache := newOffsetCache(100 * time.Millisecond)

	pubkey := [32]byte{1}

	// Put best (low RTT).
	cache.Put(makeTestOffset(pubkey, 500))

	// Wait past half-maxAge (>50ms).
	time.Sleep(60 * time.Millisecond)

	// Put a higher RTT offset; this should refresh the backup slot since
	// we're past the half-maxAge window.
	cache.Put(makeTestOffset(pubkey, 2000))

	got := cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected offset, got nil")
	}
	// Best should still be the low RTT.
	if got.RttNs != 500 {
		t.Errorf("expected RttNs=500 (best unchanged), got %d", got.RttNs)
	}

	// Wait for best to expire but backup should still be valid since it was
	// refreshed recently.
	time.Sleep(50 * time.Millisecond)

	got = cache.Get(pubkey)
	if got == nil {
		t.Fatal("expected backup offset after best expired, got nil")
	}
	if got.RttNs != 2000 {
		t.Errorf("expected RttNs=2000 (backup still valid), got %d", got.RttNs)
	}
}

func TestOffsetCache_GetBestWithTwoSlots(t *testing.T) {
	cache := newOffsetCache(1 * time.Hour)

	// Sender 1: best=500, backup=1500.
	cache.Put(makeTestOffset([32]byte{1}, 500))
	cache.Put(makeTestOffset([32]byte{1}, 1500))

	// Sender 2: best=300, backup=2000.
	cache.Put(makeTestOffset([32]byte{2}, 300))
	cache.Put(makeTestOffset([32]byte{2}, 2000))

	// Sender 3: best=800, backup=3000.
	cache.Put(makeTestOffset([32]byte{3}, 800))
	cache.Put(makeTestOffset([32]byte{3}, 3000))

	best := cache.GetBest()
	if best == nil {
		t.Fatal("expected best offset, got nil")
	}
	if best.RttNs != 300 {
		t.Errorf("expected global best RttNs=300, got %d", best.RttNs)
	}
	if best.SenderPubkey != [32]byte{2} {
		t.Errorf("expected sender pubkey {2}, got %v", best.SenderPubkey)
	}
}

func TestOffsetCache_EvictBothSlots(t *testing.T) {
	cache := newOffsetCache(10 * time.Millisecond)

	pubkey := [32]byte{1}

	// Put best and backup.
	cache.Put(makeTestOffset(pubkey, 500))
	cache.Put(makeTestOffset(pubkey, 2000))

	// Also add a non-expiring sender.
	cache2 := [32]byte{2}
	cache.Put(makeTestOffset(cache2, 1000))

	// Wait for all entries of sender 1 to expire.
	time.Sleep(15 * time.Millisecond)

	// Refresh sender 2 so it stays valid.
	cache.Put(makeTestOffset(cache2, 1000))

	evicted := cache.Evict()
	if evicted != 1 {
		t.Errorf("expected 1 evicted (sender with both slots expired), got %d", evicted)
	}

	// Sender 1 should be gone.
	if got := cache.Get(pubkey); got != nil {
		t.Errorf("expected sender 1 evicted, got %+v", got)
	}

	// Sender 2 should still be present.
	if got := cache.Get(cache2); got == nil {
		t.Error("expected sender 2 still present")
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
	if best.SenderPubkey != [32]byte{2} {
		t.Errorf("expected sender pubkey {2}, got %v", best.SenderPubkey)
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

func TestParseAllowedPubkeys_Empty(t *testing.T) {
	keys, err := parseAllowedPubkeys("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keys != nil {
		t.Errorf("expected nil for empty input, got %v", keys)
	}
}

func TestParseAllowedPubkeys_Single(t *testing.T) {
	wallet := solana.NewWallet()
	keys, err := parseAllowedPubkeys(wallet.PublicKey().String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	var expected [32]byte
	pk := wallet.PublicKey()
	copy(expected[:], pk[:])
	if keys[0] != expected {
		t.Errorf("key mismatch: got %v, want %v", keys[0], expected)
	}
}

func TestParseAllowedPubkeys_Multiple(t *testing.T) {
	w1 := solana.NewWallet()
	w2 := solana.NewWallet()
	w3 := solana.NewWallet()

	input := w1.PublicKey().String() + "," + w2.PublicKey().String() + "," + w3.PublicKey().String()
	keys, err := parseAllowedPubkeys(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
}

func TestParseAllowedPubkeys_Whitespace(t *testing.T) {
	w1 := solana.NewWallet()
	w2 := solana.NewWallet()

	input := "  " + w1.PublicKey().String() + " , " + w2.PublicKey().String() + "  "
	keys, err := parseAllowedPubkeys(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestParseAllowedPubkeys_Invalid(t *testing.T) {
	_, err := parseAllowedPubkeys("not-a-valid-pubkey")
	if err == nil {
		t.Fatal("expected error for invalid pubkey")
	}
}

func TestParseAllowedPubkeys_MixedValid_Invalid(t *testing.T) {
	wallet := solana.NewWallet()
	input := wallet.PublicKey().String() + ",not-a-pubkey"
	_, err := parseAllowedPubkeys(input)
	if err == nil {
		t.Fatal("expected error for mixed valid/invalid pubkeys")
	}
}

func TestParseParentDZD_Empty(t *testing.T) {
	parent, err := parseParentDZD("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parent != nil {
		t.Errorf("expected nil for empty input, got %+v", parent)
	}
}

func TestParseParentDZD_Valid(t *testing.T) {
	parentWallet := solana.NewWallet()
	authorityWallet := solana.NewWallet()

	input := parentWallet.PublicKey().String() + "," + authorityWallet.PublicKey().String()

	parent, err := parseParentDZD(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parent == nil {
		t.Fatal("expected parent, got nil")
	}

	var expectedParent, expectedAuthority [32]byte
	pk := parentWallet.PublicKey()
	copy(expectedParent[:], pk[:])
	ak := authorityWallet.PublicKey()
	copy(expectedAuthority[:], ak[:])

	if parent.pubkey != expectedParent {
		t.Errorf("unexpected parent pubkey")
	}
	if parent.authorityPubkey != expectedAuthority {
		t.Errorf("unexpected authority pubkey")
	}
}

func TestParseParentDZD_WrongPartCount(t *testing.T) {
	kp := solana.NewWallet()

	// Single key (missing authority).
	_, err := parseParentDZD(kp.PublicKey().String())
	if err == nil {
		t.Fatal("expected error for single key")
	}

	// Three keys.
	kp2 := solana.NewWallet()
	kp3 := solana.NewWallet()
	_, err = parseParentDZD(kp.PublicKey().String() + "," + kp2.PublicKey().String() + "," + kp3.PublicKey().String())
	if err == nil {
		t.Fatal("expected error for three keys")
	}
}

func TestParseParentDZD_InvalidParentPubkey(t *testing.T) {
	authority := solana.NewWallet()
	_, err := parseParentDZD("not-a-pubkey," + authority.PublicKey().String())
	if err == nil {
		t.Fatal("expected error for invalid parent pubkey")
	}
}

func TestParseParentDZD_InvalidAuthorityPubkey(t *testing.T) {
	parent := solana.NewWallet()
	_, err := parseParentDZD(parent.PublicKey().String() + ",not-a-pubkey")
	if err == nil {
		t.Fatal("expected error for invalid authority pubkey")
	}
}
