package ripeatlas

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInternetLatency_RIPEAtlas_State_New(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	require.NotNil(t, ms)
	require.Equal(t, "test.json", ms.filename)
	require.NotNil(t, ms.tracker)
	require.NotNil(t, ms.tracker.Metadata)
	require.Empty(t, ms.tracker.Metadata)
}

func TestInternetLatency_RIPEAtlas_State_LoadSave(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_timestamps.json")

	ms := NewMeasurementState(filename)

	// Test loading non-existent file
	err := ms.Load()
	require.NoError(t, err, "Load() on non-existent file should not error")
	require.Empty(t, ms.tracker.Metadata, "Metadata should be empty initially")

	// Test saving
	ms.UpdateTimestamp(100, 1640995200)
	ms.UpdateTimestamp(200, 1640995300)
	ms.UpdateTimestamp(300, 1640995400)

	err = ms.Save()
	require.NoError(t, err, "Save() should not error")

	// Test loading existing file
	ms2 := NewMeasurementState(filename)
	err = ms2.Load()
	require.NoError(t, err, "Load() should not error")
	require.Len(t, ms2.tracker.Metadata, 3, "Expected 3 metadata entries")

	// Verify timestamps
	ts, exists := ms2.GetLastTimestamp(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995200), ts)

	ts, exists = ms2.GetLastTimestamp(200)
	require.True(t, exists)
	require.Equal(t, int64(1640995300), ts)

	ts, exists = ms2.GetLastTimestamp(300)
	require.True(t, exists)
	require.Equal(t, int64(1640995400), ts)
}

func TestInternetLatency_RIPEAtlas_State_GetLastTimestamp(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	// Test non-existent measurement
	ts, exists := ms.GetLastTimestamp(999)
	require.False(t, exists)
	require.Equal(t, int64(0), ts)

	// Add timestamp and test
	ms.UpdateTimestamp(100, 1640995200)
	ts, exists = ms.GetLastTimestamp(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995200), ts)
}

func TestInternetLatency_RIPEAtlas_State_UpdateTimestamp(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	// Initial update
	ms.UpdateTimestamp(100, 1640995200)
	ts, exists := ms.GetLastTimestamp(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995200), ts)

	// Update existing timestamp
	ms.UpdateTimestamp(100, 1640995300)
	ts, exists = ms.GetLastTimestamp(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995300), ts)
}

func TestInternetLatency_RIPEAtlas_State_GetAllTimestamps(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	// Test empty state
	timestamps := ms.GetAllTimestamps()
	require.Empty(t, timestamps)

	// Add some timestamps
	ms.UpdateTimestamp(100, 1640995200)
	ms.UpdateTimestamp(200, 1640995300)
	ms.UpdateTimestamp(300, 1640995400)

	timestamps = ms.GetAllTimestamps()
	require.Len(t, timestamps, 3)
	require.Equal(t, int64(1640995200), timestamps[100])
	require.Equal(t, int64(1640995300), timestamps[200])
	require.Equal(t, int64(1640995400), timestamps[300])

	// Verify it's a copy (modifying returned map doesn't affect original)
	timestamps[100] = 9999
	ts, _ := ms.GetLastTimestamp(100)
	require.Equal(t, int64(1640995200), ts, "Original should not be modified")
}

func TestInternetLatency_RIPEAtlas_State_InvalidFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Test corrupted JSON
	jsonFile := filepath.Join(tempDir, "corrupted.json")
	err := os.WriteFile(jsonFile, []byte("{invalid json"), 0644)
	require.NoError(t, err, "Failed to write corrupted JSON file")

	ms := NewMeasurementState(jsonFile)
	err = ms.Load()
	require.Error(t, err, "Expected error for corrupted JSON")
	require.Contains(t, err.Error(), "failed to decode timestamp file")
}

func TestInternetLatency_RIPEAtlas_State_FilePermissionError(t *testing.T) {
	t.Parallel()

	// Skip this test if running as root (common in Docker containers)
	if os.Geteuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tempDir := t.TempDir()

	// Create a directory where we can't write
	readOnlyDir := filepath.Join(tempDir, "readonly")
	err := os.Mkdir(readOnlyDir, 0555) // Read and execute only
	require.NoError(t, err)

	filename := filepath.Join(readOnlyDir, "timestamps.json")
	ms := NewMeasurementState(filename)
	ms.UpdateTimestamp(100, 1640995200)

	err = ms.Save()
	require.Error(t, err, "Expected error when saving to read-only directory")
	require.Contains(t, err.Error(), "failed to create temp timestamp file")
}

func TestInternetLatency_RIPEAtlas_State_EmptyMetadataInFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "empty_metadata.json")

	// Write JSON with null metadata
	err := os.WriteFile(filename, []byte(`{"metadata": null}`), 0644)
	require.NoError(t, err)

	ms := NewMeasurementState(filename)
	err = ms.Load()
	require.NoError(t, err, "Should handle null metadata gracefully")
	require.NotNil(t, ms.tracker.Metadata, "Metadata map should be initialized")
	require.Empty(t, ms.tracker.Metadata, "Metadata should be empty")
}

