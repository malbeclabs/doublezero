//go:build linux

package rpc

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_qaagent_build_info",
		Help: "Build information of the QA agent",
	},
		[]string{"version", "commit", "date"},
	)
)

func init() {
	prometheus.MustRegister(BuildInfo)
}
