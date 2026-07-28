package jsonrpc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Retry counters, labelled by JSON-RPC method. Method cardinality is bounded by
// the Solana JSON-RPC surface, so it is safe as a label.
//
// A rising retriesTotal with a flat exhaustedTotal is the endpoint wobbling and
// the retries absorbing it. A rising exhaustedTotal means the caller is seeing
// errors and reads are actually failing.
var (
	retriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_solana_rpc_retries_total",
		Help: "Number of Solana JSON-RPC requests retried after a retryable error, by method.",
	}, []string{"method"})

	exhaustedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_solana_rpc_retries_exhausted_total",
		Help: "Number of Solana JSON-RPC requests that failed after exhausting all retry attempts, by method.",
	}, []string{"method"})
)
