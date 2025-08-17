package devicetelemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameErrors    = "doublezero_monitor_device_telemetry_errors_total"
	MetricNameSamples   = "doublezero_monitor_device_telemetry_samples_total"
	MetricNameSuccesses = "doublezero_monitor_device_telemetry_successes_total"
	MetricNameLosses    = "doublezero_monitor_device_telemetry_losses_total"

	// Labels.
	MetricLabelErrorType = "error_type"
	MetricLabelCircuit   = "circuit"

	// Error types.
	MetricErrorTypeGetCircuits       = "get_circuits"
	MetricErrorTypeGetEpochInfo      = "get_epoch_info"
	MetricErrorTypeGetLatencySamples = "get_latency_samples"
)

var (
	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType},
	)

	MetricSamples = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameSamples,
			Help: "Number of samples",
		},
		[]string{MetricLabelCircuit},
	)

	MetricSuccesses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameSuccesses,
			Help: "Number of successes",
		},
		[]string{MetricLabelCircuit},
	)

	MetricLosses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameLosses,
			Help: "Number of losses",
		},
		[]string{MetricLabelCircuit},
	)
)
