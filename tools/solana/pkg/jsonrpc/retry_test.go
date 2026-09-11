package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	gojson "github.com/goccy/go-json"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestTools_Solana_JSONRPC_IsRetryableJSONRPC(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"net timeout", timeoutErr{}, true},
		{"econnreset", syscall.ECONNRESET, true},
		{"etimedout", syscall.ETIMEDOUT, true},
		{"econnrefused", syscall.ECONNREFUSED, true},
		{"broken pipe msg", errors.New("write: broken pipe"), true},
		{"closed conn msg", errors.New("use of closed network connection"), true},
		{"rate limited msg", errors.New("rate limited"), true},

		// Real solana-go *jsonrpc.HTTPError values. This is what the client returns
		// when a >=400 response body does not decode as a JSON-RPC envelope.
		{"HTTPError 429", httpError(http.StatusTooManyRequests), true},
		{"HTTPError 500", httpError(http.StatusInternalServerError), true},
		{"HTTPError 502", httpError(http.StatusBadGateway), true},
		{"HTTPError 503", httpError(http.StatusServiceUnavailable), true},
		{"HTTPError 504", httpError(http.StatusGatewayTimeout), true},
		{"HTTPError 400", httpError(http.StatusBadRequest), false},
		{"HTTPError 404", httpError(http.StatusNotFound), false},
		{"HTTPError 503 wrapped", fmt.Errorf("fetching program accounts: %w", httpError(http.StatusServiceUnavailable)), true},

		// Real solana-go *jsonrpc.RPCError values. This is what the client returns
		// when a >=400 response *does* carry a JSON-RPC error body — the shape
		// RPCPool's load-balancer 503s arrived in.
		{"RPCError 503 service unavailable", &jsonrpc.RPCError{Code: 503, Message: "Service unavailable"}, true},
		{"RPCError 429", &jsonrpc.RPCError{Code: 429, Message: "Too many requests"}, true},
		{"RPCError 502", &jsonrpc.RPCError{Code: 502, Message: "Bad gateway"}, true},
		{"RPCError busy -32005", &jsonrpc.RPCError{Code: -32005, Message: "Node is behind by 42 slots"}, true},
		{"RPCError busy -32004", &jsonrpc.RPCError{Code: -32004, Message: "Block not available for slot 42"}, true},
		{"RPCError busy -32429", &jsonrpc.RPCError{Code: -32429, Message: "Rate limit"}, true},
		// -32003 is a deterministic rejection of this exact payload, not a busy
		// signal. simulateTransaction is idempotent, so if this were retryable a
		// bad signature would burn the full budget against the endpoint every time.
		{"RPCError signature verification failure -32003", &jsonrpc.RPCError{Code: -32003, Message: "Transaction signature verification failure"}, false},
		// -32011 describes a node that carries no long-term history at all; the
		// same request to the same node cannot start succeeding.
		{"RPCError history not available -32011", &jsonrpc.RPCError{Code: -32011, Message: "Transaction history is not available from this node"}, false},
		{"RPCError transient message, no code", &jsonrpc.RPCError{Code: -32603, Message: "Service Unavailable"}, true},
		{"RPCError invalid params", &jsonrpc.RPCError{Code: -32602, Message: "Invalid param: not a Pubkey"}, false},
		{"RPCError preflight failure", &jsonrpc.RPCError{Code: -32002, Message: "Transaction simulation failed: Blockhash not found"}, false},
		{"RPCError method not found", &jsonrpc.RPCError{Code: -32601, Message: "Method not found"}, false},

		{"json syntax", &json.SyntaxError{Offset: 1}, false},
		{"truncated body msg", errors.New("could not decode body to rpc response: unexpected end of JSON input"), true},
		{"truncated body decode", truncatedJSONErr(), true},
		{"truncated body nul decode", truncatedJSONNulErr(), true},
		{"malformed but complete json", malformedJSONErr(), false},
		{"random non-retryable", errors.New("bad request"), false},
		{"net.Error non-timeout", net.UnknownNetworkError("wat"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isRetryableJSONRPC(tc.err))
		})
	}
}

