package geoprobe

import (
	"sync"
	"testing"
	"time"
)

type testMeasurement struct {
	rttNs uint64
	label string
}

func testRttFunc(m testMeasurement) uint64 { return m.rttNs }

// newTestCache creates a MinCache with a controllable clock.
func newTestCache(maxAge time.Duration) (*MinCache[testMeasurement], *time.Time) {
	now := time.Now()
	c := NewMinCache[testMeasurement](maxAge, testRttFunc)
	c.nowFunc = func() time.Time { return now }
	return c, &now
}

func TestMinCache_FirstMeasurementBecomesBest(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	info := c.Update(testMeasurement{rttNs: 1000, label: "first"})
	if info.Result != UpdateBest {
		t.Fatalf("expected UpdateBest, got %v", info.Result)
	}
	if info.Promoted {
		t.Fatal("unexpected promotion")
	}
	if info.HadPrevBest {
		t.Fatal("unexpected previous best")
	}
	got, ok := c.Best()
	if !ok || got.rttNs != 1000 {
		t.Fatalf("expected rttNs=1000, got %d (ok=%v)", got.rttNs, ok)
	}
}

func TestMinCache_LowerRTTReplacesBest(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 2000, label: "high"})

	info := c.Update(testMeasurement{rttNs: 1000, label: "low"})
	if info.Result != UpdateBest {
		t.Fatalf("expected UpdateBest, got %v", info.Result)
	}
	if !info.HadPrevBest || info.PrevBestRttNs != 2000 {
		t.Fatalf("expected prev best 2000, got %d (had=%v)", info.PrevBestRttNs, info.HadPrevBest)
	}
	got, ok := c.Best()
	if !ok || got.label != "low" {
		t.Fatalf("expected label=low, got %s", got.label)
	}
}

func TestMinCache_HigherRTTGoesToBackup(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	info := c.Update(testMeasurement{rttNs: 2000, label: "backup"})
	if info.Result != UpdateBackup {
		t.Fatalf("expected UpdateBackup, got %v", info.Result)
	}
	if info.Promoted {
		t.Fatal("unexpected promotion")
	}
	got, ok := c.Best()
	if !ok || got.label != "best" {
		t.Fatalf("expected best to remain 'best', got %v", got)
	}
}

func TestMinCache_ExpiredBestPromotesBackup(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	*now = now.Add(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	// Advance past best's expiry but backup still valid.
	*now = now.Add(50 * time.Millisecond)

	// Insert something worse than backup — triggers promotion, incoming goes to backup.
	info := c.Update(testMeasurement{rttNs: 3000, label: "new"})
	if !info.Promoted {
		t.Fatal("expected promotion")
	}
	if info.Result != UpdateBackup {
		t.Fatalf("expected incoming to become backup, got %v", info.Result)
	}
	// PrevBestRttNs should be the expired best's RTT, not the backup's.
	if !info.HadPrevBest || info.PrevBestRttNs != 1000 {
		t.Fatalf("expected prev best 1000 (expired), got %d (had=%v)", info.PrevBestRttNs, info.HadPrevBest)
	}
}

func TestMinCache_PromotionThenNewBestReturnsUpdateBest(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 5000, label: "best"})

	*now = now.Add(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 8000, label: "backup"})

	// Best (5000) expires, backup (8000) still valid.
	*now = now.Add(50 * time.Millisecond)

	// New measurement beats the promoted backup.
	info := c.Update(testMeasurement{rttNs: 3000, label: "new-low"})
	if info.Result != UpdateBest {
		t.Fatalf("expected UpdateBest, got %v", info.Result)
	}
	if !info.Promoted {
		t.Fatal("expected promotion flag")
	}
	if !info.HadPrevBest || info.PrevBestRttNs != 5000 {
		t.Fatalf("expected prev best 5000, got %d", info.PrevBestRttNs)
	}
	got, _ := c.Best()
	if got.label != "new-low" {
		t.Fatalf("expected new-low, got %s", got.label)
	}
}

func TestMinCache_BothExpiredReturnsNothing(t *testing.T) {
	c, now := newTestCache(20 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 1000, label: "a"})
	c.Update(testMeasurement{rttNs: 2000, label: "b"})

	*now = now.Add(30 * time.Millisecond)

	_, ok := c.Best()
	if ok {
		t.Fatal("expected no best after both expired")
	}
}

