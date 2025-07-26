package wheresitup

import (
	"os"
	"path/filepath"
	"testing"

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
	require.Empty(t, jt.JobIDs, "JobIDs should be empty initially")

	// Test saving
	jt.JobIDs = []string{"job1", "job2", "job3"}
	err = jt.Save()
	require.NoError(t, err, "Save() should not error")

	// Test loading existing file
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")
	require.Len(t, jt2.JobIDs, 3, "Expected 3 job IDs")
	require.Equal(t, []string{"job1", "job2", "job3"}, jt2.JobIDs, "JobIDs should match expected values")
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
	require.Len(t, jt2.JobIDs, 4, "Expected 4 job IDs")
}

func TestInternetLatency_Wheresitup_State_RemoveJobIDs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)
	jt.JobIDs = []string{"job1", "job2", "job3", "job4"}
	err := jt.Save()
	require.NoError(t, err, "Save() should not error")

	// Remove some jobs
	err = jt.RemoveJobIDs([]string{"job2", "job4"})
	require.NoError(t, err, "RemoveJobIDs() should not error")

	// Verify only job1 and job3 remain
	jt2 := NewState(filename)
	err = jt2.Load()
	require.NoError(t, err, "Load() should not error")
	require.Len(t, jt2.JobIDs, 2, "Expected 2 job IDs")
	require.Equal(t, []string{"job1", "job3"}, jt2.JobIDs, "Expected [job1, job3]")
}

func TestInternetLatency_Wheresitup_State_InvalidFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Test non-JSON file
	filename := filepath.Join(tempDir, "test.txt")
	jt := NewState(filename)
	err := jt.Save()
	require.Error(t, err, "Expected error for non-JSON file")

	// Test corrupted JSON
	jsonFile := filepath.Join(tempDir, "corrupted.json")
	err = os.WriteFile(jsonFile, []byte("{invalid json"), 0644)
	require.NoError(t, err, "Failed to write corrupted JSON file")
	jt2 := NewState(jsonFile)
	err = jt2.Load()
	require.Error(t, err, "Expected error for corrupted JSON")
}

func TestInternetLatency_Wheresitup_State_GetJobIDs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_jobs.json")

	jt := NewState(filename)
	jt.JobIDs = []string{"job1", "job2"}

	jobIDs := jt.GetJobIDs()
	require.Len(t, jobIDs, 2, "GetJobIDs() should return 2 items")
}
