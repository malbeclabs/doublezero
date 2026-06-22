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

	// getConfigUnknownPubkey counts GetConfig requests from pubkeys that are not
	// present in the ledger cache (e.g. a device removed from the on-chain ledger
	// that is still calling in). It deliberately carries no per-pubkey label so a
	// flood of distinct removed pubkeys cannot reintroduce unbounded cardinality.
	getConfigUnknownPubkey = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "controller_grpc_getconfig_unknown_pubkey_total",
		Help: "The total number of GetConfig requests from pubkeys not present in the ledger cache",
	})

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
	prometheus.MustRegister(getConfigRenderErrors)
	prometheus.MustRegister(duplicateTunnelPairs)
	prometheus.MustRegister(getConfigOps)
	prometheus.MustRegister(getConfigUnknownPubkey)
	prometheus.MustRegister(getConfigMsgSize)
	prometheus.MustRegister(getConfigDuration)

	// cache update metrics
	prometheus.MustRegister(cacheUpdateErrors)
	prometheus.MustRegister(cacheUpdateFetchErrors)
	prometheus.MustRegister(cacheUpdateOps)

	// link metrics
	prometheus.MustRegister(linkMetrics)
	prometheus.MustRegister(linkMetricInvalid)

	// gRPC middleware metrics
	prometheus.MustRegister(srvMetrics)
}

// deleteDeviceMetrics drops every per-device series carrying the given device
// pubkey (and code) from the metric vectors. It is called when a device is
// removed from the on-chain ledger so Prometheus can no longer scrape its
// now-frozen counters; after a scrape interval plus the staleness window the
// series go stale and queries return empty. DeletePartialMatch removes all
// series matching just the given label(s), regardless of the other
// (agent_version, etc.) label values.
//
// linkMetricInvalid is keyed by link_pubkey rather than device_pubkey, so it is
// pruned by device_code instead (device codes are unique per device).
func deleteDeviceMetrics(pubkey, code string) {
	byPubkey := prometheus.Labels{"pubkey": pubkey}
	getConfigOps.DeletePartialMatch(byPubkey)
	getConfigRenderErrors.DeletePartialMatch(byPubkey)
	duplicateTunnelPairs.DeletePartialMatch(byPubkey)
	linkMetrics.DeletePartialMatch(prometheus.Labels{"device_pubkey": pubkey})
	linkMetricInvalid.DeletePartialMatch(prometheus.Labels{"device_code": code})
}

// clearDeviceLinkMetrics drops every controller_link_metrics gauge series for a
// device that is still present in the ledger, keyed by its pubkey. updateStateCache
// calls this at the top of each per-device iteration before re-Setting the gauges
// for the device's currently active links. Clearing by device_pubkey (rather than
// per-interface) covers every way a gauge can go stale on a surviving device: a
// link removed or drained to an unlisted status, an interface removed or renamed
// on-chain, a device code change, or a device that gains a pathology and never
// reaches the interface loop.
func clearDeviceLinkMetrics(devicePubKey string) {
	linkMetrics.DeletePartialMatch(prometheus.Labels{"device_pubkey": devicePubKey})
}
