package jsonrpc

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// A rate-limited endpoint tells us how long to wait, and until now nothing in this
// stack could hear it. The retry budget is ~3s of jittered backoff across 4 attempts
// (see sleepBackoff), while the provider fronting our mainnet ledger enforces its
// limits over a rolling 10s window. Every attempt therefore lands inside the same
// window the first one was refused in, so a rate-limited call cannot be rescued by
// retrying — it burns four requests to be told the same thing, which is exactly the
// load the endpoint is trying to shed.
//
// The number that fixes this is on the response, in Retry-After. It cannot be read
// from the error: solana-go discards the http.Response inside CallForInto, and its
// error types carry a code and nothing else. So the transport records it against the
// in-flight call (see NoteRetryAfter) and the retry loop takes it before sleeping.
//
// Guessing instead of reading was the alternative, and it is worse in both
// directions: a fixed 10s wait stalls a call the endpoint would have served again in
// 1s, and it is still too short for a provider whose window is longer.

// retryAfterKey addresses the per-call slot in a context. An unexported key type
// means no other package can collide with it or read the slot by accident.
type retryAfterKey struct{}

// retryAfterSink holds the most recent Retry-After the transport saw for one call.
// Attempts run one at a time, but a single attempt can produce more than one round
// trip (a redirect, a retried h2 stream), so writes are atomic and the last one wins.
type retryAfterSink struct{ nanos atomic.Int64 }

// take returns the recorded wait and clears the slot. Clearing matters: without it a
// Retry-After from attempt 2 would still be sitting there before attempt 3, and a
// refusal that carried no header would be paced by a stale number.
func (s *retryAfterSink) take() time.Duration {
	return time.Duration(s.nanos.Swap(0))
}

// withRetryAfterSink returns a context carrying a fresh slot for the transport to
// write into, and the slot itself.
func withRetryAfterSink(ctx context.Context) (context.Context, *retryAfterSink) {
	sink := &retryAfterSink{}
	return context.WithValue(ctx, retryAfterKey{}, sink), sink
}

// NoteRetryAfter records how long an endpoint asked the caller to wait before
// repeating the in-flight request. It is for HTTP transports wrapping a client built
// by this repo's rpc package; a context from anywhere else is ignored, so calling it
// is always safe.
//
// Only the retry loop reads this, and only after an attempt has failed with a
// retryable error. So a header on a successful response costs nothing, and there is
// no need to filter by status code here — which matters, because the refusals that
// caused this work arrived as HTTP 200 with the rate limit inside the JSON-RPC
// envelope, a shape no status-code check would have matched.
func NoteRetryAfter(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	if sink, ok := ctx.Value(retryAfterKey{}).(*retryAfterSink); ok {
		sink.nanos.Store(int64(d))
	}
}

// ParseRetryAfter reads a Retry-After header value as a duration, relative to now.
// RFC 9110 allows either delta-seconds or an HTTP-date, and providers send both. A
// value that is missing, unparseable, or already in the past yields 0, meaning "the
// endpoint said nothing" — the caller then keeps its own backoff.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}
