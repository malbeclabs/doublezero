package collector

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		name     string
		level    LogLevel
		expected string
	}{
		{"Debug level", LogLevelDebug, "debug"},
		{"Info level", LogLevelInfo, "info"},
		{"Warn level", LogLevelWarn, "warn"},
		{"Error level", LogLevelError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, string(tt.level))
		})
	}
}

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name          string
		level         LogLevel
		expectedLevel slog.Level
		expectDebug   bool
	}{
		{"Debug level", LogLevelDebug, slog.LevelDebug, true},
		{"Info level", LogLevelInfo, slog.LevelInfo, false},
		{"Warn level", LogLevelWarn, slog.LevelWarn, false},
		{"Error level", LogLevelError, slog.LevelError, false},
		{"Invalid level defaults to info", LogLevel("invalid"), slog.LevelInfo, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLogger := Logger

			// Initialize logger with test level
			InitLogger(tt.level)

			// Verify logger is not nil
			require.NotNil(t, Logger, "InitLogger() should set Logger to non-nil value")

			// Verify the default logger is set
			require.Equal(t, Logger, slog.Default(), "InitLogger() should set slog default logger")

			// Restore original logger
			Logger = originalLogger
		})
	}
}

func TestLogError(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Create a test error with context
	err := NewAPIError("test_operation", "test error message", nil).
		WithContext("key1", "value1").
		WithContext("key2", 42)

	// Log the error
	LogError(err, "Test error occurred")

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "ERROR", logEntry["level"])
	require.Equal(t, "Test error occurred", logEntry["msg"])
	require.Equal(t, "api_error", logEntry["error_type"])
	require.Equal(t, "test_operation", logEntry["operation"])
	require.Equal(t, "test error message", logEntry["error_message"])
	require.Equal(t, "value1", logEntry["key1"])
	require.Equal(t, float64(42), logEntry["key2"]) // JSON numbers are float64
}

func TestLogError_WithCause(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Create a test error with a cause
	causeErr := NewNetworkError("network_call", "connection failed", nil)
	err := NewAPIError("api_operation", "api call failed", causeErr)

	// Log the error
	LogError(err, "API operation failed")

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify cause is logged
	require.Contains(t, logEntry["cause"].(string), "network_error failed in network_call: connection failed", "Log cause should contain network error details")
}

func TestLogWarning(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Log a warning with attributes
	LogWarning("Test warning",
		slog.String("component", "test"),
		slog.Int("count", 5))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "WARN", logEntry["level"])
	require.Equal(t, "Test warning", logEntry["msg"])
	require.Equal(t, "test", logEntry["component"])
	require.Equal(t, float64(5), logEntry["count"])
}

func TestLogInfo(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Log an info message
	LogInfo("Test info message", slog.String("status", "success"))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "INFO", logEntry["level"])
	require.Equal(t, "Test info message", logEntry["msg"])
	require.Equal(t, "success", logEntry["status"])
}

func TestLogDebug(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Log a debug message
	LogDebug("Debug information", slog.Any("data", map[string]int{"items": 10}))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "DEBUG", logEntry["level"])
	require.Equal(t, "Debug information", logEntry["msg"])

	// Verify complex data structure
	data, ok := logEntry["data"].(map[string]any)
	require.True(t, ok, "Log data should be a map")
	require.Equal(t, float64(10), data["items"])
}

func TestLogOperationStart(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Log operation start
	LogOperationStart("test_operation", slog.String("param", "value"))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "INFO", logEntry["level"])
	require.Equal(t, "Operation started", logEntry["msg"])
	require.Equal(t, "test_operation", logEntry["operation"])
	require.Equal(t, "value", logEntry["param"])
}

func TestLogOperationComplete(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Log operation complete
	LogOperationComplete("test_operation", slog.Int("result_count", 42))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "INFO", logEntry["level"])
	require.Equal(t, "Operation completed", logEntry["msg"])
	require.Equal(t, "test_operation", logEntry["operation"])
	require.Equal(t, float64(42), logEntry["result_count"])
}

func TestLogOperationFailed(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// Create a test error
	testErr := NewAPIError("api_call", "request failed", nil)

	// Log operation failed
	LogOperationFailed("test_operation", testErr, slog.String("context", "test"))

	// Parse the JSON log output
	var logEntry map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, jsonErr, "Failed to parse log JSON")

	// Verify log entry fields
	require.Equal(t, "ERROR", logEntry["level"])
	require.Equal(t, "Operation failed", logEntry["msg"])
	require.Equal(t, "test_operation", logEntry["operation"])
	require.Contains(t, logEntry["error"].(string), "api_error failed in api_call: request failed", "Log error should contain error details")
	require.Equal(t, "test", logEntry["context"])
}

func TestLoggingLevels(t *testing.T) {
	// Test that different log levels are respected
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	testLogger := slog.New(handler)
	originalLogger := Logger
	Logger = testLogger

	defer func() {
		Logger = originalLogger
	}()

	// These should not appear in output (below WARN level)
	LogDebug("Debug message")
	LogInfo("Info message")

	// These should appear in output (WARN level and above)
	LogWarning("Warning message")
	LogError(NewAPIError("test", "test", nil), "Error message")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should only have 2 lines (warning and error)
	require.Len(t, lines, 2, "Expected 2 log entries")

	// Verify warning and error are present
	require.Contains(t, output, "Warning message", "Warning message should be logged")
	require.Contains(t, output, "Error message", "Error message should be logged")

	// Verify debug and info are not present
	require.NotContains(t, output, "Debug message", "Debug message should not be logged at WARN level")
	require.NotContains(t, output, "Info message", "Info message should not be logged at WARN level")
}
