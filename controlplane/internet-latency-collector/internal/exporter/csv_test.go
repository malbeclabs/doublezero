package exporter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_CSVExporter_New(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Create a temporary directory for testing
	tempDir := t.TempDir()

	exporter, err := exporter.NewCSVExporter(log, "test_prefix", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	// Verify the exporter is properly initialized
	require.NotEmpty(t, exporter.GetFilename())

	// Verify filename format
	expectedPrefix := "test_prefix_"
	require.Contains(t, filepath.Base(exporter.GetFilename()), expectedPrefix)
	require.True(t, strings.HasSuffix(exporter.GetFilename(), ".csv"))

	// Verify file exists
	_, err = os.Stat(exporter.GetFilename())
	require.NoError(t, err)

	// Read the file and verify header
	content, err := os.ReadFile(exporter.GetFilename())
	require.NoError(t, err)

	expectedHeader := "source_exchange_code,target_exchange_code,timestamp,latency\n"
	require.Equal(t, expectedHeader, string(content))
}

func TestInternetLatency_CSVExporter_CreateDirectory(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Create a temporary directory and a subdirectory path
	tempDir := t.TempDir()
	newDir := filepath.Join(tempDir, "subdir", "deeper")

	exporter, err := exporter.NewCSVExporter(log, "test", newDir)
	require.NoError(t, err)
	defer exporter.Close()

	// Verify directory was created
	_, err = os.Stat(newDir)
	require.NoError(t, err)
}

func TestInternetLatency_CSVExporter_InvalidDirectory(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	// Create a file where we expect a directory - this will cause os.MkdirAll to fail
	tempDir := t.TempDir()
	invalidDir := filepath.Join(tempDir, "file_not_dir")

	// Create a file at this path
	err := os.WriteFile(invalidDir, []byte("test"), 0644)
	require.NoError(t, err)

	// Now try to use this file path as a directory - should fail
	invalidPath := filepath.Join(invalidDir, "subdir")
	_, err = exporter.NewCSVExporter(log, "test", invalidPath)
	require.Error(t, err)
}

func TestInternetLatency_CSVExporter_GetFilename(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	exporter, err := exporter.NewCSVExporter(log, "test_prefix", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	filename := exporter.GetFilename()
	require.NotEmpty(t, filename)
	require.Contains(t, filename, "test_prefix")
	require.True(t, strings.HasSuffix(filename, ".csv"))
}

func TestInternetLatency_CSVExporter_WriteRecords(t *testing.T) {
	t.Parallel()

	log := logger.With("test", t.Name())

	tempDir := t.TempDir()
	e, err := exporter.NewCSVExporter(log, "test", tempDir)
	require.NoError(t, err)
	defer e.Close()

	// Test writing a valid record
	records := []exporter.Record{
		{
			SourceExchangeCode: "source1",
			TargetExchangeCode: "target1",
			Timestamp:          time.Unix(100, 0),
			RTT:                time.Duration(100),
		},
		{
			SourceExchangeCode: "source2",
			TargetExchangeCode: "target2",
			Timestamp:          time.Unix(200, 0),
			RTT:                time.Duration(200),
		},
	}

	// Test writing a record
	err = e.WriteRecords(t.Context(), records)
	require.NoError(t, err)

	// Close to flush
	e.Close()

	// Read and verify content
	content, err := os.ReadFile(e.GetFilename())
	require.NoError(t, err)

	expectedContent := "source_exchange_code,target_exchange_code,timestamp,latency\nsource1,target1," + time.Unix(100, 0).Format(time.RFC3339) + ",100ns\nsource2,target2," + time.Unix(200, 0).Format(time.RFC3339) + ",200ns\n"
	require.Equal(t, expectedContent, string(content))
}

func TestInternetLatency_CSVExporter_EscapeCSVField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"No commas", "simple text", "simple text"},
		{"With comma", "text, with comma", "\"text, with comma\""},
		{"Multiple commas", "a,b,c", "\"a,b,c\""},
		{"Empty string", "", ""},
		{"Only comma", ",", "\",\""},
		{"Already quoted", "\"quoted\"", "\"quoted\""},
		{"Complex text", "Hello, world! How are you?", "\"Hello, world! How are you?\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exporter.EscapeCSVField(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
