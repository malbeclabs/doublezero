package devicetelemetry

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// Metric names.
	MetricNameErrors          = "doublezero_monitor_device_telemetry_errors_total"
	MetricNameSamples         = "doublezero_monitor_device_telemetry_samples_total"
	MetricNameSuccesses       = "doublezero_monitor_device_telemetry_successes_total"
	MetricNameLosses          = "doublezero_monitor_device_telemetry_losses_total"
	MetricNameAccountNotFound = "doublezero_monitor_device_telemetry_account_not_found_total"

	// Labels.
	MetricLabelErrorType = "error_type"
	MetricLabelCircuit   = "circuit"

	// Error types.
	MetricErrorTypeGetCircuits       = "get_circuits"
	MetricErrorTypeGetEpochInfo      = "get_epoch_info"
	MetricErrorTypeGetLatencySamples = "get_latency_samples"
)

type Metrics struct {
	Errors          *prometheus.CounterVec
	Samples         *prometheus.CounterVec
	Successes       *prometheus.CounterVec
	Losses          *prometheus.CounterVec
	AccountNotFound *prometheus.CounterVec
}

// NewMetrics creates the collectors but does not auto-register them.
func NewMetrics() *Metrics {
	return &Metrics{
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameErrors,
				Help: "Number of errors encountered",
			},
			[]string{MetricLabelErrorType},
		),
		Samples: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameSamples,
				Help: "Number of samples",
			},
			[]string{MetricLabelCircuit},
		),
		Successes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameSuccesses,
				Help: "Number of successes",
			},
			[]string{MetricLabelCircuit},
		),
		Losses: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameLosses,
				Help: "Number of losses",
			},
			[]string{MetricLabelCircuit},
		),
		AccountNotFound: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameAccountNotFound,
				Help: "Number of account not found",
			},
			[]string{MetricLabelCircuit},
		),
	}
}

// Register all metrics with the provided registry.
func (m *Metrics) Register(r prometheus.Registerer) {
	r.MustRegister(m.Errors, m.Samples, m.Successes, m.Losses, m.AccountNotFound)
}
