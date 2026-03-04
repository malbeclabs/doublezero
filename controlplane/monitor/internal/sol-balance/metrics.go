package solbalance

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metric names.
	MetricNameBalanceLamports = "doublezero_monitor_sol_balance_lamports"
	MetricNameBalanceSOL      = "doublezero_monitor_sol_balance_sol"
	MetricNameErrors          = "doublezero_monitor_sol_balance_errors_total"

	// Labels.
	MetricLabelAccount   = "account"
	MetricLabelErrorType = "error_type"

	// Error types.
	MetricErrorTypeGetBalance = "get_balance"

	// Lamports per SOL.
	LamportsPerSOL = 1_000_000_000
)

var (
	MetricBalanceLamports = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBalanceLamports,
			Help: "SOL balance in lamports",
		},
		[]string{MetricLabelAccount},
	)

	MetricBalanceSOL = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBalanceSOL,
			Help: "SOL balance in SOL",
		},
		[]string{MetricLabelAccount},
	)

	MetricErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{MetricLabelErrorType},
	)
)
