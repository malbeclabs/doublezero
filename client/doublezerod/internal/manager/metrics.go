package manager

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	labelStatus      = "status"
	labelServiceType = "service_type"

	statusSuccess = "success"
	statusError   = "error"

	serviceUnicast   = "unicast"
	serviceMulticast = "multicast"
)

var (
	metricPollsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_reconciler_polls_total",
			Help: "Total number of reconciler poll cycles",
		},
		[]string{labelStatus},
	)

	metricProvisionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_reconciler_provisions_total",
			Help: "Total number of service provision attempts",
		},
		[]string{labelServiceType, labelStatus},
	)

	metricRemovalsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_reconciler_removals_total",
			Help: "Total number of service removal attempts",
		},
		[]string{labelServiceType, labelStatus},
	)

	metricUpdatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_reconciler_updates_total",
			Help: "Total number of incremental group update attempts",
		},
		[]string{labelServiceType, labelStatus},
	)

	metricMatchedUsers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_reconciler_matched_users",
			Help: "Number of activated users matching this client IP",
		},
		[]string{labelServiceType},
	)
)
