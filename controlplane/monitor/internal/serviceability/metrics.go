package serviceability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameErrors           = "doublezero_monitor_serviceability_errors_total"
	MetricNameProgramBuildInfo = "doublezero_monitor_serviceability_program_build_info"

	// Labels.
	MetricLabelErrorType      = "error_type"
	MetricLabelProgramVersion = "program_version"

	// Error types.
	MetricErrorTypeGetProgramData = "get_program_data"
)

var (
	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType},
	)

	MetricProgramBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameProgramBuildInfo,
			Help: "Program build info",
		},
		[]string{MetricLabelProgramVersion},
	)
)
