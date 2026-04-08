package serviceability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	labelResult = "result"

	resultSuccess      = "success"
	resultErrorStale   = "error_stale"
	resultErrorNoCache = "error_no_cache"
)

var (
	metricFetchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_telemetry_programdata_fetch_duration_seconds",
			Help:    "Duration of serviceability program data RPC fetch calls (excludes cache hits)",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
	)

	metricFetchTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_telemetry_programdata_fetch_total",
			Help: "Total serviceability program data RPC fetch attempts by result",
		},
		[]string{labelResult},
	)

	metricStaleCacheAge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_telemetry_programdata_stale_cache_age_seconds",
			Help: "Age of stale cache data served on fetch failure (0 when cache is fresh)",
		},
	)
)