func TestWithRetry_NilOptionsUsesDefaults(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callForIntoSeq: []error{syscall.ECONNRESET, nil},
	}

	c := WithRetry(inner, nil)

	var out any
	require.NoError(t, c.CallForInto(context.Background(), &out, "m", nil))
	require.Equal(t, int32(2), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_DoRetry_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callForIntoSeq: []error{syscall.ECONNRESET, syscall.ETIMEDOUT, nil},
	}
	c := WithRetry(inner, fastRetryOpt(5))

	var out any
	require.NoError(t, c.CallForInto(context.Background(), &out, "m", nil))
	require.Equal(t, int32(3), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_DoRetry_StopsOnNonRetryable(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callForIntoSeq: []error{errors.New("bad request"), nil},
	}
	c := WithRetry(inner, fastRetryOpt(5))

	var out any
	err := c.CallForInto(context.Background(), &out, "m", nil)
	require.Error(t, err)
	require.Equal(t, int32(1), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_DoRetry_ExhaustsAttempts(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callForIntoSeq: []error{syscall.ECONNRESET, syscall.ECONNRESET, syscall.ECONNRESET},
	}
	c := WithRetry(inner, fastRetryOpt(3))

	var out any
	err := c.CallForInto(context.Background(), &out, "m", nil)
	require.Error(t, err)
	require.Equal(t, int32(3), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_DoRetry_ContextCancelDuringBackoff(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callForIntoSeq: []error{syscall.ECONNRESET, nil},
	}
	c := WithRetry(inner, &RetryOptions{
		MaxAttempts: 3,
		BaseBackoff: 200 * time.Millisecond,
		MaxBackoff:  200 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var out any
	err := c.CallForInto(ctx, &out, "m", nil)
	require.ErrorIs(t, err, context.Canceled)
	// The error that triggered the retry must survive, or an operator whose caller
	// deadline is shorter than the retry budget sees only the ctx error.
	require.ErrorIs(t, err, syscall.ECONNRESET)
	require.Equal(t, int32(1), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_CallBatch_RetriesAndReturnsResponses(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callBatchSeq: []error{syscall.ECONNRESET, nil},
	}
	c := WithRetry(inner, fastRetryOpt(3))

	_, err := c.CallBatch(context.Background(), jsonrpc.RPCRequests{})
	require.NoError(t, err)
	require.Equal(t, int32(2), inner.callBatchN.Load())
}

func TestTools_Solana_JSONRPC_DoRetry_CustomIsRetryable(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("custom retryable error")
	inner := &seqClient{
		callForIntoSeq: []error{sentinel, nil},
	}
	c := WithRetry(inner, &RetryOptions{
		MaxAttempts:     3,
		BaseBackoff:     1 * time.Millisecond,
		MaxBackoff:      2 * time.Millisecond,
		IsRetryableFunc: func(err error) bool { return errors.Is(err, sentinel) },
	})

	var out any
	require.NoError(t, c.CallForInto(context.Background(), &out, "m", nil))
	require.Equal(t, int32(2), inner.callForIntoN.Load())
}

func TestTools_Solana_JSONRPC_CallWithCallback_Retries(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callWithCbSeq: []error{syscall.ETIMEDOUT, nil},
	}
	c := WithRetry(inner, fastRetryOpt(3))

	require.NoError(t, c.CallWithCallback(context.Background(), "m", nil, func(*http.Request, *http.Response) error { return nil }))
	require.Equal(t, int32(2), inner.callWithCbN.Load())
}

// A retryable failure on a non-idempotent write must not be resent: the endpoint may
// already have accepted the transaction, and the resend surfaces as "already
// processed" instead of the signature that actually landed. This holds even when the
// caller explicitly asks for retries.
func TestTools_Solana_JSONRPC_NonIdempotentMethodsAreNeverRetried(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"sendTransaction", "requestAirdrop"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			inner := &seqClient{callForIntoSeq: []error{syscall.ECONNRESET, nil}}
			c := WithRetry(inner, fastRetryOpt(5))

			var out any
			err := c.CallForInto(context.Background(), &out, method, nil)
			require.Error(t, err)
			require.Equal(t, int32(1), inner.callForIntoN.Load())
		})
	}
}

func TestTools_Solana_JSONRPC_IdempotentMethodsAreRetried(t *testing.T) {
	t.Parallel()

	// simulateTransaction only asks the endpoint what would happen, so repeating it
	// is safe even though it carries a transaction.
	for _, method := range []string{"getProgramAccounts", "getAccountInfo", "simulateTransaction"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			inner := &seqClient{callForIntoSeq: []error{syscall.ECONNRESET, nil}}
			c := WithRetry(inner, fastRetryOpt(5))

			var out any
			require.NoError(t, c.CallForInto(context.Background(), &out, method, nil))
			require.Equal(t, int32(2), inner.callForIntoN.Load())
		})
	}
}

