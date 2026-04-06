package geoprobe

import (
	"testing"
	"time"
)

type testMeasurement struct {
	rttNs uint64
	label string
}

func testRttFunc(m testMeasurement) uint64 { return m.rttNs }

func TestMinCache_FirstMeasurementBecomesBest(t *testing.T) {
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
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
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
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
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	result := c.Update(testMeasurement{rttNs: 2000, label: "backup"})
	if result != UpdateBackup {
		t.Fatalf("expected UpdateBackup, got %v", result)
	}
	// Best should still be the lower value.
	got, ok := c.Best()
	if !ok || got.label != "best" {
		t.Fatalf("expected best to remain 'best', got %v", got)
	}
}

func TestMinCache_ExpiredBestPromotesBackup(t *testing.T) {
	c := NewMinCache[testMeasurement](100*time.Millisecond, testRttFunc)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})

	// Insert backup 60ms later so it outlives best.
	time.Sleep(60 * time.Millisecond)
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	// Wait for best to expire but backup to still be valid.
	time.Sleep(50 * time.Millisecond)

	// Insert something with higher RTT than backup — this triggers promotion.
	result := c.Update(testMeasurement{rttNs: 3000, label: "new"})
	if result != UpdatePromoted {
		t.Fatalf("expected UpdatePromoted, got %v", result)
	}
}

func TestMinCache_BothExpiredReturnsNothing(t *testing.T) {
	c := NewMinCache[testMeasurement](20*time.Millisecond, testRttFunc)
	c.Update(testMeasurement{rttNs: 1000, label: "a"})
	c.Update(testMeasurement{rttNs: 2000, label: "b"})

	time.Sleep(30 * time.Millisecond)

	_, ok := c.Best()
	if ok {
		t.Fatal("expected no best after both expired")
	}
}

func TestMinCache_StaleBackupReplaced(t *testing.T) {
	c := NewMinCache[testMeasurement](200*time.Millisecond, testRttFunc)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})
	c.Update(testMeasurement{rttNs: 5000, label: "backup-old"})

	// Wait past half-maxAge so the backup is considered stale.
	time.Sleep(110 * time.Millisecond)

	result := c.Update(testMeasurement{rttNs: 9000, label: "backup-new"})
	if result != UpdateBackup {
		t.Fatalf("expected UpdateBackup (stale replacement), got %v", result)
	}
}

func TestMinCache_UpdateNoneWhenBackupIsBetter(t *testing.T) {
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
	c.Update(testMeasurement{rttNs: 1000, label: "best"})
	c.Update(testMeasurement{rttNs: 2000, label: "backup"})

	result := c.Update(testMeasurement{rttNs: 3000, label: "worse"})
	if result != UpdateNone {
		t.Fatalf("expected UpdateNone, got %v", result)
	}
}

func TestMinCache_EqualRTTReplacesBest(t *testing.T) {
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
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
	c := NewMinCache[testMeasurement](time.Hour, testRttFunc)
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
