package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MetricNameBuildInfo = "doublezero_device_health_oracle_build_info"
	MetricNameErrors    = "doublezero_device_health_oracle_errors_total"

	MetricLabelVersion   = "version"
	MetricLabelCommit    = "commit"
	MetricLabelDate      = "date"
	MetricLabelErrorType = "error_type"
)

var (
	MetricBuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBuildInfo,
			Help: "Build information of the device health oracle",
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