func TestInternetLatency_RIPEAtlas_State_PersistenceAcrossInstances(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "persistence_test.json")

	// First instance: add some timestamps
	ms1 := NewMeasurementState(filename)
	ms1.UpdateTimestamp(100, 1640995200)
	ms1.UpdateTimestamp(200, 1640995300)
	err := ms1.Save()
	require.NoError(t, err)

	// Second instance: load and add more
	ms2 := NewMeasurementState(filename)
	err = ms2.Load()
	require.NoError(t, err)

	// Verify existing timestamps
	ts, exists := ms2.GetLastTimestamp(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995200), ts)

	// Add new timestamp
	ms2.UpdateTimestamp(300, 1640995400)
	err = ms2.Save()
	require.NoError(t, err)

	// Third instance: verify all timestamps
	ms3 := NewMeasurementState(filename)
	err = ms3.Load()
	require.NoError(t, err)

	timestamps := ms3.GetAllTimestamps()
	require.Len(t, timestamps, 3)
	require.Equal(t, int64(1640995200), timestamps[100])
	require.Equal(t, int64(1640995300), timestamps[200])
	require.Equal(t, int64(1640995400), timestamps[300])
}

func TestInternetLatency_RIPEAtlas_State_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "concurrent_test.json")
	ms := NewMeasurementState(filename)

	// Simulate the race that caused the create-destroy-create loop:
	// one goroutine sets metadata + saves (management), while another
	// reads timestamps + saves (export). With a shared instance, the
	// export goroutine's Save must not lose the management goroutine's
	// metadata.
	var wg sync.WaitGroup

	// Management goroutine: creates measurements with metadata
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ms.SetMetadata(i, MeasurementMeta{
				TargetLocation: "test",
				TargetProbeID:  i,
				CreatedAt:      int64(1000 + i),
			})
			_ = ms.Save()
		}
	}()

	// Export goroutine: reads and updates timestamps, then saves
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ms.UpdateTimestamp(i, int64(2000+i))
			_ = ms.Save()
		}
	}()

	wg.Wait()

	// All 100 measurements must still have metadata — none should be lost
	allMeta := ms.GetAllMetadata()
	require.Len(t, allMeta, 100, "all metadata entries should be present after concurrent access")
	for i := 0; i < 100; i++ {
		meta, exists := ms.GetMetadata(i)
		require.True(t, exists, "metadata for measurement %d should exist", i)
		require.Equal(t, "test", meta.TargetLocation)
		require.Equal(t, i, meta.TargetProbeID)
	}
}

func TestInternetLatency_RIPEAtlas_State_LargeNumberOfMeasurements(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "large_test.json")

	ms := NewMeasurementState(filename)

	// Add many measurements
	const numMeasurements = 1000
	for i := 0; i < numMeasurements; i++ {
		ms.UpdateTimestamp(i, int64(1640995200+i))
	}

	// Save and reload
	err := ms.Save()
	require.NoError(t, err)

	ms2 := NewMeasurementState(filename)
	err = ms2.Load()
	require.NoError(t, err)

	// Verify all timestamps
	timestamps := ms2.GetAllTimestamps()
	require.Len(t, timestamps, numMeasurements)

	for i := 0; i < numMeasurements; i++ {
		require.Equal(t, int64(1640995200+i), timestamps[i])
	}
}

func TestInternetLatency_RIPEAtlas_State_UpdateSourceProbeResponse(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	// Set up a measurement with source probes
	ms.SetMetadata(100, MeasurementMeta{
		TargetLocation: "xams",
		TargetProbeID:  6626,
		Sources: []SourceProbeMeta{
			{LocationCode: "xsin", ProbeID: 6726},
			{LocationCode: "xtyo", ProbeID: 7080},
		},
		CreatedAt: 1640995200,
	})

	// Update source probe response
	ms.UpdateSourceProbeResponse(100, 6726, 1640995300)
	meta, exists := ms.GetMetadata(100)
	require.True(t, exists)
	require.Equal(t, int64(1640995300), meta.Sources[0].LastResponseAt)
	require.Equal(t, int64(0), meta.Sources[1].LastResponseAt)

	// Update with newer timestamp
	ms.UpdateSourceProbeResponse(100, 6726, 1640995400)
	meta, _ = ms.GetMetadata(100)
	require.Equal(t, int64(1640995400), meta.Sources[0].LastResponseAt)

	// Update with older timestamp (should not regress)
	ms.UpdateSourceProbeResponse(100, 6726, 1640995200)
	meta, _ = ms.GetMetadata(100)
	require.Equal(t, int64(1640995400), meta.Sources[0].LastResponseAt)

	// Update for non-existent measurement (should not panic)
	ms.UpdateSourceProbeResponse(999, 6726, 1640995500)

	// Update for non-existent probe (should not panic)
	ms.UpdateSourceProbeResponse(100, 9999, 1640995500)
}

