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
)