func TestMinCache_StaleBackupReplaced(t *testing.T) {
	c, now := newTestCache(200 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})
	c.Update(testMeasurement{rttNs: 5000, label: "backup-old"})

	// Advance past half-maxAge so the backup is considered stale.
	*now = now.Add(110 * time.Millisecond)

	info := c.Update(testMeasurement{rttNs: 9000, label: "backup-new"})
	if info.Result != UpdateBackup {
		t.Fatalf("expected UpdateBackup (stale replacement), got %v", info.Result)
	}
}

func TestMinCache_FreshBackupNotReplacedByWorse(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	info := c.Update(testMeasurement{rttNs: 3000, label: "worse"})
	if info.Result != UpdateNone {
		t.Fatalf("expected UpdateNone (fresh backup not replaced), got %v", info.Result)
	}
}

func TestMinCache_EqualRTTReplacesBest(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "first"})
	info := c.Update(testMeasurement{rttNs: 1000, label: "second"})
	if info.Result != UpdateBest {
		t.Fatalf("expected UpdateBest for equal RTT, got %v", info.Result)
	}
	got, _ := c.Best()
	if got.label != "second" {
		t.Fatalf("expected second, got %s", got.label)
	}
}

func TestMinCache_BestRttNs(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	_, ok := c.BestRttNs()
	if ok {
		t.Fatal("expected no best on empty cache")
	}

	c.Update(testMeasurement{rttNs: 5000})
	rtt, ok := c.BestRttNs()
	if !ok || rtt != 5000 {
		t.Fatalf("expected 5000, got %d (ok=%v)", rtt, ok)
	}
}

func TestMinCache_Empty(t *testing.T) {
	c, now := newTestCache(20 * time.Millisecond)
	if !c.Empty() {
		t.Fatal("new cache should be empty")
	}
	c.Update(testMeasurement{rttNs: 1000})
	if c.Empty() {
		t.Fatal("cache with entry should not be empty")
	}
	*now = now.Add(30 * time.Millisecond)
	if !c.Empty() {
		t.Fatal("cache should be empty after expiry")
	}
}

func TestMinCache_ConcurrentAccess(t *testing.T) {
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(rtt uint64) {
			defer wg.Done()
			c.Update(testMeasurement{rttNs: rtt, label: "concurrent"})
			c.Best()
			c.BestRttNs()
		}(uint64(i * 100))
	}
	wg.Wait()

	_, ok := c.Best()
	if !ok {
		t.Fatal("expected best to be present after concurrent updates")
	}
}

func TestMinCacheMap_Sweep(t *testing.T) {
	now := time.Now()
	m := NewMinCacheMap[string, testMeasurement](50*time.Millisecond, testRttFunc)

	cA := m.Get("a")
	cA.nowFunc = func() time.Time { return now }
	cA.Update(testMeasurement{rttNs: 1000})

	cB := m.Get("b")
	cB.nowFunc = func() time.Time { return now }
	cB.Update(testMeasurement{rttNs: 2000})

	// Expire only "a" by advancing its clock.
	expiredNow := now.Add(60 * time.Millisecond)
	cA.nowFunc = func() time.Time { return expiredNow }

	m.Sweep()

	// "a" should be evicted, "b" should remain.
	m.mu.RLock()
	_, hasA := m.caches["a"]
	_, hasB := m.caches["b"]
	m.mu.RUnlock()

	if hasA {
		t.Fatal("expected 'a' to be swept")
	}
	if !hasB {
		t.Fatal("expected 'b' to remain")
	}
}

func TestUpdateResult_String(t *testing.T) {
	tests := []struct {
		r    UpdateResult
		want string
	}{
		{UpdateNone, "no_change"},
		{UpdateBest, "new_best"},
		{UpdateBackup, "backup_updated"},
		{UpdateResult(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("UpdateResult(%d).String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}

func TestUpdateInfo_Changed(t *testing.T) {
	tests := []struct {
		info UpdateInfo
		want bool
	}{
		{UpdateInfo{Result: UpdateNone}, false},
		{UpdateInfo{Result: UpdateBest}, true},
		{UpdateInfo{Result: UpdateBackup}, false},
		{UpdateInfo{Result: UpdateBackup, Promoted: true}, true},
		{UpdateInfo{Result: UpdateNone, Promoted: true}, true},
	}
	for _, tt := range tests {
		if got := tt.info.Changed(); got != tt.want {
			t.Errorf("%+v.Changed() = %v, want %v", tt.info, got, tt.want)
		}
	}
}
