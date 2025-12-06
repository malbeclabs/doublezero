package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameBuildInfo = "doublezero_global_monitor_build_info"

	// Labels.
	LabelVersion = "version"
	LabelCommit  = "commit"
	LabelDate    = "date"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBuildInfo,
			Help: "Build information of the global monitor",
		},
		[]string{LabelVersion, LabelCommit, LabelDate},
	)
)