func TestInternetLatency_RIPEAtlas_State_UnresponsiveProbeExpiry(t *testing.T) {
	t.Parallel()

	ms := NewMeasurementState("test.json")

	// Add a probe marked now — should be unresponsive
	ms.AddUnresponsiveProbe(100)
	require.True(t, ms.IsProbeUnresponsive(100))
	require.Equal(t, []int{100}, ms.GetUnresponsiveProbes())

	// Manually backdate the entry to 25 hours ago (past the 24h expiry)
	ms.mu.Lock()
	ms.tracker.UnresponsiveProbes[0].MarkedAt = time.Now().Add(-25 * time.Hour).Unix()
	ms.mu.Unlock()

	// Should no longer be considered unresponsive
	require.False(t, ms.IsProbeUnresponsive(100))
	require.Empty(t, ms.GetUnresponsiveProbes())

	// Prune should remove it
	pruned := ms.PruneExpiredUnresponsiveProbes()
	require.Equal(t, 1, pruned)

	ms.mu.Lock()
	require.Empty(t, ms.tracker.UnresponsiveProbes)
	ms.mu.Unlock()
}

func TestInternetLatency_RIPEAtlas_State_UnresponsiveProbeBackwardsCompat(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "legacy_state.json")

	// Write legacy format with bare int array
	legacyJSON := `{
		"metadata": {},
		"unresponsive_probes": [7466, 1234]
	}`
	err := os.WriteFile(filename, []byte(legacyJSON), 0644)
	require.NoError(t, err)

	ms := NewMeasurementState(filename)
	err = ms.Load()
	require.NoError(t, err)

	// Legacy probes should be migrated and treated as freshly marked (not expired)
	require.True(t, ms.IsProbeUnresponsive(7466))
	require.True(t, ms.IsProbeUnresponsive(1234))
	require.ElementsMatch(t, []int{7466, 1234}, ms.GetUnresponsiveProbes())

	// Save in new format and reload
	err = ms.Save()
	require.NoError(t, err)

	ms2 := NewMeasurementState(filename)
	err = ms2.Load()
	require.NoError(t, err)
	require.True(t, ms2.IsProbeUnresponsive(7466))
	require.True(t, ms2.IsProbeUnresponsive(1234))
}

func TestInternetLatency_RIPEAtlas_State_TimestampTracker_Structure(t *testing.T) {
	t.Parallel()

	// Test that TimestampTracker can be marshaled/unmarshaled correctly
	tracker := &MetadataTracker{
		Metadata: map[int]MeasurementMeta{
			100: {
				TargetLocation: "lax",
				TargetProbeID:  999,
				Sources: []SourceProbeMeta{
					{LocationCode: "nyc", ProbeID: 100},
					{LocationCode: "chi", ProbeID: 101},
				},
				CreatedAt:    1640995200,
				LastExportAt: 1640995300,
			},
		},
	}

	// Marshal
	data, err := json.Marshal(tracker)
	require.NoError(t, err)

	// Unmarshal
	var tracker2 MetadataTracker
	err = json.Unmarshal(data, &tracker2)
	require.NoError(t, err)

	require.Equal(t, tracker.Metadata, tracker2.Metadata)
}

