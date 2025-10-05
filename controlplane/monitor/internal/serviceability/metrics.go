package serviceability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameErrors                  = "doublezero_monitor_serviceability_errors_total"
	MetricNameProgramBuildInfo        = "doublezero_monitor_serviceability_program_build_info"
	MetricNameUnlinkedInterfaceErrors = "doublezero_monitor_unlinked_interface_errors_total"
	MetricNameUserPendingDuration     = "doublezero_monitor_user_pending_duration_seconds"
	MetricNameUserDeletingDuration    = "doublezero_monitor_user_deleting_duration_seconds"

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

	MetricUnlinkedInterfaceErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameUnlinkedInterfaceErrors,
			Help: "Onchain error when a device interface is unlinked but participating in an activated link",
		},
		[]string{"device_pubkey", "device_code", "interface_name", "link_pubkey"},
	)

	MetricUserPendingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: MetricNameUserPendingDuration,
			Help: "The duration of a user being in a pending state",
		},
		[]string{"user_pubkey"},
	)

	MetricUserDeletingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    MetricNameUserDeletingDuration,
			Help:    "The duration of a user being in a deleting state",
			Buckets: prometheus.LinearBuckets(0, 30, 10), // 0-300 seconds
		},
		[]string{"user_pubkey"},
	)
)
