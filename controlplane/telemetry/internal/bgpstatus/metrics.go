package bgpstatus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricSubmissionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_bgpstatus_submissions_total",
			Help: "Total onchain BGP status submissions by BGP status and result",
		},
		[]string{"bgp_status", "result"},
	)

	metricSubmissionDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_bgpstatus_submission_duration_seconds",
			Help:    "Duration of successful onchain BGP status submissions",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
	)
)
