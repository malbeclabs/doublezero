package runlog

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func appendRow(t *testing.T, path string, row map[string]any) {
	t.Helper()
	buf, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	buf = append(buf, '\n')
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestProvisionPair: submit followed by activate for the same user yields
// one provision duration and zero deprovision durations.
func TestProvisionPair(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	path := filepath.Join(dir, inputFilename)
	base := time.Now().UnixNano()
	appendRow(t, path, map[string]any{"user_index": 1, "event": "submit", "t_ns": base})
	appendRow(t, path, map[string]any{"user_index": 1, "event": "activate", "t_ns": base + int64(2*time.Second)})

	r.tick()

	got := r.ProvisionDurations(time.Hour)
	if len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("provision durations = %v, want [2s]", got)
	}
	if len(r.DeprovisionDurations(time.Hour)) != 0 {
		t.Errorf("deprovision durations should be empty, got %v", r.DeprovisionDurations(time.Hour))
	}
}

// TestDeprovisionPair: deprovision_submit followed by deprovision_activate
// yields one deprovision duration.
func TestDeprovisionPair(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	path := filepath.Join(dir, inputFilename)
	base := time.Now().UnixNano()
	appendRow(t, path, map[string]any{"user_index": 7, "event": "deprovision_submit", "t_ns": base})
	appendRow(t, path, map[string]any{"user_index": 7, "event": "deprovision_activate", "t_ns": base + int64(time.Second)})

	r.tick()

	got := r.DeprovisionDurations(time.Hour)
	if len(got) != 1 || got[0] != time.Second {
		t.Fatalf("deprovision durations = %v, want [1s]", got)
	}
}

// TestOrphanSubmit: an activate without a prior submit (or a submit
// without a matching activate) does not produce a duration.
func TestOrphanSubmit(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	path := filepath.Join(dir, inputFilename)
	base := time.Now().UnixNano()
	// Activate with no submit.
	appendRow(t, path, map[string]any{"user_index": 1, "event": "activate", "t_ns": base})
	// Submit with no matching activate.
	appendRow(t, path, map[string]any{"user_index": 2, "event": "submit", "t_ns": base})

	r.tick()

	if len(r.ProvisionDurations(time.Hour)) != 0 {
		t.Errorf("orphan should not produce durations, got %v", r.ProvisionDurations(time.Hour))
	}
}

// TestWindowFilter: completions older than the requested window are
// excluded from the returned slice.
func TestWindowFilter(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	path := filepath.Join(dir, inputFilename)

	// Old completion: 1 hour ago.
	oldStart := time.Now().Add(-time.Hour - time.Second).UnixNano()
	oldEnd := time.Now().Add(-time.Hour).UnixNano()
	appendRow(t, path, map[string]any{"user_index": 1, "event": "submit", "t_ns": oldStart})
	appendRow(t, path, map[string]any{"user_index": 1, "event": "activate", "t_ns": oldEnd})

	// Recent completion: ~now.
	now := time.Now().UnixNano()
	appendRow(t, path, map[string]any{"user_index": 2, "event": "submit", "t_ns": now - int64(500*time.Millisecond)})
	appendRow(t, path, map[string]any{"user_index": 2, "event": "activate", "t_ns": now})

	r.tick()

	got := r.ProvisionDurations(10 * time.Minute)
	if len(got) != 1 {
		t.Fatalf("window filter got %d durations, want 1 (recent only)", len(got))
	}
}

// TestRingEviction: pushing more than ringCapacity samples evicts the
// oldest. Inspect the ring directly to avoid coupling to clock-window
// behavior.
func TestRingEviction(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	for i := 0; i < ringCapacity+5; i++ {
		r.provisionRing = pushRing(r.provisionRing, durationSample{at: time.Now(), dur: time.Duration(i)})
	}
	if len(r.provisionRing) != ringCapacity {
		t.Fatalf("ring len = %d, want %d", len(r.provisionRing), ringCapacity)
	}
	// First sample should now be the 6th original (index 5) due to eviction.
	if r.provisionRing[0].dur != time.Duration(5) {
		t.Errorf("oldest after eviction = %v, want 5", r.provisionRing[0].dur)
	}
}

