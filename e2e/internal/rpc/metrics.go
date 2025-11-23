//go:build linux

package rpc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "doublezero_qaagent_build_info",
		Help: "Build information of the QA agent",
	},
		[]string{"version", "commit", "date"},
	)

	PingPacketsLostTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_qaagent_ping_packets_lost_total",
		Help: "Total number of packets lost during ping tests",
	},
		[]string{"source_ip", "target_ip"},
	)

	UserConnectDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "doublezero_qaagent_user_connect_duration_seconds",
		Help:    "Duration of connect operations",
		Buckets: prometheus.ExponentialBuckets(1, 1.5, 12), // 1s to 128s
	}, []string{"user_type"})

	UserDisconnectDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "doublezero_qaagent_user_disconnect_duration_seconds",
		Help:    "Duration of disconnect operations",
		Buckets: prometheus.ExponentialBuckets(1, 1.5, 12), // 1s to 128s
	})
)
