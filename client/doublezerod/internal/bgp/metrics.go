package bgp

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SessionMetric abstracts setting the session-up metric so that
// callers (e.g. the manager) can bind labels without the BGP layer
// knowing about connection metadata.
type SessionMetric func(value float64)

var (
	MetricSessionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_session_is_up",
			Help: "Status of BGP session to DoubleZero",
		},
		[]string{
			"user_type",
			"network",
			"current_device",
			"metro",
			"tunnel_name",
			"tunnel_src",
			"tunnel_dst",
		},
	)

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
