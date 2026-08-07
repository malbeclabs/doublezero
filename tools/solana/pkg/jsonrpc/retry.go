package jsonrpc

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const (
	defaultMaxAttempts = 4
	defaultBaseBackoff = 500 * time.Millisecond
	defaultMaxBackoff  = 5 * time.Second

	// batchLabel is the metric label for CallBatch, which carries a mix of methods.
	batchLabel = "batch"
)

type RetryOptions struct {
	MaxAttempts     int
	BaseBackoff     time.Duration
	MaxBackoff      time.Duration
	IsRetryableFunc func(error) bool
}

func WithRetry(inner solanarpc.JSONRPCClient, opt *RetryOptions) solanarpc.JSONRPCClient {
	if opt == nil {
		opt = &RetryOptions{}
	}
	if opt.MaxAttempts <= 0 {
		opt.MaxAttempts = defaultMaxAttempts
	}
	if opt.BaseBackoff <= 0 {
		opt.BaseBackoff = defaultBaseBackoff
	}
	if opt.MaxBackoff <= 0 {
		opt.MaxBackoff = defaultMaxBackoff
	}
	if opt.IsRetryableFunc == nil {
		opt.IsRetryableFunc = isRetryableJSONRPC
	}
	return &retryingJSONRPCClient{inner: inner, opt: *opt}
}

type retryingJSONRPCClient struct {
	inner solanarpc.JSONRPCClient
	opt   RetryOptions
}

func (c *retryingJSONRPCClient) CallForInto(ctx context.Context, out any, method string, params []any) error {
	return doRetry(ctx, c.opt, method, isIdempotentMethod(method), func(ctx context.Context) error {
		return c.inner.CallForInto(ctx, out, method, params)
	})
}

func (c *retryingJSONRPCClient) CallWithCallback(ctx context.Context, method string, params []any, callback func(*http.Request, *http.Response) error) error {
	return doRetry(ctx, c.opt, method, isIdempotentMethod(method), func(ctx context.Context) error {
		return c.inner.CallWithCallback(ctx, method, params, callback)
	})
}

func (c *retryingJSONRPCClient) CallBatch(ctx context.Context, requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	var resp jsonrpc.RPCResponses
	err := doRetry(ctx, c.opt, batchLabel, batchIsIdempotent(requests), func(ctx context.Context) error {
		r, err := c.inner.CallBatch(ctx, requests)
		if err != nil {
			return err
		}
		resp = r
		return nil
	})
	return resp, err
}

// nonIdempotentMethods must never be retried, no matter what RetryOptions a caller
// passes. Re-sending a signed transaction after a transport error can resubmit one
// the endpoint already accepted: the resend fails with "already processed" and the
// caller reads a landed transaction as a failure. Reads and simulateTransaction are
// safe to repeat, so retry stays on by default for everything else.
var nonIdempotentMethods = map[string]struct{}{
	"sendTransaction": {},
	"requestAirdrop":  {},
}

func isIdempotentMethod(method string) bool {
	_, nonIdempotent := nonIdempotentMethods[method]
	return !nonIdempotent
}

// batchIsIdempotent reports whether every request in a batch is safe to repeat.
// A batch is retried as a whole, so one non-idempotent member disqualifies it.
func batchIsIdempotent(requests jsonrpc.RPCRequests) bool {
	for _, req := range requests {
		if req != nil && !isIdempotentMethod(req.Method) {
			return false
		}
	}
	return true
}

func doRetry(ctx context.Context, opt RetryOptions, label string, idempotent bool, f func(context.Context) error) error {
	maxAttempts := opt.MaxAttempts
	if !idempotent {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepBackoff(ctx, opt, attempt); err != nil {
				// Keep the error that caused the retry. A caller whose deadline is
				// shorter than the retry budget would otherwise log only "context
				// deadline exceeded" and lose the 503 behind it — the one thing
				// worth seeing during an endpoint outage.
				return errors.Join(err, lastErr)
			}
			retriesTotal.WithLabelValues(label).Inc()
		}

		lastErr = f(ctx)
		if carrier := rateLimitCarrier(lastErr); carrier != "" {
			rateLimitedTotal.WithLabelValues(label, carrier).Inc()
		}
		if lastErr == nil || !opt.IsRetryableFunc(lastErr) {
			return lastErr
		}
	}

	// Every attempt failed with a retryable error. Surface it — never a silent
	// empty result — and record it so an endpoint outage is visible in metrics.
	if lastErr != nil && maxAttempts > 1 {
		exhaustedTotal.WithLabelValues(label).Inc()
	}
	return lastErr
}

