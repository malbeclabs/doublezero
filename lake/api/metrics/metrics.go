package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "doublezero_lake_api_build_info",
			Help: "Build information of the DoubleZero Lake API",
		},
		[]string{"version", "commit", "date"},
	)

	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_lake_api_http_requests_in_flight",
			Help: "Number of HTTP requests currently being processed",
		},
	)

	// ClickHouse metrics
	ClickHouseQueriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_clickhouse_queries_total",
			Help: "Total number of ClickHouse queries",
		},
		[]string{"status"},
	)

	ClickHouseQueryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_clickhouse_query_duration_seconds",
			Help:    "Duration of ClickHouse queries in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms to ~41s
		},
	)

	// Anthropic API metrics
	AnthropicRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_anthropic_requests_total",
			Help: "Total number of Anthropic API requests",
		},
		[]string{"endpoint", "status"},
	)

	AnthropicRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_anthropic_request_duration_seconds",
			Help:    "Duration of Anthropic API requests in seconds",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 100ms to ~410s
		},
		[]string{"endpoint"},
	)

	AnthropicTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_anthropic_tokens_total",
			Help: "Total number of Anthropic API tokens used",
		},
		[]string{"type"}, // "input" or "output"
	)
)

// Middleware returns a chi middleware that records HTTP metrics.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		// Use the route pattern if available, otherwise use the path
		path := chi.RouteContext(r.Context()).RoutePattern()
		if path == "" {
			path = r.URL.Path
		}

		status := strconv.Itoa(ww.Status())
		duration := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// RecordClickHouseQuery records metrics for a ClickHouse query.
func RecordClickHouseQuery(duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	ClickHouseQueriesTotal.WithLabelValues(status).Inc()
	ClickHouseQueryDuration.Observe(duration.Seconds())
}

// RecordAnthropicRequest records metrics for an Anthropic API request.
func RecordAnthropicRequest(endpoint string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	AnthropicRequestsTotal.WithLabelValues(endpoint, status).Inc()
	AnthropicRequestDuration.WithLabelValues(endpoint).Observe(duration.Seconds())
}

// RecordAnthropicTokens records token usage for an Anthropic API request.
func RecordAnthropicTokens(inputTokens, outputTokens int64) {
	AnthropicTokensTotal.WithLabelValues("input").Add(float64(inputTokens))
	AnthropicTokensTotal.WithLabelValues("output").Add(float64(outputTokens))
}