func TestInternetLatency_RIPEAtlas_State_EvaluateTargetLoss(t *testing.T) {
	t.Parallel()

	const measurementID = 42
	windowSecs := int64(TargetLossWindow.Seconds())
	graceSecs := int64(TargetLossWindowGrace.Seconds())

	newState := func() *MeasurementState {
		ms := NewMeasurementState(filepath.Join(t.TempDir(), "state.json"))
		ms.SetMetadata(measurementID, MeasurementMeta{TargetLocation: "cmh", TargetProbeID: 7})
		return ms
	}

	t.Run("lossy target is flagged once the window closes", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		// 15% success, the rate observed against a NAT'd target probe.
		ms.RecordTargetResults(measurementID, 100, 15, start, start)

		lossy, _, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs-graceSecs-1)
		require.False(t, lossy, "window should not be judged before its grace begins")

		lossy, attempts, successes := ms.EvaluateTargetLoss(measurementID, start+windowSecs)
		require.True(t, lossy)
		require.Equal(t, int64(100), attempts)
		require.Equal(t, int64(15), successes)
	})

	t.Run("a window a little short of the hour is still judged", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		ms.RecordTargetResults(measurementID, 100, 15, start, start)

		// The management cycle fires an hour apart but judges against a clock that
		// includes its own pre-work, so a faster cycle lands seconds short. Without the
		// grace that slips the verdict a whole further hour, at random.
		lossy, attempts, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs-30)
		require.True(t, lossy, "a cycle arriving 30s early must still render a verdict")
		require.Equal(t, int64(100), attempts)
	})

	t.Run("healthy target is not flagged", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		ms.RecordTargetResults(measurementID, 100, 99, start, start)

		lossy, _, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs)
		require.False(t, lossy)
	})

	t.Run("sparse window is not judged but keeps accumulating", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		// Total loss, but too few attempts to tell a lossy target from a quiet one.
		ms.RecordTargetResults(measurementID, MinTargetAttemptsForLossCheck-1, 0, start, start)

		lossy, attempts, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs)
		require.False(t, lossy)
		require.Equal(t, int64(MinTargetAttemptsForLossCheck-1), attempts)

		// The short window stays open rather than discarding its evidence, so a
		// measurement with too few sources to reach the minimum in one hour is still
		// judged once enough attempts accumulate — provided it gets there inside
		// MaxTargetLossWindowAge, past which the tallies are dropped unjudged.
		ms.RecordTargetResults(measurementID, 1, 0, start+windowSecs, start+windowSecs)

		lossy, attempts, successes := ms.EvaluateTargetLoss(measurementID, start+2*windowSecs-graceSecs-60)
		require.True(t, lossy, "accumulated window should be judged once it reaches the minimum")
		require.Equal(t, int64(MinTargetAttemptsForLossCheck), attempts)
		require.Zero(t, successes)
	})

	t.Run("a short window is dropped rather than judged once it ages out", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		// Two sources at the 10 minute sampling interval give 12 attempts an hour, so
		// the window never reaches the minimum inside one hour. Left to accumulate it
		// would pool two lossy hours with one recovered one (36 attempts, 14 successes,
		// 61% loss) and mark a target that has answered every recent ping.
		ms.RecordTargetResults(measurementID, 12, 1, start, start)
		lossy, attempts, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs)
		require.False(t, lossy)
		require.Equal(t, int64(12), attempts, "one short window keeps its evidence")

		ms.RecordTargetResults(measurementID, 12, 1, start+windowSecs, start+windowSecs)
		lossy, attempts, _ = ms.EvaluateTargetLoss(measurementID, start+2*windowSecs)
		require.False(t, lossy)
		require.Equal(t, int64(24), attempts, "two short windows are still inside the cap")

		// Hour 3: the target answers everything, and the window is now past the cap.
		// Pooled it would read 36/14, 61% loss, and mark a probe that has just answered
		// every ping; instead the evidence is dropped and the window starts over.
		ms.RecordTargetResults(measurementID, 12, 12, start+2*windowSecs, start+2*windowSecs)
		lossy, attempts, successes := ms.EvaluateTargetLoss(measurementID, start+3*windowSecs)
		require.False(t, lossy, "a recovered target must not be marked on evidence from an old outage")
		require.Equal(t, int64(36), attempts, "the dropped window reports what it held")
		require.Equal(t, int64(14), successes)

		meta, ok := ms.GetMetadata(measurementID)
		require.True(t, ok)
		require.Zero(t, meta.TargetAttempts, "the aged-out tallies are cleared")
		require.Equal(t, start+3*windowSecs, meta.TargetWindowStart)
	})

	t.Run("a window ready to judge just under the cap is judged", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		capSecs := int64(MaxTargetLossWindowAge.Seconds())
		start := time.Now().Unix()

		// A 3- or 4-source measurement reaches the minimum in its second window, so it
		// arrives here at an age of about two windows. Without the grace on the cap,
		// cycle drift of a few seconds either side would decide between this verdict
		// and discarding the evidence.
		ms.RecordTargetResults(measurementID, 36, 6, start, start)

		lossy, attempts, successes := ms.EvaluateTargetLoss(measurementID, start+capSecs-30)
		require.True(t, lossy, "evidence sitting right on the cap must still be judged")
		require.Equal(t, int64(36), attempts)
		require.Equal(t, int64(6), successes)

		// And a few seconds the other side of the bare cap, which is the half of the
		// coin flip a capless bound got wrong.
		late := newState()
		lateStart := time.Now().Unix()
		late.RecordTargetResults(measurementID, 36, 6, lateStart, lateStart)

		lossy, _, _ = late.EvaluateTargetLoss(measurementID, lateStart+capSecs+10)
		require.True(t, lossy, "a cycle landing just past two windows must judge, not discard")
	})

	t.Run("a window past the cap and its grace is dropped even when judgeable", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		capSecs := int64(MaxTargetLossWindowAge.Seconds())
		start := time.Now().Unix()

		ms.RecordTargetResults(measurementID, 36, 6, start, start)

		now := start + capSecs + graceSecs + 30
		lossy, attempts, _ := ms.EvaluateTargetLoss(measurementID, now)
		require.False(t, lossy, "past the cap the evidence is too old to convict on")
		require.Equal(t, int64(36), attempts, "the dropped window still reports what it held")

		meta, ok := ms.GetMetadata(measurementID)
		require.True(t, ok)
		require.Zero(t, meta.TargetAttempts)
		require.Zero(t, meta.TargetSuccesses)
		require.Equal(t, now, meta.TargetWindowStart, "the window restarts from now")
	})

	t.Run("window resets after evaluation", func(t *testing.T) {
		t.Parallel()
		ms := newState()
		start := time.Now().Unix()

		ms.RecordTargetResults(measurementID, 100, 10, start, start)
		lossy, _, _ := ms.EvaluateTargetLoss(measurementID, start+windowSecs)
		require.True(t, lossy)

		// A recovered probe is judged on the new window alone, not the old loss.
		ms.RecordTargetResults(measurementID, 100, 100, start+windowSecs, start+windowSecs)
		lossy, attempts, successes := ms.EvaluateTargetLoss(measurementID, start+2*windowSecs)
		require.False(t, lossy)
		require.Equal(t, int64(100), attempts)
		require.Equal(t, int64(100), successes)
	})

	t.Run("unknown measurement is ignored", func(t *testing.T) {
		t.Parallel()
		ms := newState()

		ms.RecordTargetResults(999, 100, 0, time.Now().Unix(), time.Now().Unix())
		lossy, attempts, _ := ms.EvaluateTargetLoss(999, time.Now().Unix()+windowSecs)
		require.False(t, lossy)
		require.Zero(t, attempts)
	})
}

