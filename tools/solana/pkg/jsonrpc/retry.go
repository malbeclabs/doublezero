package jsonrpc

import (
	"context"
	"errors"
	"io"
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
	return doRetry(ctx, c.opt, func(ctx context.Context) error {
		return c.inner.CallForInto(ctx, out, method, params)
	})
}

func (c *retryingJSONRPCClient) CallWithCallback(ctx context.Context, method string, params []any, callback func(*http.Request, *http.Response) error) error {
	return doRetry(ctx, c.opt, func(ctx context.Context) error {
		return c.inner.CallWithCallback(ctx, method, params, callback)
	})
}

func (c *retryingJSONRPCClient) CallBatch(ctx context.Context, requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	var resp jsonrpc.RPCResponses
	err := doRetry(ctx, c.opt, func(ctx context.Context) error {
		r, err := c.inner.CallBatch(ctx, requests)
		if err != nil {
			return err
		}
		resp = r
		return nil
	})
	return resp, err
}

func doRetry(ctx context.Context, opt RetryOptions, f func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= opt.MaxAttempts; attempt++ {
		if attempt > 1 {
			d := opt.BaseBackoff * time.Duration(attempt-1)
			if d > opt.MaxBackoff {
				d = opt.MaxBackoff
			}
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			}
		}

		lastErr = f(ctx)
		if lastErr == nil || !opt.IsRetryableFunc(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func isRetryableJSONRPC(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation is authoritative
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
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

	// HTTP-layer retry signals (if exposed by the client)
	type hasStatusCode interface{ StatusCode() int }
	var sc hasStatusCode
	if errors.As(err, &sc) {
		switch sc.StatusCode() {
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		}
	}

	// JSON-RPC server "retry later / busy" codes (provider-specific)
	type hasCode interface{ Code() int }
	var ce hasCode
	if errors.As(err, &ce) {
		switch ce.Code() {
		case -32005, -32004, -32003, -32429:
			return true
		}
	}

	return false
}
