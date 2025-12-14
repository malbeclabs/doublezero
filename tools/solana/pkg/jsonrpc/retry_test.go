package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
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
		{"http 429", statusCodeErr(http.StatusTooManyRequests), true},
		{"http 503", statusCodeErr(http.StatusServiceUnavailable), true},
		{"rpc busy -32005", rpcCodeErr(-32005), true},
		{"json syntax", &json.SyntaxError{Offset: 1}, false},
		{"random non-retryable", errors.New("bad request"), false},
		{"net.Error non-timeout", net.UnknownNetworkError("wat"), false},
	}

	for _, tc := range cases {
		tc := tc
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

func TestTools_Solana_JSONRPC_CallWithCallback_Retries(t *testing.T) {
	t.Parallel()

	inner := &seqClient{
		callWithCbSeq: []error{syscall.ETIMEDOUT, nil},
	}
	c := WithRetry(inner, fastRetryOpt(3))

	require.NoError(t, c.CallWithCallback(context.Background(), "m", nil, func(*http.Request, *http.Response) error { return nil }))
	require.Equal(t, int32(2), inner.callWithCbN.Load())
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

type statusCodeErr int

func (e statusCodeErr) Error() string   { return "http status" }
func (e statusCodeErr) StatusCode() int { return int(e) }

type rpcCodeErr int

func (e rpcCodeErr) Error() string { return "rpc code" }
func (e rpcCodeErr) Code() int     { return int(e) }

func fastRetryOpt(max int) *RetryOptions {
	return &RetryOptions{
		MaxAttempts: max,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	}
}