func TestInternetLatency_RIPEAtlas_State_UnresponsiveTargetsRoundTrip(t *testing.T) {
	t.Parallel()

	const targetProbeID = 12651
	stateFile := filepath.Join(t.TempDir(), "state.json")

	saved := NewMeasurementState(stateFile)
	saved.AddUnresponsiveTarget(targetProbeID)
	require.NoError(t, saved.Save())

	// Load decodes through an intermediate struct. Omitting unresponsive_targets there
	// left the tracker nil after every restart, and the next Save dropped the field,
	// erasing the list roughly ten minutes into each process lifetime.
	loaded := NewMeasurementState(stateFile)
	require.NoError(t, loaded.Load())

	require.True(t, loaded.IsTargetUnresponsive(targetProbeID))
	require.Equal(t, []int{targetProbeID}, loaded.GetUnresponsiveTargets())

	// And it survives being written back out.
	require.NoError(t, loaded.Save())
	reloaded := NewMeasurementState(stateFile)
	require.NoError(t, reloaded.Load())
	require.Equal(t, []int{targetProbeID}, reloaded.GetUnresponsiveTargets())

	// A target mark must not bar the probe from sourcing measurements.
	require.False(t, reloaded.IsProbeUnresponsive(targetProbeID))
}

func TestInternetLatency_RIPEAtlas_State_RepeatMarkRefreshesMarkedAt(t *testing.T) {
	t.Parallel()

	const probeID = 12651

	// markedAt reads a probe's timestamp back out of the persisted file, which is the
	// only place it is observable.
	markedAt := func(t *testing.T, file string, pick func(MetadataTracker) []UnresponsiveProbeEntry) int64 {
		t.Helper()
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		var tracker MetadataTracker
		require.NoError(t, json.Unmarshal(raw, &tracker))
		for _, entry := range pick(tracker) {
			if entry.ProbeID == probeID {
				return entry.MarkedAt
			}
		}
		t.Fatalf("probe %d absent from the list under test", probeID)
		return 0
	}

	for _, tc := range []struct {
		name string
		pick func(MetadataTracker) []UnresponsiveProbeEntry
		mark func(*MeasurementState)
	}{
		{
			"targets",
			func(t MetadataTracker) []UnresponsiveProbeEntry { return t.UnresponsiveTargets },
			func(ms *MeasurementState) { ms.AddUnresponsiveTarget(probeID) },
		},
		{
			"sources",
			func(t MetadataTracker) []UnresponsiveProbeEntry { return t.UnresponsiveProbes },
			func(ms *MeasurementState) { ms.AddUnresponsiveProbe(probeID) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stateFile := filepath.Join(t.TempDir(), "state.json")

			// A mark made 23h ago: still live, but an hour from expiring.
			stale := time.Now().Add(-23 * time.Hour).Unix()
			seeded := NewMeasurementState(stateFile)
			tc.mark(seeded)
			require.NoError(t, seeded.Save())
			raw, err := os.ReadFile(stateFile)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(stateFile,
				[]byte(strings.Replace(string(raw),
					fmt.Sprintf("%d", markedAt(t, stateFile, tc.pick)),
					fmt.Sprintf("%d", stale), 1)), 0o600))

			ms := NewMeasurementState(stateFile)
			require.NoError(t, ms.Load())
			require.NoError(t, ms.Save())
			require.Equal(t, stale, markedAt(t, stateFile, tc.pick), "the seeded mark should load unchanged")

			// The collector judges the probe bad again. Discarding that as a duplicate
			// would let the mark lapse an hour later and the probe be re-adopted.
			tc.mark(ms)
			require.NoError(t, ms.Save())

			require.Greater(t, markedAt(t, stateFile, tc.pick), stale,
				"fresh failure evidence must extend the mark")
		})
	}
}

