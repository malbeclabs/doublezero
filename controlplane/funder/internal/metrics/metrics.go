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
	ErrorTypeGetRecipients                           = "get_recipients"
	ErrorTypeGetFunderAccountBalance                 = "get_funder_account_balance"
	ErrorTypeFunderAccountBalanceBelowMinimum        = "funder_account_balance_below_minimum"
	ErrorTypeGetRecipientAccountBalance              = "get_recipient_account_balance"
	ErrorTypeTransferFundsToRecipient                = "transfer_funds_to_recipient"
	ErrorTypeWaitForRecipientAccountBalance          = "wait_for_recipient_account_balance"
	ErrorTypeGetInternetLatencyCollectorBalance      = "get_internet_latency_collector_account_balance"
	ErrorTypeTransferFundsToInternetLatencyCollector = "transfer_funds_to_internet_latency_collector"
	ErrorTypeWaitForInternetLatencyCollectorBalance  = "wait_for_internet_latency_collector_balance"
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
