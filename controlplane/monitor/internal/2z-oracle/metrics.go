package twozoracle

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameErrors           = "doublezero_monitor_twozoracle_errors_total"
	MetricNameHealthNotHealthy = "doublezero_monitor_twozoracle_health_not_healthy_total"
	MetricNameSwapRate         = "doublezero_monitor_twozoracle_swap_rate"
	MetricNameSOLPriceUSD      = "doublezero_monitor_twozoracle_sol_price_usd"
	MetricNameTwoZPriceUSD     = "doublezero_monitor_twozoracle_twoz_price_usd"
	MetricNameHealthResponse   = "doublezero_monitor_twozoracle_health_responses_total"
	MetricNameSwapRateResponse = "doublezero_monitor_twozoracle_swap_rate_responses_total"

	// Labels.
	MetricLabelErrorType  = "error_type"
	MetricLabelStatusCode = "status_code"

	// Error types.
	MetricErrorTypeGetHealth         = "get_health"
	MetricErrorTypeGetSwapRate       = "get_swap_rate"
	MetricErrorTypeParseSOLPriceUSD  = "parse_sol_price_usd"
	MetricErrorTypeParseTwoZPriceUSD = "parse_twoz_price_usd"
	MetricErrorTypeMalformedSwapRate = "malformed_swap_rate"
)

var (
	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType, MetricLabelStatusCode},
	)

	MetricHealthResponse = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameHealthResponse,
			Help: "Number of health responses",
		},
		[]string{MetricLabelStatusCode},
	)

	MetricSwapRateResponse = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameSwapRateResponse,
			Help: "Number of swap rate responses",
		},
		[]string{MetricLabelStatusCode},
	)

	MetricHealthNotHealthy = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: MetricNameHealthNotHealthy,
			Help: "Number of health not healthy",
		},
	)

	MetricSwapRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameSwapRate,
			Help: "Swap rate",
		},
	)

	MetricSOLPriceUSD = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameSOLPriceUSD,
			Help: "SOL price USD",
		},
	)

	MetricTwoZPriceUSD = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: MetricNameTwoZPriceUSD,
			Help: "2Z price USD",
		},
	)
)