func TestInternetLatency_RIPEAtlas_State_MigratesLegacyTargetMarks(t *testing.T) {
	t.Parallel()

	const targetProbe = 1009793 // marked by the old staleness path, as a measurement target
	const sourceProbe = 6726    // marked by Step 4b, genuinely a source failure

	stateFile := filepath.Join(t.TempDir(), "state.json")

	// A file as an earlier build wrote it: both roles in one list, no migration flag.
	markedAt := time.Now().Add(-6 * time.Hour).Unix()
	legacy := fmt.Sprintf(`{
	  "metadata": {
	    "1001": {"target_location": "cmh", "target_probe_id": %d, "sources": [
	      {"location_code": "nyc", "probe_id": %d}
	    ], "created_at": %d}
	  },
	  "unresponsive_probes": [
	    {"probe_id": %d, "marked_at": %d},
	    {"probe_id": %d, "marked_at": %d}
	  ]
	}`, targetProbe, sourceProbe, markedAt, targetProbe, markedAt, sourceProbe, markedAt)
	require.NoError(t, os.WriteFile(stateFile, []byte(legacy), 0o600))

	ms := NewMeasurementState(stateFile)
	require.NoError(t, ms.Load())

	require.Equal(t, 1, ms.MigratedTargetMarks())
	require.Equal(t, []int{targetProbe}, ms.GetUnresponsiveTargets())

	// The target failure no longer bars the probe from sourcing, which is the cascade
	// this avoids paying once more on the cycle after deploy.
	require.False(t, ms.IsProbeUnresponsive(targetProbe))
	require.True(t, ms.IsTargetUnresponsive(targetProbe))

	// The genuine source failure is left where it was.
	require.True(t, ms.IsProbeUnresponsive(sourceProbe))
	require.Equal(t, []int{sourceProbe}, ms.GetUnresponsiveProbes())

	// MarkedAt carries over, so the mark expires when it always would have rather than
	// being extended by the upgrade.
	require.NoError(t, ms.Save())
	raw, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	var saved MetadataTracker
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Len(t, saved.UnresponsiveTargets, 1)
	require.Equal(t, markedAt, saved.UnresponsiveTargets[0].MarkedAt)

	// And it runs once: a source marked afterwards that happens to be some measurement's
	// target probe is not reclassified on the next start.
	ms.AddUnresponsiveProbe(targetProbe)
	require.NoError(t, ms.Save())

	reloaded := NewMeasurementState(stateFile)
	require.NoError(t, reloaded.Load())
	require.Zero(t, reloaded.MigratedTargetMarks(), "the migration must not run twice")
	require.True(t, reloaded.IsProbeUnresponsive(targetProbe))
}

func TestInternetLatency_RIPEAtlas_State_UnresponsiveTargetExpiry(t *testing.T) {
	t.Parallel()

	const freshProbe = 55128
	const staleProbe = 12651

	ms := NewMeasurementState(filepath.Join(t.TempDir(), "state.json"))
	ms.AddUnresponsiveTarget(freshProbe)
	ms.AddUnresponsiveTarget(staleProbe)

	// Backdate one mark past the expiry. Under rank-last a mark that never expires pins
	// a metro to a dead probe rather than retrying it, which is the opposite of intended.
	ms.mu.Lock()
	for i := range ms.tracker.UnresponsiveTargets {
		if ms.tracker.UnresponsiveTargets[i].ProbeID == staleProbe {
			ms.tracker.UnresponsiveTargets[i].MarkedAt = time.Now().Add(-25 * time.Hour).Unix()
		}
	}
	ms.mu.Unlock()

	require.False(t, ms.IsTargetUnresponsive(staleProbe), "an expired mark should not demote the probe")
	require.True(t, ms.IsTargetUnresponsive(freshProbe))
	require.Equal(t, []int{freshProbe}, ms.GetUnresponsiveTargets(), "expired entries are not reported")

	require.Equal(t, 1, ms.PruneExpiredUnresponsiveTargets(), "the expired entry should be pruned")
	require.Equal(t, 0, ms.PruneExpiredUnresponsiveTargets(), "and pruning is idempotent")

	require.Equal(t, []int{freshProbe}, ms.GetUnresponsiveTargets())
	require.True(t, ms.IsTargetUnresponsive(freshProbe), "the live mark survives the prune")
}

func TestInternetLatency_RIPEAtlas_State_SaveIsAtomic(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "timestamps.json")

	ms := NewMeasurementState(filename)
	ms.SetMetadata(100, MeasurementMeta{TargetLocation: "nyc", TargetProbeID: 1})
	require.NoError(t, ms.Save())

	// A leftover .tmp would accumulate once per management cycle.
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the state file should remain")
	require.Equal(t, "timestamps.json", entries[0].Name())

	firstStat, err := os.Stat(filename)
	require.NoError(t, err)

	// A rename leaves a different inode; a truncate-in-place would keep the same one.
	ms.SetMetadata(200, MeasurementMeta{TargetLocation: "lon", TargetProbeID: 2})
	require.NoError(t, ms.Save())

	secondStat, err := os.Stat(filename)
	require.NoError(t, err)
	require.False(t, os.SameFile(firstStat, secondStat), "Save should replace the state file, not truncate it in place")
	require.Equal(t, firstStat.Mode().Perm(), secondStat.Mode().Perm(), "Save should preserve the file mode")

	entries, err = os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the state file should remain after a replacing save")

	reloaded := NewMeasurementState(filename)
	require.NoError(t, reloaded.Load(), "the saved state file should decode")
	require.Len(t, reloaded.GetAllMetadata(), 2)
	require.Equal(t, "nyc", reloaded.GetAllMetadata()[100].TargetLocation)
	require.Equal(t, "lon", reloaded.GetAllMetadata()[200].TargetLocation)
}

