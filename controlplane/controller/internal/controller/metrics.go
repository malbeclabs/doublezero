package controller

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "controller_build_info",
		Help: "Build information of the agent",
	},
		[]string{"version", "commit", "date"},
	)

	// gRPC metrics
	getConfigPubkeyErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_grpc_getconfig_pubkey_errors_total",
		Help: "The total number of missing pubkeys in cache",
	},
		[]string{"pubkey"},
	)

	getConfigRenderErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_grpc_getconfig_render_errors_total",
		Help: "The total number of failed config renderings",
	},
		[]string{"pubkey"},
	)

	getConfigOps = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_grpc_getconfig_requests_total",
		Help: "The total number of getconfig requests",
	},
		[]string{"pubkey"},
	)

	// cache update metrics
	cacheUpdateErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "controller_cache_update_errors_total",
		Help: "The total number of cache update errors",
	})

	cacheUpdateFetchErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "controller_cache_update_fetch_errors_total",
		Help: "The total number of cache update errors from fetching on-chain data",
	})

	cacheUpdateOps = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "controller_cache_update_ops_total",
		Help: "The total number of cache update ops",
	})

	// link metrics
	linkMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "controller_link_metrics",
		Help: "Metrics for device links",
	},
		[]string{"device_code", "interface", "device_pubkey"},
	)
	linkMetricInvalid = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_link_metrics_invalid_total",
		Help: "The total number of invalid link metrics",
	},
		[]string{"link_pubkey", "device_code", "interface"},
	)
)

func init() {
	// build info
	prometheus.MustRegister(BuildInfo)

	// gRPC metrics
	prometheus.MustRegister(getConfigPubkeyErrors)
	prometheus.MustRegister(getConfigRenderErrors)
	prometheus.MustRegister(getConfigOps)

	// cache update metrics
	prometheus.MustRegister(cacheUpdateErrors)
	prometheus.MustRegister(cacheUpdateFetchErrors)
	prometheus.MustRegister(cacheUpdateOps)
}
