package controller

import (
	"github.com/prometheus/client_golang/prometheus"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
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

	duplicateTunnelPairs = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_duplicate_tunnel_pairs_total",
		Help: "The total number of duplicate tunnel pairs detected during config rendering",
	},
		[]string{"pubkey", "device_code"},
	)

	getConfigOps = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "controller_grpc_getconfig_requests_total",
		Help: "The total number of getconfig requests",
	},
		[]string{"pubkey", "device_code", "contributor_code", "exchange_code", "location_code", "device_status", "agent_version", "agent_commit", "agent_date"},
	)

	getConfigMsgSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "controller_grpc_getconfig_msg_size_bytes",
		Help:    "The size of GetConfig response messages in bytes",
		Buckets: prometheus.ExponentialBucketsRange(16384, 1048576, 8),
	})

	getConfigDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "controller_grpc_getconfig_duration_seconds",
		Help:    "The duration of GetConfig requests in seconds",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5},
	})

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

	srvMetrics = grpcprom.NewServerMetrics(
		grpcprom.WithServerHandlingTimeHistogram(
			grpcprom.WithHistogramBuckets([]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5}),
		),
	)
)

func init() {
	// build info
	prometheus.MustRegister(BuildInfo)

	// gRPC metrics
	prometheus.MustRegister(getConfigPubkeyErrors)
	prometheus.MustRegister(getConfigRenderErrors)
	prometheus.MustRegister(duplicateTunnelPairs)
	prometheus.MustRegister(getConfigOps)
	prometheus.MustRegister(getConfigMsgSize)
	prometheus.MustRegister(getConfigDuration)

	// cache update metrics
	prometheus.MustRegister(cacheUpdateErrors)
	prometheus.MustRegister(cacheUpdateFetchErrors)
	prometheus.MustRegister(cacheUpdateOps)

	// gRPC middleware metrics
	prometheus.MustRegister(srvMetrics)
}