func TestInternetLatency_RIPEAtlas_State_SaveFailureLeavesTargetIntact(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	target := filepath.Join(tempDir, "timestamps.json")
	ms := NewMeasurementState(target)
	ms.SetMetadata(100, MeasurementMeta{TargetLocation: "nyc", TargetProbeID: 1})
	require.NoError(t, ms.Save())

	// A directory at the target path fails the save at the rename, which is the step a
	// kill would interrupt.
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Mkdir(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "sentinel"), []byte("intact"), 0644))

	err := ms.Save()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to replace timestamp file")

	sentinel, err := os.ReadFile(filepath.Join(target, "sentinel"))
	require.NoError(t, err, "the target should be untouched by a failed save")
	require.Equal(t, "intact", string(sentinel))

	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a failed save should leave no temp file behind")
}

func TestInternetLatency_RIPEAtlas_State_SaveRefusesUnloadedFile(t *testing.T) {
	t.Parallel()

	corrupt := []byte("{truncated")

	t.Run("corrupt file that failed to load", func(t *testing.T) {
		t.Parallel()

		filename := filepath.Join(t.TempDir(), "timestamps.json")
		require.NoError(t, os.WriteFile(filename, corrupt, 0644))

		ms := NewMeasurementState(filename)
		require.Error(t, ms.Load())

		err := ms.Save()
		require.Error(t, err, "an unread file must not be overwritten")
		require.Contains(t, err.Error(), "never loaded")

		onDisk, err := os.ReadFile(filename)
		require.NoError(t, err)
		require.Equal(t, corrupt, onDisk, "the unreadable file must be left for an operator to recover")
	})

	t.Run("no file on disk", func(t *testing.T) {
		t.Parallel()

		filename := filepath.Join(t.TempDir(), "timestamps.json")
		ms := NewMeasurementState(filename)
		require.NoError(t, ms.Load(), "a missing file is a clean empty state")
		ms.SetMetadata(100, MeasurementMeta{TargetLocation: "nyc", TargetProbeID: 1})
		require.NoError(t, ms.Save())

		reloaded := NewMeasurementState(filename)
		require.NoError(t, reloaded.Load())
		require.Len(t, reloaded.GetAllMetadata(), 1)
	})

	t.Run("valid file that loaded", func(t *testing.T) {
		t.Parallel()

		filename := filepath.Join(t.TempDir(), "timestamps.json")
		require.NoError(t, os.WriteFile(filename, []byte(`{"metadata": {"100": {"target_location": "nyc"}}}`), 0644))

		ms := NewMeasurementState(filename)
		require.NoError(t, ms.Load())
		ms.SetMetadata(200, MeasurementMeta{TargetLocation: "lon", TargetProbeID: 2})
		require.NoError(t, ms.Save())

		reloaded := NewMeasurementState(filename)
		require.NoError(t, reloaded.Load())
		require.Len(t, reloaded.GetAllMetadata(), 2)
	})
}

// TestInternetLatency_RIPEAtlas_State_LoadClearsLoadedOnFailure verifies that a failed reload
// drops the trust established by an earlier success. Leaving the flag set let Save overwrite
// the unreadable replacement with the stale in-memory tracker, bypassing its own guard.
func TestInternetLatency_RIPEAtlas_State_LoadClearsLoadedOnFailure(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "timestamps.json")
	ms := NewMeasurementState(filename)
	ms.SetMetadata(100, MeasurementMeta{TargetLocation: "nyc", TargetProbeID: 1})
	require.NoError(t, ms.Save())
	require.NoError(t, ms.Load())

	corrupt := []byte("{truncated")
	require.NoError(t, os.WriteFile(filename, corrupt, 0644))
	require.Error(t, ms.Load(), "a truncated replacement must not load")

	err := ms.Save()
	require.Error(t, err, "the earlier successful load must not still authorize a save")
	require.Contains(t, err.Error(), "never loaded")

	onDisk, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.Equal(t, corrupt, onDisk, "the unreadable file must be left for an operator to recover")
}

