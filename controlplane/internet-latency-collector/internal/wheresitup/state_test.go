package wheresitup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Wheresitup_State_LoadSave(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)

	// Test loading non-existent file
	err := jt.Load()
	require.NoError(t, err, "Load() on non-existent file should not error")
	require.Empty(t, jt.Jobs, "Jobs should be empty initially")

	// Test saving via AddJobIDs
	err = jt.AddJobIDs([]string{"job1", "job2", "job3"})
	require.NoError(t, err, "AddJobIDs() should not error")

	// Test loading existing file
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")
	jobIDs := jt2.GetJobIDs()
	require.Len(t, jobIDs, 3, "Expected 3 job IDs")
	require.Equal(t, []string{"job1", "job2", "job3"}, jobIDs, "JobIDs should match expected values")
}

func TestInternetLatency_Wheresitup_State_AddJobIDs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)

	// Add first batch
	err := jt.AddJobIDs([]string{"job1", "job2"})
	require.NoError(t, err, "AddJobIDs() should not error")

	// Add second batch
	err = jt.AddJobIDs([]string{"job3", "job4"})
	require.NoError(t, err, "AddJobIDs() should not error")

	// Verify all jobs are saved
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")
	jobIDs := jt2.GetJobIDs()
	require.Len(t, jobIDs, 4, "Expected 4 job IDs")
}

func TestInternetLatency_Wheresitup_State_RemoveJobIDs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)
	err := jt.AddJobIDs([]string{"job1", "job2", "job3", "job4"})
	require.NoError(t, err, "AddJobIDs() should not error")

	// Remove some jobs
	err = jt.RemoveJobIDs([]string{"job2", "job4"})
	require.NoError(t, err, "RemoveJobIDs() should not error")

	// Verify only job1 and job3 remain
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")
	jobIDs := jt2.GetJobIDs()
	require.Len(t, jobIDs, 2, "Expected 2 job IDs")
	require.Equal(t, []string{"job1", "job3"}, jobIDs, "Expected [job1, job3]")
}

func TestInternetLatency_Wheresitup_State_InvalidFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Test non-JSON file
	filename := filepath.Join(tempDir, "test.txt")
	jt := NewState(filename)
	err := jt.Save()
	require.Error(t, err, "Expected error for non-JSON file")

	// Test corrupted JSON - should start fresh instead of erroring
	jsonFile := filepath.Join(tempDir, "corrupted.json")
	err = os.WriteFile(jsonFile, []byte("{invalid json"), 0644)
	require.NoError(t, err, "Failed to write corrupted JSON file")
	jt2 := NewState(jsonFile)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error on corrupted JSON, should start fresh")
	require.Empty(t, jt2.Jobs, "Jobs should be empty after loading corrupted file")
}

func TestInternetLatency_Wheresitup_State_GetJobIDs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)
	err := jt.AddJobIDs([]string{"job1", "job2"})
	require.NoError(t, err, "AddJobIDs() should not error")

	jobIDs := jt.GetJobIDs()
	require.Len(t, jobIDs, 2, "GetJobIDs() should return 2 items")
}

func TestInternetLatency_Wheresitup_State_JobExpiration(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)

	// Add jobs with different timestamps
	now := time.Now()
	jt.Jobs = []JobEntry{
		{JobID: "recent", CreatedAt: now.Add(-30 * time.Minute)},
		{JobID: "old", CreatedAt: now.Add(-3 * time.Hour)},
		{JobID: "very_recent", CreatedAt: now.Add(-5 * time.Minute)},
	}

	// Save should filter out the old job
	err := jt.Save()
	require.NoError(t, err, "Save() should not error")

	// Load and verify only recent jobs remain
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")

	jobIDs := jt2.GetJobIDs()
	require.Len(t, jobIDs, 2, "Expected 2 jobs after filtering")
	require.Contains(t, jobIDs, "recent", "Recent job should be present")
	require.Contains(t, jobIDs, "very_recent", "Very recent job should be present")
	require.NotContains(t, jobIDs, "old", "Old job should have been filtered out")
}