// sleepBackoff waits before the given attempt, honoring ctx cancellation and
// deadlines. The wait is jittered over [d/2, d] so that the ~60 doublezerod hosts
// and dozen services reading the same ledger endpoint do not retry in lockstep and
// re-spike an endpoint that is already shedding load. Jitter keeps a floor at half
// the interval rather than reaching down to zero, so the total budget stays
// predictable against the caller's poll interval.
func sleepBackoff(ctx context.Context, opt RetryOptions, attempt int) error {
	d := opt.BaseBackoff * time.Duration(attempt-1)
	if d > opt.MaxBackoff {
		d = opt.MaxBackoff
	}
	if d > 0 {
		d = d/2 + rand.N(d/2+1)
	}

	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isRetryableHTTPStatus reports whether an HTTP status is worth another attempt:
// the endpoint is shedding load or a load balancer in front of it is unhealthy,
// rather than the request being wrong.
func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// isRetryableRPCCode covers the "busy / retry later" JSON-RPC codes: the node
// cannot serve this request right now, but an identical request later can succeed.
//
//	-32005 NODE_UNHEALTHY (Agave): the node is behind the cluster by more than its
//	        health-check slot distance. It catches up, or a load balancer sends the
//	        retry to a backend that already has.
//	-32004 BLOCK_NOT_AVAILABLE (Agave): the requested slot has not reached this node
//	        yet. Same shape — it arrives, or another backend already holds it.
//	-32429  not an Agave code. Providers that front Agave (RPCPool in our case) mint
//	        it to mirror HTTP 429 inside a JSON-RPC envelope, so it reaches us as an
//	        *RPCError rather than an *HTTPError. Rate limits lift; retry with backoff.
//
// Deliberately absent:
//
//	-32003 TRANSACTION_SIGNATURE_VERIFICATION_FAILURE — a deterministic rejection of
//	        this exact payload. Retrying burns the whole budget to be told the same
//	        thing, every time, against an endpoint that is often already degraded.
//	-32011 TRANSACTION_HISTORY_NOT_AVAILABLE — describes a node that does not carry
//	        long-term history at all, not one that is momentarily busy. Retrying the
//	        same node cannot change the answer.
//
// Codes are only listed here once there is a reason to believe the same request
// later succeeds. An unlisted code falls through to the transport checks in
// isRetryableJSONRPC and ends up non-retryable, which is the safe default.
func isRetryableRPCCode(code int) bool {
	switch code {
	case -32005, -32004, -32429:
		return true
	}
	return false
}

// rateLimitCarrier reports which error shape carried a rate limit, or "" if err is
// not one. The distinction is the point: an *HTTPError means the refusal arrived as
// an HTTP 429 status, which an edge or CDN limiter produces and the transport layer
// can also see; an *RPCError means it arrived inside a JSON-RPC envelope on an
// otherwise-successful response, which is what a limiter at or behind the origin
// typically does and which no HTTP-level metric will ever show.
//
// -32429 is included because a provider fronting Agave mints it to mirror HTTP 429
// inside an envelope (see isRetryableRPCCode); a bare positive 429 in an *RPCError
// is a different origin from the same provider's -32429, which is precisely the
// kind of thing worth being able to tell apart after the fact.
func rateLimitCarrier(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *jsonrpc.HTTPError
	if errors.As(err, &httpErr) && httpErr.Code == http.StatusTooManyRequests {
		return "http_status"
	}
	var rpcErr *jsonrpc.RPCError
	if errors.As(err, &rpcErr) && (rpcErr.Code == http.StatusTooManyRequests || rpcErr.Code == -32429) {
		return "jsonrpc_error"
	}
	return ""
}

func isRetryableJSONRPC(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation is authoritative
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// HTTP status, read off the concrete solana-go error types. Both expose their
	// code as a struct field, not a method, so an interface assertion on
	// StatusCode()/Code() matches nothing in production — that was the bug that
	// made every Go ledger reader give up after one attempt on RPCPool's 503s.
	//
	// *HTTPError carries the real HTTP status and is returned when a >=400 response
	// body did not decode as a JSON-RPC envelope. *RPCError carries the code from a
	// body that did decode; a provider fronting a load balancer commonly puts the
	// HTTP status there, which is how those 503s actually arrived.
	var httpErr *jsonrpc.HTTPError
	if errors.As(err, &httpErr) && isRetryableHTTPStatus(httpErr.Code) {
		return true
	}

	// Neither branch returns false on a non-matching code: an unrecognized code
	// falls through to the transport checks below, which also match transient
	// wording from providers that set no machine-readable code. A code we do
	// recognize as an onchain or request-level rejection (-32002, -32602, …) has no
	// transient wording to match and ends up non-retryable there.
	var rpcErr *jsonrpc.RPCError
	if errors.As(err, &rpcErr) && (isRetryableHTTPStatus(rpcErr.Code) || isRetryableRPCCode(rpcErr.Code)) {
		return true
	}

	// Timeouts (net.Error.Timeout is still valid)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Common transport / syscall failures
	if errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "rate limited") ||
		// Providers that return transient wording without a machine-readable code.
		// A load balancer shedding load is the same class of failure whether it
		// labels itself 503 or just says so.
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "bad gateway") ||
		strings.Contains(msg, "gateway timeout") ||
		strings.Contains(msg, "gateway time-out") ||
		// Truncated/partial response body (e.g. a 200 whose body is cut off
		// mid-stream). The solana-go client decodes with goccy/go-json, which
		// reports most cut points as "unexpected end of JSON input" and some as
		// a NUL invalid-character from its leftover decode buffer (a raw NUL
		// never appears in valid JSON, so it's distinctive of a truncated read).
		// Complete-but-malformed JSON (e.g. "expected colon after object key")
		// is deliberately not matched and falls through to non-retryable.
		strings.Contains(msg, "unexpected end of json input") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "invalid character '\x00'") {
		return true
	}

	return false
}
