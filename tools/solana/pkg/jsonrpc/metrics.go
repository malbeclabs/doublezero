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

	// rateLimitedTotal counts rate-limited responses by the shape that carried the
	// limit. That shape is the diagnostic: an edge/CDN limiter answers with an HTTP
	// 429 status, while a limiter at or behind the origin commonly answers 200 with
	// the refusal inside the JSON-RPC envelope. Only the second reaches this package
	// as an *RPCError, and only the first is visible to the transport layer, so the
	// split between these two label values says which hop refused the call — the
	// question a provider will ask first, and one we could not answer for a 4.5h
	// getTransaction rate limit because nothing recorded it.
	rateLimitedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "doublezero_solana_rpc_ratelimited_total",
		Help: "Rate-limited JSON-RPC responses, by method and the error shape that carried the limit (http_status or jsonrpc_error).",
	}, []string{"method", "carrier"})
)