// TestPendingMapBounded: inserting more than maxPending entries evicts
// the oldest by submit time so the map never grows past the cap.
func TestPendingMapBounded(t *testing.T) {
	m := map[int]time.Time{}
	base := time.Unix(0, 0)
	for i := 0; i < maxPending+10; i++ {
		insertPending(m, i, base.Add(time.Duration(i)*time.Second))
	}
	if len(m) != maxPending {
		t.Fatalf("len(map) = %d, want %d", len(m), maxPending)
	}
	// The first 10 keys should have been evicted (oldest by time).
	for i := 0; i < 10; i++ {
		if _, ok := m[i]; ok {
			t.Errorf("key %d should have been evicted", i)
		}
	}
	// The newest keys should still be present.
	for i := maxPending; i < maxPending+10; i++ {
		if _, ok := m[i]; !ok {
			t.Errorf("key %d should be present", i)
		}
	}
}

// TestLateRunlogFile: the runlog file appearing after the first tick is
// picked up on the next poll without error.
func TestLateRunlogFile(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())
	r.tick() // file absent; should be a no-op
	path := filepath.Join(dir, inputFilename)
	base := time.Now().UnixNano()
	appendRow(t, path, map[string]any{"user_index": 1, "event": "submit", "t_ns": base})
	appendRow(t, path, map[string]any{"user_index": 1, "event": "activate", "t_ns": base + int64(time.Second)})
	r.tick()

	if len(r.ProvisionDurations(time.Hour)) != 1 {
		t.Errorf("late file: expected 1 duration, got %d", len(r.ProvisionDurations(time.Hour)))
	}
}

// TestRunCancels: Run returns nil on context cancel.
func TestRunCancels(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, time.Hour, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}
}

var _ collector.Collector = (*Reader)(nil)

// TestActiveUserCountTracksRunlog confirms the reader exposes the most
// recent n_after_event and timestamps the most recent provision activate.
// Order of arrival matters: a later submit/deprovision_submit/activate
// for a different user should also update the count, but only an activate
// event updates lastActivate.
func TestActiveUserCountTracksRunlog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator-runlog.jsonl")
	r := New(dir, time.Hour, discardLogger())

	// Initial: no rows yet, source should signal "not seen".
	r.tick()
	if _, _, ok := r.ActiveUserCount(); ok {
		t.Fatal("ActiveUserCount must report ok=false before any row")
	}

	// Provision two users.
	submit0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	activate0 := submit0.Add(14 * time.Second)
	submit1 := activate0.Add(time.Microsecond)
	activate1 := submit1.Add(14 * time.Second)
	appendRow(t, path, map[string]any{
		"user_index":    0,
		"event":         "submit",
		"t_ns":          submit0.UnixNano(),
		"n_after_event": 0,
	})
	appendRow(t, path, map[string]any{
		"user_index":    0,
		"event":         "activate",
		"t_ns":          activate0.UnixNano(),
		"n_after_event": 1,
	})
	appendRow(t, path, map[string]any{
		"user_index":    1,
		"event":         "submit",
		"t_ns":          submit1.UnixNano(),
		"n_after_event": 1,
	})
	appendRow(t, path, map[string]any{
		"user_index":    1,
		"event":         "activate",
		"t_ns":          activate1.UnixNano(),
		"n_after_event": 2,
	})
	r.tick()

	count, last, ok := r.ActiveUserCount()
	if !ok {
		t.Fatal("ActiveUserCount must report ok=true after seeing rows")
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if !last.Equal(activate1) {
		t.Errorf("lastActivate = %v, want %v", last, activate1)
	}

	// Deprovision the newest user. n_after_event drops to 1; lastActivate
	// stays at activate1 (only provision activates move it forward).
	deprovSubmit := activate1.Add(time.Second)
	deprovActivate := deprovSubmit.Add(14 * time.Second)
	appendRow(t, path, map[string]any{
		"user_index":    1,
		"event":         "deprovision_submit",
		"t_ns":          deprovSubmit.UnixNano(),
		"n_after_event": 2,
	})
	appendRow(t, path, map[string]any{
		"user_index":    1,
		"event":         "deprovision_activate",
		"t_ns":          deprovActivate.UnixNano(),
		"n_after_event": 1,
	})
	r.tick()

	count, last, _ = r.ActiveUserCount()
	if count != 1 {
		t.Errorf("count after deprovision = %d, want 1", count)
	}
	if !last.Equal(activate1) {
		t.Errorf("lastActivate after deprovision = %v, want %v (unchanged)", last, activate1)
	}
}

var _ = context.Background // keep import used if tests above remove it
