package internettelemetry

import "github.com/prometheus/client_golang/prometheus"

const (
	// Metric names.
	MetricNameErrors          = "doublezero_monitor_internet_telemetry_errors_total"
	MetricNameSamples         = "doublezero_monitor_internet_telemetry_samples_total"
	MetricNameSuccesses       = "doublezero_monitor_internet_telemetry_successes_total"
	MetricNameLosses          = "doublezero_monitor_internet_telemetry_losses_total"
	MetricNameAccountNotFound = "doublezero_monitor_internet_telemetry_account_not_found_total"

	// Labels.
	MetricLabelErrorType    = "error_type"
	MetricLabelCircuit      = "circuit"
	MetricLabelDataProvider = "data_provider"

	// Error types.
	MetricErrorTypeGetCircuits       = "get_circuits"
	MetricErrorTypeGetEpochInfo      = "get_epoch_info"
	MetricErrorTypeGetLatencySamples = "get_latency_samples"
)

// Metrics groups all Prometheus collectors for Internet telemetry.
type Metrics struct {
	Errors          *prometheus.CounterVec
	Samples         *prometheus.CounterVec
	Successes       *prometheus.CounterVec
	Losses          *prometheus.CounterVec
	AccountNotFound *prometheus.CounterVec
}

// NewMetrics constructs collectors but does not register them.
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
			[]string{MetricLabelDataProvider, MetricLabelCircuit},
		),
		Successes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameSuccesses,
				Help: "Number of successes",
			},
			[]string{MetricLabelDataProvider, MetricLabelCircuit},
		),
		Losses: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameLosses,
				Help: "Number of losses",
			},
			[]string{MetricLabelDataProvider, MetricLabelCircuit},
		),
		AccountNotFound: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: MetricNameAccountNotFound,
				Help: "Number of account not found",
			},
			[]string{MetricLabelDataProvider, MetricLabelCircuit},
		),
	}
}

// Register all metrics with the provided registerer.
func (m *Metrics) Register(r prometheus.Registerer) {
	r.MustRegister(m.Errors, m.Samples, m.Successes, m.Losses, m.AccountNotFound)
}
