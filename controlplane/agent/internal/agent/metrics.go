package agent

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_agent_build_info",
			Help: "Build information of the agent",
		},
		[]string{"version", "commit", "date"},
	)
	ErrorsBgpNeighbors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_agent_bgp_neighbors_errors_total",
			Help: "Number of errors encountered while retrieving BGP neighbors from the device",
		},
	)
	ErrorsGetConfig = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_agent_get_config_errors_total",
			Help: "Number of errors encountered while getting config from the controller",
		},
	)
	ErrorsApplyConfig = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_agent_apply_config_errors_total",
			Help: "Number of errors encountered while applying config to the device",
		},
	)
)
