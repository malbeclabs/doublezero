package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Metrics names.
	MetricNameBuildInfo               = "doublezero_funder_build_info"
	MetricNameErrors                  = "doublezero_funder_errors_total"
	MetricNameFunderAccountBalanceSOL = "doublezero_funder_account_balance_sol"

	// Labels.
	LabelVersion       = "version"
	LabelCommit        = "commit"
	LabelDate          = "date"
	LabelErrorType     = "error_type"
	LabelFunderAccount = "funder_account"

	// Error types.
	ErrorTypeLoadServiceabilityState           = "load_serviceability_state"
	ErrorTypeGetFunderAccountBalance           = "get_funder_account_balance"
	ErrorTypeFunderAccountBalanceBelowMinimum  = "funder_account_balance_below_minimum"
	ErrorTypeGetMetricsPublisherAccountBalance = "get_metrics_publisher_account_balance"
	ErrorTypeTransferFundsToMetricsPublisher   = "transfer_funds_to_metrics_publisher"
	ErrorTypeWaitForMetricsPublisherBalance    = "wait_for_metrics_publisher_balance"
)

var (
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameBuildInfo,
			Help: "Build information of the funder agent",
		},
		[]string{LabelVersion, LabelCommit, LabelDate},
	)

	Errors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricNameErrors,
			Help: "Number of errors encountered",
		},
		[]string{LabelErrorType},
	)

	FunderAccountBalanceSOL = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricNameFunderAccountBalanceSOL,
			Help: "The balance of the funder account in SOL",
		},
		[]string{LabelFunderAccount},
	)
)
