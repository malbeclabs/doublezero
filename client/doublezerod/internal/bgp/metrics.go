package bgp

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MetricSessionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_session_is_up",
			Help: "Status of BGP session to DoubleZero",
		},
	)
	MetricSessionStatusDesc = `
# HELP doublezero_session_is_up Status of BGP session to DoubleZero
# TYPE doublezero_session_is_up gauge
doublezero_session_is_up %d
`

	MetricSessionEstablishedDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_session_established_duration_seconds",
			Help:    "Duration of BGP session establishment to DoubleZero",
			Buckets: prometheus.ExponentialBuckets(1, 1.5, 12), // 1s to 128s
		},
		[]string{"peer_ip"},
	)

	MetricHandleUpdateDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_bgp_handle_update_duration_seconds",
			Help:    "Duration of processing a BGP update message batch",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15), // 0.1ms to ~1.6s
		},
	)
)
