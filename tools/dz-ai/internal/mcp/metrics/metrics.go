package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_ai_mcp_build_info",
			Help: "Build information of the DoubleZero AI MCP server",
		},
		[]string{"version", "commit", "date"},
	)

	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_mcp_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_mcp_http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 0.01s to ~41s
		},
	)

	AuthFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_mcp_auth_failures_total",
			Help: "Total number of authentication failures",
		},
		[]string{"reason"},
	)

	ToolCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_mcp_tool_calls_total",
			Help: "Total number of tool calls",
		},
		[]string{"tool_name", "status"},
	)

	ToolCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_mcp_tool_call_duration_seconds",
			Help:    "Duration of tool calls",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 0.01s to ~41s
		},
		[]string{"tool_name"},
	)

	ViewRefreshTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_mcp_view_refresh_total",
			Help: "Total number of view refreshes",
		},
		[]string{"view_type", "status"},
	)

	ViewRefreshDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_mcp_view_refresh_duration_seconds",
			Help:    "Duration of view refreshes",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 0.1s to ~410s (~6.8 minutes)
		},
		[]string{"view_type"},
	)

	DatabaseQueriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_ai_mcp_database_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"status"},
	)

	DatabaseQueryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_ai_mcp_database_query_duration_seconds",
			Help:    "Duration of database queries",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 0.001s to ~4.1s
		},
	)
)
