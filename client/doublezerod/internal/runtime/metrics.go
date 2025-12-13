package runtime

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Labels.
	LabelRouteSrc     = "src"
	LabelRouteNextHop = "next_hop"
)

var (
	metricBGPRoutesInstalled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_bgp_routes_installed",
			Help: "Number of BGP routes installed",
		},
		[]string{LabelRouteSrc, LabelRouteNextHop},
	)
)
