package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCSVExporter(t *testing.T) {

	// Create a temporary directory for testing
	tempDir := t.TempDir()

	exporter, err := NewCSVExporter("test_prefix", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	// Verify the exporter is properly initialized
	require.NotNil(t, exporter.file)
	require.NotNil(t, exporter.writer)
	require.NotEmpty(t, exporter.filename)

	// Verify filename format
	expectedPrefix := "test_prefix_"
	require.Contains(t, filepath.Base(exporter.filename), expectedPrefix)
	require.True(t, strings.HasSuffix(exporter.filename, ".csv"))

	// Verify file exists
	_, err = os.Stat(exporter.filename)
	require.NoError(t, err)
}

func TestNewCSVExporter_CreateDirectory(t *testing.T) {

	// Create a temporary directory and a subdirectory path
	tempDir := t.TempDir()
	newDir := filepath.Join(tempDir, "subdir", "deeper")

	exporter, err := NewCSVExporter("test", newDir)
	require.NoError(t, err)
	defer exporter.Close()

	// Verify directory was created
	_, err = os.Stat(newDir)
	require.NoError(t, err)
}

func TestNewCSVExporter_InvalidDirectory(t *testing.T) {

	// Create a file where we expect a directory - this will cause os.MkdirAll to fail
	tempDir := t.TempDir()
	invalidDir := filepath.Join(tempDir, "file_not_dir")

	// Create a file at this path
	err := os.WriteFile(invalidDir, []byte("test"), 0644)
	require.NoError(t, err)

	// Now try to use this file path as a directory - should fail
	invalidPath := filepath.Join(invalidDir, "subdir")
	_, err = NewCSVExporter("test", invalidPath)
	require.Error(t, err)

	// Verify it's a CollectorError
	var collectorErr *CollectorError
	require.True(t, isCollectorError(err, &collectorErr))
	require.Equal(t, ErrorTypeFileIO, collectorErr.Type)
	require.Equal(t, "create_output_directory", collectorErr.Operation)
}

func TestCSVExporter_WriteHeader(t *testing.T) {

	tempDir := t.TempDir()
	exporter, err := NewCSVExporter("test", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	header := []string{"column1", "column2", "column3"}
	err = exporter.WriteHeader(header)
	require.NoError(t, err)

	// Flush and close to ensure data is written
	exporter.Close()

	// Read the file and verify header
	content, err := os.ReadFile(exporter.filename)
	require.NoError(t, err)

	expectedHeader := "column1,column2,column3\n"
	require.Equal(t, expectedHeader, string(content))
}

func TestCSVExporter_GetFilename(t *testing.T) {

	tempDir := t.TempDir()
	exporter, err := NewCSVExporter("test_prefix", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	filename := exporter.GetFilename()
	require.NotEmpty(t, filename)
	require.Contains(t, filename, "test_prefix")
	require.True(t, strings.HasSuffix(filename, ".csv"))
}

func TestCSVExporter_WriteRecordWithWarning(t *testing.T) {

	tempDir := t.TempDir()
	exporter, err := NewCSVExporter("test", tempDir)
	require.NoError(t, err)
	defer exporter.Close()

	// Write header first
	header := []string{"col1", "col2", "col3"}
	err = exporter.WriteHeader(header)
	require.NoError(t, err)

	// Test writing a valid record
	record := []string{"value1", "value2", "value3"}
	exporter.WriteRecordWithWarning(record)

	// Test writing a record with commas (should be escaped)
	recordWithComma := []string{"value,with,comma", "normal", "value3"}
	exporter.WriteRecordWithWarning(recordWithComma)

	// Close to flush
	exporter.Close()

	// Read and verify content
	content, err := os.ReadFile(exporter.filename)
	require.NoError(t, err)

	expectedContent := "col1,col2,col3\nvalue1,value2,value3\n\"value,with,comma\",normal,value3\n"
	require.Equal(t, expectedContent, string(content))
}

func TestEscapeCSVField(t *testing.T) {
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
			result := EscapeCSVField(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

// Helper function to check if an error is a CollectorError
func isCollectorError(err error, collectorErr **CollectorError) bool {
	if ce, ok := err.(*CollectorError); ok {
		*collectorErr = ce
		return true
	}
	return false
}
