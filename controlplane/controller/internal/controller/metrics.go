package controller

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
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
)

func init() {
	// gRPC metrics
	prometheus.MustRegister(getConfigPubkeyErrors)
	prometheus.MustRegister(getConfigRenderErrors)
	prometheus.MustRegister(getConfigOps)

	// cache update metrics
	prometheus.MustRegister(cacheUpdateErrors)
	prometheus.MustRegister(cacheUpdateFetchErrors)
	prometheus.MustRegister(cacheUpdateOps)
}
