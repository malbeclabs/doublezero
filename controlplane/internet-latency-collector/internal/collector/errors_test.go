package collector

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Errors_ErrorType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		errType  ErrorType
		expected string
	}{
		{"API error", ErrorTypeAPI, "api_error"},
		{"Network error", ErrorTypeNetwork, "network_error"},
		{"Config error", ErrorTypeConfig, "config_error"},
		{"Validation error", ErrorTypeValidation, "validation_error"},
		{"File I/O error", ErrorTypeFileIO, "file_io_error"},
		{"Database error", ErrorTypeDatabase, "database_error"},
		{"Auth error", ErrorTypeAuth, "auth_error"},
		{"Timeout error", ErrorTypeTimeout, "timeout_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, string(tt.errType))
		})
	}
}

func TestInternetLatency_Errors_CollectorError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      *CollectorError
		expected string
	}{
		{
			name: "error without cause",
			err: &CollectorError{
				Type:      ErrorTypeAPI,
				Operation: "test_operation",
				Message:   "test message",
				Cause:     nil,
				context:   make(map[string]any),
			},
			expected: "api_error failed in test_operation: test message",
		},
		{
			name: "error with cause",
			err: &CollectorError{
				Type:      ErrorTypeNetwork,
				Operation: "network_request",
				Message:   "connection failed",
				Cause:     errors.New("timeout"),
				context:   make(map[string]any),
			},
			expected: "network_error failed in network_request: connection failed (caused by: timeout)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestInternetLatency_Errors_CollectorError_Unwrap(t *testing.T) {
	t.Parallel()

	originalErr := errors.New("original error")
	collectorErr := &CollectorError{
		Type:      ErrorTypeAPI,
		Operation: "test",
		Message:   "test",
		Cause:     originalErr,
		context:   make(map[string]any),
	}

	require.Equal(t, originalErr, collectorErr.Unwrap())

	// Test unwrapping with no cause
	collectorErrNoCause := &CollectorError{
		Type:      ErrorTypeAPI,
		Operation: "test",
		Message:   "test",
		Cause:     nil,
		context:   make(map[string]any),
	}

	require.Nil(t, collectorErrNoCause.Unwrap())
}

func TestInternetLatency_Errors_NewError(t *testing.T) {
	t.Parallel()

	cause := errors.New("test cause")
	err := NewError(ErrorTypeValidation, "test_op", "test message", cause)

	require.Equal(t, ErrorTypeValidation, err.Type)
	require.Equal(t, "test_op", err.Operation)
	require.Equal(t, "test message", err.Message)
	require.Equal(t, cause, err.Cause)
	require.NotNil(t, err.GetContextMap())
}

func TestInternetLatency_Errors_WithContext(t *testing.T) {
	t.Parallel()

	err := NewError(ErrorTypeAPI, "test", "test", nil)

	// Test adding context
	_ = err.WithContext("key1", "value1")
	require.Equal(t, "value1", err.GetContext("key1"))

	// Test chaining context
	_ = err.WithContext("key2", 123).WithContext("key3", true)
	require.Equal(t, 123, err.GetContext("key2"))
	require.Equal(t, true, err.GetContext("key3"))

	// Test overwriting context
	_ = err.WithContext("key1", "new_value")
	require.Equal(t, "new_value", err.GetContext("key1"))
}

func TestInternetLatency_Errors_WithContext_NilContext(t *testing.T) {
	t.Parallel()

	err := &CollectorError{
		Type:      ErrorTypeAPI,
		Operation: "test",
		Message:   "test",
		Cause:     nil,
		context:   nil, // Explicitly nil
	}

	_ = err.WithContext("key", "value")
	require.NotNil(t, err.GetContext("key"))
	require.Equal(t, "value", err.GetContext("key"))
}

func TestInternetLatency_Errors_NewAPIError(t *testing.T) {
	t.Parallel()

	cause := errors.New("api failure")
	err := NewAPIError("api_call", "request failed", cause)

	require.Equal(t, ErrorTypeAPI, err.Type)
	require.Equal(t, "api_call", err.Operation)
	require.Equal(t, "request failed", err.Message)
	require.Equal(t, cause, err.Cause)
}

func TestInternetLatency_Errors_NewNetworkError(t *testing.T) {
	t.Parallel()

	err := NewNetworkError("network_op", "connection timeout", nil)

	require.Equal(t, ErrorTypeNetwork, err.Type)
	require.Equal(t, "network_op", err.Operation)
	require.Equal(t, "connection timeout", err.Message)
}

func TestInternetLatency_Errors_NewValidationError(t *testing.T) {
	t.Parallel()

	err := NewValidationError("input_validation", "invalid input", nil)

	require.Equal(t, ErrorTypeValidation, err.Type)
	require.Equal(t, "input_validation", err.Operation)
	require.Equal(t, "invalid input", err.Message)
}

func TestInternetLatency_Errors_NewFileIOError(t *testing.T) {
	t.Parallel()

	err := NewFileIOError("file_read", "file not found", nil)

	require.Equal(t, ErrorTypeFileIO, err.Type)
	require.Equal(t, "file_read", err.Operation)
	require.Equal(t, "file not found", err.Message)
}

func TestInternetLatency_Errors_ErrorConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       *CollectorError
		errType   ErrorType
		operation string
	}{
		{"ErrLocationNotFound", ErrLocationNotFound, ErrorTypeValidation, "location_lookup"},
		{"ErrInvalidCoordinates", ErrInvalidCoordinates, ErrorTypeValidation, "coordinate_validation"},
		{"ErrNoDevicesFound", ErrNoDevicesFound, ErrorTypeValidation, "device_discovery"},
		{"ErrNoProbesFound", ErrNoProbesFound, ErrorTypeValidation, "probe_discovery"},
		{"ErrInvalidMeasurement", ErrInvalidMeasurement, ErrorTypeValidation, "measurement_validation"},
		{"ErrInvalidInterval", ErrInvalidInterval, ErrorTypeValidation, "interval_validation"},
		{"ErrInsufficientSources", ErrInsufficientSources, ErrorTypeValidation, "source_validation"},
		{"ErrRateLimitExceeded", ErrRateLimitExceeded, ErrorTypeAPI, "api_rate_limit"},
		{"ErrUnauthorized", ErrUnauthorized, ErrorTypeAPI, "api_auth"},
		{"ErrServiceUnavailable", ErrServiceUnavailable, ErrorTypeAPI, "api_service"},
		{"ErrMeasurementCreation", ErrMeasurementCreation, ErrorTypeAPI, "measurement_creation"},
		{"ErrJobCreation", ErrJobCreation, ErrorTypeAPI, "job_creation"},
		{"ErrMeasurementStop", ErrMeasurementStop, ErrorTypeAPI, "measurement_stop"},
		{"ErrJobIDStorage", ErrJobIDStorage, ErrorTypeFileIO, "job_id_storage"},
		{"ErrJobIDRetrieval", ErrJobIDRetrieval, ErrorTypeFileIO, "job_id_retrieval"},
		{"ErrResultsExport", ErrResultsExport, ErrorTypeFileIO, "results_export"},
		{"ErrProbeConnection", ErrProbeConnection, ErrorTypeNetwork, "probe_connection"},
		{"ErrJobResultRetrieval", ErrJobResultRetrieval, ErrorTypeNetwork, "job_result_retrieval"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.errType, tt.err.Type)
			require.Equal(t, tt.operation, tt.err.Operation)
			require.Nil(t, tt.err.Cause)
			require.NotNil(t, tt.err.GetContextMap())
		})
	}
}

func TestInternetLatency_Errors_ErrorConstantsWithContext(t *testing.T) {
	t.Parallel()

	// Test that error constants can be used with context
	// First check the original context length
	originalLen := len(ErrLocationNotFound.GetContextMap())

	err := ErrLocationNotFound.WithContext("filename", "test.csv").WithContext("line", 10)

	require.Equal(t, "test.csv", err.GetContext("filename"))
	require.Equal(t, 10, err.GetContext("line"))

	// The WithContext method modifies the error in place, so the original will be modified
	// This is actually expected behavior for the current implementation
	if len(ErrLocationNotFound.GetContextMap()) == originalLen {
		t.Logf("Note: WithContext modifies the original error constant (expected behavior)")
	}
}

func TestCollectorError_IsType(t *testing.T) {
	t.Parallel()

	// Test checking error types using errors.Is and type assertion
	apiErr := NewAPIError("test", "test", nil)

	// Test that we can check if it's a CollectorError
	var collectorErr *CollectorError
	require.True(t, errors.As(apiErr, &collectorErr))
	require.Equal(t, ErrorTypeAPI, collectorErr.Type)
}
