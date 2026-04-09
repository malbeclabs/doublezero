package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MetricNameBuildInfo        = "doublezero_device_health_oracle_build_info"
	MetricNameErrors           = "doublezero_device_health_oracle_errors_total"
	MetricNameCriterionResults = "doublezero_device_health_oracle_criterion_results_total"
	MetricNameUpdatesSkipped   = "doublezero_device_health_oracle_updates_skipped_total"

	MetricLabelVersion   = "version"
	MetricLabelCommit    = "commit"
	MetricLabelDate      = "date"
	MetricLabelErrorType = "error_type"
	MetricLabelCriterion = "criterion"
	MetricLabelResult    = "result"
	MetricLabelKind      = "kind"
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

	MetricCriterionResults = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameCriterionResults,
			Help: "Results of health criterion evaluations",
		},
		[]string{MetricLabelCriterion, MetricLabelResult},
	)

	MetricUpdatesSkipped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameUpdatesSkipped,
			Help: "Number of health updates skipped because the value was already set",
		},
		[]string{MetricLabelKind},
	)
)
