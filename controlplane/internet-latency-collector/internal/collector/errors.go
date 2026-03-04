package collector

import (
	"fmt"
	"maps"
	"sync"
)

type ErrorType string

const (
	ErrorTypeAPI        ErrorType = "api_error"
	ErrorTypeNetwork    ErrorType = "network_error"
	ErrorTypeConfig     ErrorType = "config_error"
	ErrorTypeValidation ErrorType = "validation_error"
	ErrorTypeFileIO     ErrorType = "file_io_error"
	ErrorTypeDatabase   ErrorType = "database_error"
	ErrorTypeAuth       ErrorType = "auth_error"
	ErrorTypeTimeout    ErrorType = "timeout_error"
)

type CollectorError struct {
	Type      ErrorType
	Operation string
	Message   string
	Cause     error

	context   map[string]any
	contextMu sync.RWMutex
}

func (e *CollectorError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s failed in %s: %s (caused by: %v)", e.Type, e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s failed in %s: %s", e.Type, e.Operation, e.Message)
}

func (e *CollectorError) Unwrap() error {
	return e.Cause
}

func NewError(errType ErrorType, operation, message string, cause error) *CollectorError {
	return &CollectorError{
		Type:      errType,
		Operation: operation,
		Message:   message,
		Cause:     cause,
		context:   make(map[string]any),
	}
}

func (e *CollectorError) GetContextMap() map[string]any {
	e.contextMu.RLock()
	defer e.contextMu.RUnlock()

	return maps.Clone(e.context)
}

func (e *CollectorError) GetContext(key string) any {
	e.contextMu.RLock()
	defer e.contextMu.RUnlock()

	return e.context[key]
}

func (e *CollectorError) WithContext(key string, value any) *CollectorError {
	e.contextMu.RLock()
	cloned := maps.Clone(e.context)
	e.contextMu.RUnlock()

	if cloned == nil {
		cloned = make(map[string]any)
	}
	cloned[key] = value
	return &CollectorError{
		Type:      e.Type,
		Operation: e.Operation,
		Message:   e.Message,
		Cause:     e.Cause,
		context:   cloned,
	}
}

func NewAPIError(operation, message string, cause error) *CollectorError {
	return NewError(ErrorTypeAPI, operation, message, cause)
}

func NewNetworkError(operation, message string, cause error) *CollectorError {
	return NewError(ErrorTypeNetwork, operation, message, cause)
}

func NewValidationError(operation, message string, cause error) *CollectorError {
	return NewError(ErrorTypeValidation, operation, message, cause)
}

var (
	ErrLocationNotFound    = NewValidationError("location_lookup", "location not found", nil)
	ErrInvalidCoordinates  = NewValidationError("coordinate_validation", "invalid coordinates provided", nil)
	ErrNoDevicesFound      = NewValidationError("device_discovery", "no devices found", nil)
	ErrNoProbesFound       = NewValidationError("probe_discovery", "no probes found", nil)
	ErrInvalidMeasurement  = NewValidationError("measurement_validation", "invalid measurement data", nil)
	ErrInvalidInterval     = NewValidationError("interval_validation", "invalid interval configuration", nil)
	ErrInsufficientSources = NewValidationError("source_validation", "insufficient sources for operation", nil)

	ErrRateLimitExceeded   = NewAPIError("api_rate_limit", "rate limit exceeded", nil)
	ErrUnauthorized        = NewAPIError("api_auth", "unauthorized access", nil)
	ErrServiceUnavailable  = NewAPIError("api_service", "service unavailable", nil)
	ErrMeasurementCreation = NewAPIError("measurement_creation", "failed to create measurement", nil)
	ErrJobCreation         = NewAPIError("job_creation", "failed to create job", nil)
	ErrMeasurementStop     = NewAPIError("measurement_stop", "failed to stop measurement", nil)

	ErrProbeConnection    = NewNetworkError("probe_connection", "failed to connect to probe", nil)
	ErrJobResultRetrieval = NewNetworkError("job_result_retrieval", "failed to retrieve job results", nil)
)