func TestTools_Solana_JSONRPC_CallBatch_NotRetriedWhenAnyRequestIsNonIdempotent(t *testing.T) {
	t.Parallel()

	inner := &seqClient{callBatchSeq: []error{syscall.ECONNRESET, nil}}
	c := WithRetry(inner, fastRetryOpt(5))

	_, err := c.CallBatch(context.Background(), jsonrpc.RPCRequests{
		{Method: "getAccountInfo"},
		{Method: "sendTransaction"},
	})
	require.Error(t, err)
	require.Equal(t, int32(1), inner.callBatchN.Load())
}

func TestTools_Solana_JSONRPC_CallWithCallback_NonIdempotentNotRetried(t *testing.T) {
	t.Parallel()

	inner := &seqClient{callWithCbSeq: []error{syscall.ETIMEDOUT, nil}}
	c := WithRetry(inner, fastRetryOpt(5))

	err := c.CallWithCallback(context.Background(), "sendTransaction", nil, func(*http.Request, *http.Response) error { return nil })
	require.Error(t, err)
	require.Equal(t, int32(1), inner.callWithCbN.Load())
}

func TestTools_Solana_JSONRPC_RetryMetrics(t *testing.T) {
	// Not parallel: asserts on package-level counters. Each subtest uses its own
	// method label so it cannot be perturbed by the other tests in this package.
	t.Run("retries counted, exhaustion not", func(t *testing.T) {
		const method = "metricsRetryThenSucceed"
		inner := &seqClient{callForIntoSeq: []error{syscall.ECONNRESET, syscall.ECONNRESET, nil}}
		c := WithRetry(inner, fastRetryOpt(5))

		var out any
		require.NoError(t, c.CallForInto(context.Background(), &out, method, nil))
		require.Equal(t, 2.0, testutil.ToFloat64(retriesTotal.WithLabelValues(method)))
		require.Equal(t, 0.0, testutil.ToFloat64(exhaustedTotal.WithLabelValues(method)))
	})

	t.Run("exhaustion counted", func(t *testing.T) {
		const method = "metricsExhaust"
		inner := &seqClient{callForIntoSeq: []error{syscall.ECONNRESET, syscall.ECONNRESET, syscall.ECONNRESET}}
		c := WithRetry(inner, fastRetryOpt(3))

		var out any
		require.Error(t, c.CallForInto(context.Background(), &out, method, nil))
		require.Equal(t, 2.0, testutil.ToFloat64(retriesTotal.WithLabelValues(method)))
		require.Equal(t, 1.0, testutil.ToFloat64(exhaustedTotal.WithLabelValues(method)))
	})

	t.Run("non-idempotent method records neither", func(t *testing.T) {
		const method = "sendTransaction"
		before := testutil.ToFloat64(exhaustedTotal.WithLabelValues(method))
		inner := &seqClient{callForIntoSeq: []error{syscall.ECONNRESET}}
		c := WithRetry(inner, fastRetryOpt(3))

		var out any
		require.Error(t, c.CallForInto(context.Background(), &out, method, nil))
		require.Equal(t, 0.0, testutil.ToFloat64(retriesTotal.WithLabelValues(method)))
		require.Equal(t, before, testutil.ToFloat64(exhaustedTotal.WithLabelValues(method)))
	})
}

// Backoff must stay inside the configured ceiling per attempt, and jitter must not
// push it above it. Callers poll on a 10s (controller) or 60s (state-ingest) ticker
// and a read budget that overran would overlap ticks.
func TestTools_Solana_JSONRPC_SleepBackoff_JitteredWithinBounds(t *testing.T) {
	t.Parallel()

	opt := RetryOptions{BaseBackoff: 20 * time.Millisecond, MaxBackoff: 40 * time.Millisecond}
	var sawBelowCeiling bool
	for i := 0; i < 20; i++ {
		start := time.Now()
		require.NoError(t, sleepBackoff(context.Background(), opt, 3)) // nominal 40ms, capped at 40ms
		elapsed := time.Since(start)
		require.GreaterOrEqual(t, elapsed, 20*time.Millisecond, "jitter must not drop below half the interval")
		if elapsed < 40*time.Millisecond {
			sawBelowCeiling = true
		}
	}
	require.True(t, sawBelowCeiling, "backoff is not jittered; every wait hit the ceiling")
}

