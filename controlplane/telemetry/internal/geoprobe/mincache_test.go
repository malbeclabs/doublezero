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
	result := c.Update(testMeasurement{rttNs: 1000, label: "first"})
	if result != UpdateBest {
		t.Fatalf("expected UpdateBest, got %v", result)
	}
	got, ok := c.Best()
	if !ok {
		t.Fatal("expected best to be present")
	}
	if got.rttNs != 1000 {
		t.Fatalf("expected rttNs=1000, got %d", got.rttNs)
	}
}

func TestMinCache_LowerRTTReplacesBest(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 2000, label: "high"})

	result := c.Update(testMeasurement{rttNs: 1000, label: "low"})
	if result != UpdateBest {
		t.Fatalf("expected UpdateBest, got %v", result)
	}
	got, ok := c.Best()
	if !ok {
		t.Fatal("expected best to be present")
	}
	if got.label != "low" {
		t.Fatalf("expected label=low, got %s", got.label)
	}
}

func TestMinCache_HigherRTTGoesToBackup(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	result := c.Update(testMeasurement{rttNs: 2000, label: "backup"})
	if result != UpdateBackup {
		t.Fatalf("expected UpdateBackup, got %v", result)
	}
	got, ok := c.Best()
	if !ok || got.label != "best" {
		t.Fatalf("expected best to remain 'best', got %v", got)
	}
}

func TestMinCache_ExpiredBestPromotesBackup(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	// Insert backup 60ms later so it outlives best.
	*now = now.Add(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	// Advance past best's expiry but backup still valid.
	*now = now.Add(50 * time.Millisecond)

	// Insert something with higher RTT than backup — triggers promotion.
	result := c.Update(testMeasurement{rttNs: 3000, label: "new"})
	if result != UpdatePromoted {
		t.Fatalf("expected UpdatePromoted, got %v", result)
	}
}

func TestMinCache_PromotionThenNewBestReturnsUpdateBest(t *testing.T) {
	c, now := newTestCache(100 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 5000, label: "best"})

	*now = now.Add(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 8000, label: "backup"})

	// Best (5000) expires, backup (8000) still valid.
	*now = now.Add(50 * time.Millisecond)

	// New measurement beats the promoted backup — should be UpdateBest, not UpdatePromoted.
	result := c.Update(testMeasurement{rttNs: 3000, label: "new-low"})
	if result != UpdateBest {
		t.Fatalf("expected UpdateBest (new value beats promoted backup), got %v", result)
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

	result := c.Update(testMeasurement{rttNs: 9000, label: "backup-new"})
	if result != UpdateBackup {
		t.Fatalf("expected UpdateBackup (stale replacement), got %v", result)
	}
}

func TestMinCache_FreshBackupNotReplacedByWorse(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	// Immediately insert a worse value — backup is fresh, should not be replaced.
	result := c.Update(testMeasurement{rttNs: 3000, label: "worse"})
	if result != UpdateNone {
		t.Fatalf("expected UpdateNone (fresh backup not replaced), got %v", result)
	}
}

func TestMinCache_EqualRTTReplacesBest(t *testing.T) {
	c, _ := newTestCache(time.Hour)
	c.Update(testMeasurement{rttNs: 1000, label: "first"})
	result := c.Update(testMeasurement{rttNs: 1000, label: "second"})
	if result != UpdateBest {
		t.Fatalf("expected UpdateBest for equal RTT, got %v", result)
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

func TestUpdateResult_String(t *testing.T) {
	tests := []struct {
		r    UpdateResult
		want string
	}{
		{UpdateNone, "no_change"},
		{UpdateBest, "new_best"},
		{UpdateBackup, "backup_updated"},
		{UpdatePromoted, "promoted"},
		{UpdateResult(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("UpdateResult(%d).String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}

func TestUpdateResult_Changed(t *testing.T) {
	tests := []struct {
		r    UpdateResult
		want bool
	}{
		{UpdateNone, false},
		{UpdateBest, true},
		{UpdateBackup, false},
		{UpdatePromoted, true},
	}
	for _, tt := range tests {
		if got := tt.r.Changed(); got != tt.want {
			t.Errorf("UpdateResult(%d).Changed() = %v, want %v", tt.r, got, tt.want)
		}
	}
}
