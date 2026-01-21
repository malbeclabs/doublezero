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
		[]string{"type"}, // "input", "output", "cache_creation", "cache_read"
	)

	// Workflow metrics
	WorkflowRunsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_workflow_runs_total",
			Help: "Total number of workflow runs",
		},
		[]string{"classification"}, // "data_analysis", "conversational", "out_of_scope"
	)

	WorkflowLLMCallsPerRun = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_workflow_llm_calls_per_run",
			Help:    "Number of LLM calls per workflow run",
			Buckets: []float64{1, 2, 3, 5, 7, 10, 15, 20, 30, 50},
		},
	)

	WorkflowQueriesPerRun = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_workflow_queries_per_run",
			Help:    "Number of SQL queries executed per workflow run",
			Buckets: []float64{1, 2, 3, 5, 7, 10, 15, 20},
		},
	)

	WorkflowSQLErrorsPerRun = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "doublezero_lake_api_workflow_sql_errors_per_run",
			Help:    "Number of SQL errors per workflow run",
			Buckets: []float64{0, 1, 2, 3, 5, 10},
		},
	)

	WorkflowSQLErrorsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_workflow_sql_errors_total",
			Help: "Total number of SQL errors across all workflow runs",
		},
	)

	// Usage metrics
	UsageQuestionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_usage_questions_total",
			Help: "Total number of questions asked",
		},
		[]string{"account_type"}, // "domain", "wallet", "anonymous"
	)

	UsageQuestionsDailyGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_lake_api_usage_questions_daily",
			Help: "Number of questions asked today (resets at midnight UTC)",
		},
	)

	UsageTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "doublezero_lake_api_usage_tokens_total",
			Help: "Total number of tokens used for questions",
		},
		[]string{"type", "account_type"}, // type: "input"/"output", account_type: "domain"/"wallet"/"anonymous"
	)

	UsageGlobalLimitGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_lake_api_usage_global_limit",
			Help: "Configured global daily question limit (0 = unlimited)",
		},
	)

	UsageGlobalUtilization = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "doublezero_lake_api_usage_global_utilization",
			Help: "Current utilization of global daily limit (0-1, or 0 if unlimited)",
		},
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

// RecordAnthropicTokensWithCache records token usage including cache metrics.
func RecordAnthropicTokensWithCache(inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int64) {
	AnthropicTokensTotal.WithLabelValues("input").Add(float64(inputTokens))
	AnthropicTokensTotal.WithLabelValues("output").Add(float64(outputTokens))
	if cacheCreationTokens > 0 {
		AnthropicTokensTotal.WithLabelValues("cache_creation").Add(float64(cacheCreationTokens))
	}
	if cacheReadTokens > 0 {
		AnthropicTokensTotal.WithLabelValues("cache_read").Add(float64(cacheReadTokens))
	}
}

// RecordWorkflowRun records metrics for a completed workflow run.
func RecordWorkflowRun(classification string, llmCalls, sqlQueries, sqlErrors int) {
	WorkflowRunsTotal.WithLabelValues(classification).Inc()
	WorkflowLLMCallsPerRun.Observe(float64(llmCalls))
	WorkflowQueriesPerRun.Observe(float64(sqlQueries))
	WorkflowSQLErrorsPerRun.Observe(float64(sqlErrors))
	if sqlErrors > 0 {
		WorkflowSQLErrorsTotal.Add(float64(sqlErrors))
	}
}

// RecordUsageQuestion records a question for usage metrics.
func RecordUsageQuestion(accountType string) {
	UsageQuestionsTotal.WithLabelValues(accountType).Inc()
	UsageQuestionsDailyGauge.Inc()
}

// RecordUsageTokens records token usage by account type.
func RecordUsageTokens(accountType string, inputTokens, outputTokens int64) {
	if inputTokens > 0 {
		UsageTokensTotal.WithLabelValues("input", accountType).Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		UsageTokensTotal.WithLabelValues("output", accountType).Add(float64(outputTokens))
	}
}

// SetUsageGlobalLimit sets the global limit gauge for monitoring.
func SetUsageGlobalLimit(limit int) {
	UsageGlobalLimitGauge.Set(float64(limit))
}

// SetUsageGlobalUtilization sets the current utilization (0-1).
func SetUsageGlobalUtilization(utilization float64) {
	UsageGlobalUtilization.Set(utilization)
}

// ResetDailyGauge resets the daily question gauge (call at midnight UTC).
func ResetDailyGauge() {
	UsageQuestionsDailyGauge.Set(0)
}