// compile-time: ensure wrapper still satisfies interface
var _ solanarpc.JSONRPCClient = (*retryingJSONRPCClient)(nil)

type seqClient struct {
	callForIntoSeq []error
	callWithCbSeq  []error
	callBatchSeq   []error

	callForIntoN atomic.Int32
	callWithCbN  atomic.Int32
	callBatchN   atomic.Int32
}

func (s *seqClient) CallForInto(ctx context.Context, out any, method string, params []any) error {
	i := int(s.callForIntoN.Add(1)) - 1
	if i >= len(s.callForIntoSeq) {
		return nil
	}
	return s.callForIntoSeq[i]
}

func (s *seqClient) CallWithCallback(ctx context.Context, method string, params []any, cb func(*http.Request, *http.Response) error) error {
	i := int(s.callWithCbN.Add(1)) - 1
	if i >= len(s.callWithCbSeq) {
		return nil
	}
	return s.callWithCbSeq[i]
}

func (s *seqClient) CallBatch(ctx context.Context, req jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	i := int(s.callBatchN.Add(1)) - 1
	if i >= len(s.callBatchSeq) {
		return jsonrpc.RPCResponses{}, nil
	}
	return nil, s.callBatchSeq[i]
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false } // satisfies net.Error; not used by prod code

// httpError builds the same *jsonrpc.HTTPError solana-go returns for a >=400
// response whose body did not decode as a JSON-RPC envelope. It must be built via
// the exported constructor: the wrapped error is a private field and Error() panics
// on a zero value.
func httpError(code int) error {
	return jsonrpc.NewHTTPError(code, fmt.Errorf("rpc call getProgramAccounts() on http://ledger status code: %d", code))
}

// goccyDecode mirrors the solana-go rpc/jsonrpc client's decoder configuration
// (goccy/go-json streaming, DisallowUnknownFields + UseNumber) so these tests
// exercise the decoder the client actually uses, not stdlib encoding/json (which
// the client never produces).
func goccyDecode(body string) error {
	dec := gojson.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var v any
	return dec.Decode(&v)
}

// truncatedJSONErr returns a real goccy decode error for a body cut off
// mid-stream ("unexpected end of JSON input").
func truncatedJSONErr() error { return goccyDecode(`{"`) }

// truncatedJSONNulErr returns the other real goccy truncation error: a NUL
// invalid-character from its leftover decode buffer.
func truncatedJSONNulErr() error { return goccyDecode(`{`) }

// malformedJSONErr returns a goccy decode error for complete but malformed JSON,
// which must stay non-retryable.
func malformedJSONErr() error { return goccyDecode(`{"a" "b"}`) }

func fastRetryOpt(max int) *RetryOptions {
	return &RetryOptions{
		MaxAttempts: max,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	}
}

// TestRateLimitCarrier distinguishes which hop refused the call. An HTTP 429 status
// is edge-shaped and also visible to the transport; a 429 inside a JSON-RPC envelope
// on an otherwise-successful response is origin-shaped and invisible to every
// HTTP-level metric. Being unable to tell these apart is what left a 4.5h
// getTransaction rate limit unattributable.
func TestRateLimitCarrier(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"http 429", &jsonrpc.HTTPError{Code: 429}, "http_status"},
		{"http 503 is not a rate limit", &jsonrpc.HTTPError{Code: 503}, ""},
		{"envelope 429", &jsonrpc.RPCError{Code: 429, Message: "Too many requests for a specific RPC call"}, "jsonrpc_error"},
		{"envelope -32429", &jsonrpc.RPCError{Code: -32429}, "jsonrpc_error"},
		{"envelope -32005 is not a rate limit", &jsonrpc.RPCError{Code: -32005}, ""},
		{"wrapped envelope 429", fmt.Errorf("get transaction: %w", &jsonrpc.RPCError{Code: 429}), "jsonrpc_error"},
		{"unrelated", errors.New("connection reset by peer"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rateLimitCarrier(tt.err); got != tt.want {
				t.Errorf("rateLimitCarrier() = %q, want %q", got, tt.want)
			}
		})
	}
}
