package ripeatlas

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	require.Contains(t, err.Error(), "failed to create timestamp file")
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
			{FacilityCode: "xsin", ProbeID: 6726},
			{FacilityCode: "xtyo", ProbeID: 7080},
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
					{FacilityCode: "nyc", ProbeID: 100},
					{FacilityCode: "chi", ProbeID: 101},
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
