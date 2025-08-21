package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameBuildInfo = "doublezero_monitor_build_info"
	MetricNameErrors    = "doublezero_monitor_errors_total"

	// Labels.
	MetricLabelVersion   = "version"
	MetricLabelCommit    = "commit"
	MetricLabelDate      = "date"
	MetricLabelErrorType = "error_type"

	// Error types.
)

var (
	MetricBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBuildInfo,
			Help: "Build information of the monitor agent",
		},
		[]string{MetricLabelVersion, MetricLabelCommit, MetricLabelDate},
	)

	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType},
	)
)
