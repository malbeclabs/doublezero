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

// While best is young (> maxAge/2 TTL remaining), a higher sample is discarded:
// no backup is collected outside the guard window.
func TestMinCache_HigherRTTWhileYoungIsDiscarded(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	info := c.Update(testMeasurement{rttNs: 2000, label: "high"})
	if info.Result != UpdateNone {
		t.Fatalf("expected UpdateNone while best is young, got %v", info.Result)
	}
	if c.backup != nil {
		t.Fatalf("expected no backup while best is young, got %+v", c.backup)
	}
	got, ok := c.Best()
	if !ok || got.label != "best" {
		t.Fatalf("expected best to remain 'best', got %v", got)
	}
}

// In best's final guard window, a higher sample becomes the backup.
func TestMinCache_HigherRTTInGuardGoesToBackup(t *testing.T) {
	c, now := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	*now = now.Add(40 * time.Minute) // best has 20m TTL (< 30m guard)
	info := c.Update(testMeasurement{rttNs: 2000, label: "backup"})
	if info.Result != UpdateBackup {
		t.Fatalf("expected UpdateBackup in guard window, got %v", info.Result)
	}
	got, ok := c.Best()
	if !ok || got.label != "best" {
		t.Fatalf("expected best to remain 'best', got %v", got)
	}
}

// Production failure reproduction: a steady ~3.2ms floor with noisy higher
// samples. The record-low best expires, but a recent good sample collected in
// the final guard window must be promoted — never a transient high spike.
func TestMinCache_GuardedBackupTracksFloorAcrossExpiry(t *testing.T) {
	c, now := newTestCache(time.Hour)

	c.Update(testMeasurement{rttNs: 3150, label: "floor"}) // record low at t=0

	// Young phase: higher samples are dropped, backup stays clear.
	*now = now.Add(20 * time.Minute)
	c.Update(testMeasurement{rttNs: 38000, label: "spike"})
	if got, _ := c.Best(); got.rttNs != 3150 {
		t.Fatalf("young phase: want 3150, got %d", got.rttNs)
	}

	// Final guard window: a good near-floor sample is captured as backup.
	*now = now.Add(15 * time.Minute) // t=35m, best has 25m TTL (< 30m guard)
	c.Update(testMeasurement{rttNs: 3220, label: "near-floor"})
	// More high samples in the guard window must NOT displace the good backup.
	c.Update(testMeasurement{rttNs: 38050, label: "spike2"})
	c.Update(testMeasurement{rttNs: 80000, label: "spike3"})

	// best expires (t > 60m); backup (3220) is promoted, not a spike.
	*now = now.Add(26 * time.Minute) // t=61m
	c.Update(testMeasurement{rttNs: 40000, label: "post"})
	if got, _ := c.Best(); got.rttNs != 3220 {
		t.Fatalf("after expiry: want promoted 3220, got %d", got.rttNs)
	}
}

// A genuine drought: no good sample in best's final guard window. Promotion
// must surface the elevated RTT (signal, not noise).
func TestMinCache_GuardedBackupDroughtPromotesHigh(t *testing.T) {
	c, now := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 3150, label: "floor"})

	*now = now.Add(35 * time.Minute) // guard window
	c.Update(testMeasurement{rttNs: 40000, label: "high1"})
	c.Update(testMeasurement{rttNs: 45000, label: "high2"})

	*now = now.Add(26 * time.Minute) // best expires
	c.Update(testMeasurement{rttNs: 50000, label: "post"})
	if got, _ := c.Best(); got.rttNs != 40000 {
		t.Fatalf("drought: want promoted 40000, got %d", got.rttNs)
	}
}

// A new record low resets best's clock and clears any collected backup.
func TestMinCache_RecordLowResetsAndClearsBackup(t *testing.T) {
	c, now := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 3150, label: "floor"})

	*now = now.Add(40 * time.Minute) // guard window
	info := c.Update(testMeasurement{rttNs: 3300, label: "backup"})
	if info.Result != UpdateBackup {
		t.Fatalf("want UpdateBackup, got %v", info.Result)
	}

	info = c.Update(testMeasurement{rttNs: 3000, label: "new-low"})
	if info.Result != UpdateBest {
		t.Fatalf("want UpdateBest, got %v", info.Result)
	}
	if c.backup != nil {
		t.Fatalf("record low must clear backup, got %+v", c.backup)
	}

	// best reset to 3000 with a fresh full window: it does not expire at old t.
	*now = now.Add(25 * time.Minute)
	if got, _ := c.Best(); got.rttNs != 3000 {
		t.Fatalf("want fresh best 3000, got %d", got.rttNs)
	}
}

// Read-through returns the min of the non-expired slots without mutating state.
func TestMinCache_ReadThroughMin(t *testing.T) {
	c, now := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 5000, label: "best"})

	*now = now.Add(35 * time.Minute) // guard window
	c.Update(testMeasurement{rttNs: 6000, label: "backup"})
	if got, _ := c.Best(); got.rttNs != 5000 {
		t.Fatalf("read-through want 5000, got %d", got.rttNs)
	}
}

// A promoted backup is at most a guard-window old, so it does not immediately
// expire after promotion.
func TestMinCache_PromotedBackupIsFresh(t *testing.T) {
	c, now := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 3150, label: "floor"})

	*now = now.Add(31 * time.Minute) // guard window, backup collected near here
	c.Update(testMeasurement{rttNs: 3200, label: "backup"})

	*now = now.Add(30 * time.Minute) // t=61m: best expires, promote 3200 (~30m old)
	c.Update(testMeasurement{rttNs: 9000, label: "post"})
	if _, ok := c.BestRttNs(); !ok {
		t.Fatal("promoted backup must still be live")
	}
	if got, _ := c.Best(); got.rttNs != 3200 {
		t.Fatalf("want promoted 3200, got %d", got.rttNs)
	}
}

// On promotion, the incoming worse-than-promoted sample becomes the backup.
// PrevBestRttNs reflects only a *live* best at call time; since best was already
// expired here, HadPrevBest is false.
func TestMinCache_ExpiredBestPromotesBackup(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	// Guard window (best has 40ms TTL < 50ms guard): collect a backup.
	*now = now.Add(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	// Advance past best's expiry; backup (recvAt=60ms) still valid.
	*now = now.Add(50 * time.Millisecond) // t=110ms

	// Incoming worse than promoted backup: promotion fires, incoming -> backup
	// (it lands in the promoted best's guard window).
	info := c.Update(testMeasurement{rttNs: 3000, label: "new"})
	if !info.Promoted {
		t.Fatal("expected promotion")
	}
	if info.Result != UpdateBackup {
		t.Fatalf("expected incoming to become backup, got %v", info.Result)
	}
	if info.HadPrevBest {
		t.Fatalf("expected no live prev best (best already expired), got %d", info.PrevBestRttNs)
	}
	// Promoted backup is the live best.
	if got, _ := c.Best(); got.rttNs != 2000 {
		t.Fatalf("expected promoted best 2000, got %d", got.rttNs)
	}
}

func TestMinCache_PromotionThenNewBestReturnsUpdateBest(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 5000, label: "best"})

	// Guard window: collect a backup.
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
	if info.HadPrevBest {
		t.Fatalf("expected no live prev best (best already expired), got %d", info.PrevBestRttNs)
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
