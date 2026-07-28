package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/tools/solana/pkg/jsonrpc"
)

func TestNewWithRetries_RetriesOnEOFThenSucceeds(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)

		// First request: force an EOF-ish failure by hijacking and closing.
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			require.True(t, ok, "ResponseWriter must support hijacking")
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}

		// Second request: respond with a valid JSON-RPC response (e.g. getVersion).
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = r.Body.Close()

		var req struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"solana-core": "1.0.0",
				"feature-set": 0,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	cl := NewWithRetries(srv.URL, &jsonrpc.RetryOptions{
		MaxAttempts: 3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ver, err := cl.GetVersion(ctx)
	require.NoError(t, err)
	require.NotNil(t, ver)
	require.GreaterOrEqual(t, hits.Load(), int32(2), "expected at least 2 HTTP attempts")
}

func TestNewWithHeadersAndRetries_SendsHeaders(t *testing.T) {
	t.Parallel()

	wantHeaders := map[string]string{
		"X-Test-Header": "abc123",
		"X-Other":       "zzz",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range wantHeaders {
			require.Equal(t, v, r.Header.Get(k), "missing/incorrect header %q", k)
		}

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = r.Body.Close()

		var req struct {
			ID any `json:"id"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"solana-core": "1.0.0",
				"feature-set": 0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	cl := NewWithHeadersAndRetries(srv.URL, wantHeaders, &jsonrpc.RetryOptions{
		MaxAttempts: 2,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := cl.GetVersion(ctx)
	require.NoError(t, err)
}

// Reproduces the 2026-07-28 incident end-to-end through real solana-go: RPCPool
// returned HTTP 503 with a JSON-RPC error body on getProgramAccounts, which
// solana-go surfaces as *jsonrpc.RPCError (not *jsonrpc.HTTPError). Before the
// classifier fix this made exactly one attempt.
func TestNewWithRetries_RetriesServiceUnavailable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		status      int
		body        string
		wantRetried bool
	}{
		{
			// The incident's shape: an HTTP error status with a decodable JSON-RPC
			// error envelope, so the status never reaches the caller as an HTTPError.
			name:        "503 with json-rpc error body",
			status:      http.StatusServiceUnavailable,
			body:        `{"jsonrpc":"2.0","error":{"code":503,"message":"Service unavailable"},"id":%q}`,
			wantRetried: true,
		},
		{
			// An HTML/plain body from a load balancer: undecodable, so solana-go
			// wraps it in *jsonrpc.HTTPError with the real status.
			name:        "503 with non-json body",
			status:      http.StatusServiceUnavailable,
			body:        `<html><body>Service Unavailable</body></html>`,
			wantRetried: true,
		},
		{
			name:        "429 with json-rpc error body",
			status:      http.StatusTooManyRequests,
			body:        `{"jsonrpc":"2.0","error":{"code":-32429,"message":"Too many requests"},"id":%q}`,
			wantRetried: true,
		},
		{
			// A request the endpoint will reject the same way every time must not
			// be retried — retrying only adds load to a degraded endpoint.
			name:        "400 invalid params",
			status:      http.StatusBadRequest,
			body:        `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid param: not a Pubkey"},"id":%q}`,
			wantRetried: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_ = r.Body.Close()
				var req struct {
					ID any `json:"id"`
				}
				require.NoError(t, json.Unmarshal(body, &req))

				out := tc.body
				if strings.Contains(out, "%q") {
					out = fmt.Sprintf(out, fmt.Sprint(req.ID))
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(out))
			}))
			defer srv.Close()

			cl := NewWithRetries(srv.URL, &jsonrpc.RetryOptions{
				MaxAttempts: 4,
				BaseBackoff: 1 * time.Millisecond,
				MaxBackoff:  2 * time.Millisecond,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Retry exhaustion must surface as an error, never a silent empty result.
			_, err := cl.GetProgramAccounts(ctx, solana.SystemProgramID)
			require.Error(t, err)

			if tc.wantRetried {
				require.Equal(t, int32(4), hits.Load(), "expected the full retry budget to be spent")
			} else {
				require.Equal(t, int32(1), hits.Load(), "a permanent error must not be retried")
			}
		})
	}
}

// sendTransaction must never be retried, even against a retryable status, or a
// transaction the endpoint already accepted can be resubmitted.
func TestNewWithRetries_DoesNotRetrySendTransaction(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<html>Service Unavailable</html>`))
	}))
	defer srv.Close()

	cl := NewWithRetries(srv.URL, &jsonrpc.RetryOptions{
		MaxAttempts: 4,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  2 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := cl.SendEncodedTransaction(ctx, "AQAB")
	require.Error(t, err)
	require.Equal(t, int32(1), hits.Load(), "sendTransaction was retried")
}

func TestNewHTTPTransport_ConfigMatchesConstants(t *testing.T) {
	t.Parallel()

	tr := newHTTPTransport(defaultMaxIdleConnsPerHost)
	require.NotNil(t, tr)

	require.Equal(t, defaultTimeout, tr.IdleConnTimeout)
	require.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxConnsPerHost)
	require.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
	require.True(t, tr.ForceAttemptHTTP2)
	require.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)

	// Dialer settings are embedded in DialContext; we can at least assert it's set.
	require.NotNil(t, tr.DialContext)
}

// The per-request timeout and connection pool must reach the underlying client;
// sdk/shreds and the internet-latency collector depend on a bound well under the
// ~56s blockhash validity window so a queued send cannot outlive its blockhash.
func TestNew_OptionsReachHTTPClient(t *testing.T) {
	t.Parallel()

	hc := newHTTP(15*time.Second, 128)
	require.Equal(t, 15*time.Second, hc.Timeout)

	tr := newHTTPTransport(128)
	require.Equal(t, 128, tr.MaxConnsPerHost)
	require.Equal(t, 128, tr.MaxIdleConnsPerHost)
	require.Equal(t, 128, tr.MaxIdleConns)
}

func TestNew_ZeroOptionsUsesDefaults(t *testing.T) {
	t.Parallel()

	hc := newHTTP(0, 0)
	require.Equal(t, defaultTimeout, hc.Timeout)
	require.NotNil(t, New("http://127.0.0.1:1", Options{}))
}
