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

	PingPacketsLostTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_qaagent_ping_packets_lost_total",
		Help: "Total number of packets lost during ping tests",
	},
		[]string{"source_ip", "target_ip"},
	)

	ConnectUnicastDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "doublezero_qaagent_connect_unicast_duration_seconds",
		Help: "Duration of unicast connect tests",
	})

	ConnectMulticastDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "doublezero_qaagent_connect_multicast_duration_seconds",
		Help: "Duration of multicast connect tests",
	})

	DisconnectDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "doublezero_qaagent_disconnect_duration_seconds",
		Help: "Duration of disconnect tests",
	})
)

func init() {
	prometheus.MustRegister(BuildInfo)
	prometheus.MustRegister(PingPacketsLostTotal)
}