// TestInternetLatency_RIPEAtlas_State_LoadRejectsUnreadablePaths verifies that only a path with
// nothing at it reads as a clean first deploy. A dangling symlink and a missing parent directory
// both surface as ENOENT, and treating either as an empty fleet is what reconciliation acts on
// by deleting every live measurement.
func TestInternetLatency_RIPEAtlas_State_LoadRejectsUnreadablePaths(t *testing.T) {
	t.Parallel()

	t.Run("dangling symlink", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		link := filepath.Join(tempDir, "timestamps.json")
		require.NoError(t, os.Symlink(filepath.Join(tempDir, "absent-target.json"), link))

		ms := NewMeasurementState(link)
		require.Error(t, ms.Load(), "a broken link is not a clean empty state")

		require.Error(t, ms.Save(), "an unloaded state must not replace the link")
		info, err := os.Lstat(link)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&os.ModeSymlink, "the link must survive for an operator to repair")
	})

	t.Run("missing parent directory", func(t *testing.T) {
		t.Parallel()

		ms := NewMeasurementState(filepath.Join(t.TempDir(), "absent-dir", "timestamps.json"))
		require.Error(t, ms.Load(), "an unavailable directory is not a clean empty state")
	})

	t.Run("absent file in a real directory", func(t *testing.T) {
		t.Parallel()

		ms := NewMeasurementState(filepath.Join(t.TempDir(), "timestamps.json"))
		require.NoError(t, ms.Load(), "a first deploy must still load cleanly")
	})
}

// TestInternetLatency_RIPEAtlas_State_SaveWritesThroughSymlink verifies that Save renames onto a
// symlink's target rather than over the link. Replacing the link detaches the state file from a
// persistent volume, so every measurement recorded afterwards is lost at the next redeploy.
func TestInternetLatency_RIPEAtlas_State_SaveWritesThroughSymlink(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	realDir := filepath.Join(tempDir, "persistent")
	require.NoError(t, os.Mkdir(realDir, 0755))
	realTarget := filepath.Join(realDir, "timestamps.json")
	require.NoError(t, os.WriteFile(realTarget, []byte(`{"metadata": {}}`), 0644))

	link := filepath.Join(tempDir, "timestamps.json")
	require.NoError(t, os.Symlink(realTarget, link))

	ms := NewMeasurementState(link)
	require.NoError(t, ms.Load())
	ms.SetMetadata(100, MeasurementMeta{TargetLocation: "nyc", TargetProbeID: 1})
	require.NoError(t, ms.Save())

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "the save must not convert the link into a regular file")

	reloaded := NewMeasurementState(realTarget)
	require.NoError(t, reloaded.Load())
	require.Len(t, reloaded.GetAllMetadata(), 1, "the write must land on the link's target")
}

// TestInternetLatency_RIPEAtlas_State_SavePermissions verifies that a recreated state file does
// not widen to os.Create's umask-dependent default, and that a deliberately widened existing
// file keeps its mode.
func TestInternetLatency_RIPEAtlas_State_SavePermissions(t *testing.T) {
	t.Parallel()

	t.Run("new file is owner-only", func(t *testing.T) {
		t.Parallel()

		filename := filepath.Join(t.TempDir(), "timestamps.json")
		ms := NewMeasurementState(filename)
		require.NoError(t, ms.Load())
		require.NoError(t, ms.Save())

		info, err := os.Stat(filename)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(),
			"measurement metadata must not be world-readable just because the file was recreated")
	})

	t.Run("existing mode is preserved", func(t *testing.T) {
		t.Parallel()

		filename := filepath.Join(t.TempDir(), "timestamps.json")
		require.NoError(t, os.WriteFile(filename, []byte(`{"metadata": {}}`), 0644))
		require.NoError(t, os.Chmod(filename, 0644))

		ms := NewMeasurementState(filename)
		require.NoError(t, ms.Load())
		require.NoError(t, ms.Save())

		info, err := os.Stat(filename)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0644), info.Mode().Perm())
	})
}

// TestInternetLatency_RIPEAtlas_State_LoadPrunesStaleTempFiles verifies the sweep for temp files
// Save's deferred cleanup could not remove. Save runs once per created measurement, so a
// crash-looping rebuild strands a full copy of the state each time.
func TestInternetLatency_RIPEAtlas_State_LoadPrunesStaleTempFiles(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "timestamps.json")
	require.NoError(t, os.WriteFile(filename, []byte(`{"metadata": {}}`), 0644))

	stale := filename + tempFileSuffix + "123456"
	require.NoError(t, os.WriteFile(stale, []byte("abandoned"), 0600))
	old := time.Now().Add(-2 * staleTempFileAge)
	require.NoError(t, os.Chtimes(stale, old, old))

	// A temp file another process may still be writing must survive the sweep.
	fresh := filename + tempFileSuffix + "999999"
	require.NoError(t, os.WriteFile(fresh, []byte("in flight"), 0600))

	require.NoError(t, NewMeasurementState(filename).Load())

	require.NoFileExists(t, stale, "an abandoned temp file should be pruned")
	require.FileExists(t, fresh, "a recent temp file may belong to an in-flight save")
	require.FileExists(t, filename)
}
