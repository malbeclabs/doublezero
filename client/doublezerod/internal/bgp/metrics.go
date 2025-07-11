package bgp

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MetricSessionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_session_is_up",
			Help: "Status of session to doublezero",
		},
	)
	MetricSessionStatusDesc = `
# HELP doublezero_session_is_up Status of session to doublezero
# TYPE doublezero_session_is_up gauge
doublezero_session_is_up %d
`
)
