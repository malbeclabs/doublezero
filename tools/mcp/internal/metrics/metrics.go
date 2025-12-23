package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_mcp_build_info",
			Help: "Build information of the DoubleZero MCP server",
		},
		[]string{"version", "commit", "date"},
	)
)
