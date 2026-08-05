package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

		// Decode the id as RawMessage so it can be echoed back with its original
		// JSON type. Decoding into `any` yields float64 for a numeric id, and
		// re-encoding that is how a request id silently changes type on the wire.
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
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
			ID json.RawMessage `json:"id"`
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
			body:        `{"jsonrpc":"2.0","error":{"code":503,"message":"Service unavailable"},"id":%s}`,
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
			body:        `{"jsonrpc":"2.0","error":{"code":-32429,"message":"Too many requests"},"id":%s}`,
			wantRetried: true,
		},
		{
			// A request the endpoint will reject the same way every time must not
			// be retried — retrying only adds load to a degraded endpoint.
			name:        "400 invalid params",
			status:      http.StatusBadRequest,
			body:        `{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid param: not a Pubkey"},"id":%s}`,
			wantRetried: false,
		},
		{
			// -32003 is a deterministic signature-verification rejection, not a busy
			// signal. It reaches here on an idempotent method (getProgramAccounts and
			// simulateTransaction both qualify), so classifying it as retryable would
			// spend the entire budget re-asking a question already answered.
			name:        "signature verification failure is not retried",
			status:      http.StatusBadRequest,
			body:        `{"jsonrpc":"2.0","error":{"code":-32003,"message":"Transaction signature verification failure"},"id":%s}`,
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
				// RawMessage echoes the id back byte-for-byte, preserving its JSON
				// type. Decoding into `any` gives float64 and re-encoding it via
				// fmt turns a numeric id into a string one — non-conformant to
				// JSON-RPC 2.0, and a live trap for anyone extending these cases to
				// CallBatch, which correlates responses to requests by id.
				var req struct {
					ID json.RawMessage `json:"id"`
				}
				require.NoError(t, json.Unmarshal(body, &req))

				out := tc.body
				if strings.Contains(out, "%s") {
					out = fmt.Sprintf(out, req.ID)
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

	tr := newHTTPTransport(defaultMaxIdleConnsPerHost, nil)
	require.NotNil(t, tr)

	require.Equal(t, defaultIdleConnTimeout, tr.IdleConnTimeout)
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

	hc := newHTTP(15*time.Second, 128, nil)
	require.Equal(t, 15*time.Second, hc.Timeout)

	tr := newHTTPTransport(128, nil)
	require.Equal(t, 128, tr.MaxConnsPerHost)
	require.Equal(t, 128, tr.MaxIdleConnsPerHost)
	require.Equal(t, 128, tr.MaxIdleConns)
}

func TestNew_ZeroOptionsUsesDefaults(t *testing.T) {
	t.Parallel()

	hc := newHTTP(0, 0, nil)
	require.Equal(t, defaultRequestTimeout, hc.Timeout)
	require.NotNil(t, New("http://127.0.0.1:1", Options{}))
}

// Pins the default per-attempt bound. Almost every ledger reader in the repo
// (doublezerod, controller, monitor, funder, device-health-oracle, telemetry,
// data-api, cdiff, state-ingest, flow-enricher, global-monitor, the Go SDK)
// constructs its client with nil/zero options and inherits this value, so raising
// it silently multiplies a hung endpoint by the retry budget on all of them.
//
// The upper bound is what keeps retry containing a stall instead of amplifying it:
// an exhausted budget must still fit inside the cadence of a caller that applies no
// call-site deadline of its own. state-ingest is the reference case — it refreshes
// every 60s and hands its root context straight to GetProgramData, so nothing but
// this default bounds a refresh. The lower bound is the heaviest legitimate call,
// an unfiltered getProgramAccounts over the serviceability program (~2.9MB raw /
// ~1.0MB gzipped on mainnet, ~0.4s observed); too tight a bound turns a slow scan
// into a manufactured failure.
func TestNew_DefaultRequestTimeoutIsBounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, 10*time.Second, defaultRequestTimeout)

	// Worst case for one logical call: every attempt burns the full timeout, plus
	// the jittered backoff between them.
	const maxAttempts = 4
	worstCase := maxAttempts*defaultRequestTimeout + 3*time.Second
	require.Less(t, worstCase, 60*time.Second,
		"an exhausted retry budget must fit inside state-ingest's 60s refresh tick")

	require.Greater(t, defaultRequestTimeout, 5*time.Second,
		"must leave headroom for a full unfiltered getProgramAccounts scan")

	// Idle connections are pooled far longer than any single request may run;
	// reusing one constant for both would churn a connection every poll tick.
	require.Greater(t, defaultIdleConnTimeout, defaultRequestTimeout)
}

// RequestTimeout must actually abort a slow request, not just be stored on the
// client: callers that send transactions depend on a bound well under the ~56s
// blockhash validity window so a queued request cannot outlive its blockhash.
func TestNew_RequestTimeoutAbortsSlowRequest(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	cl := New(srv.URL, Options{
		RequestTimeout: 200 * time.Millisecond,
		Retry:          &jsonrpc.RetryOptions{MaxAttempts: 1},
	})

	start := time.Now()
	_, err := cl.GetVersion(context.Background())
	require.Error(t, err, "request should have been aborted by RequestTimeout")
	require.Less(t, time.Since(start), 3*time.Second, "RequestTimeout did not bound the request")
}

// OnDial exists so a caller can record which endpoint behind a hostname it is actually
// connected to. Nothing else in the stack reports the resolved address, and during the
// 2026-07-29 ledger outage one bad load balancer address out of five was the cause.
func TestNew_OnDialReportsResolvedRemoteAddress(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"solana-core":"1.0.0","feature-set":0}}`))
	}))
	t.Cleanup(srv.Close)

	type dial struct{ addr, remote string }
	var mu sync.Mutex
	var dials []dial

	cl := New(srv.URL, Options{
		Retry: &jsonrpc.RetryOptions{MaxAttempts: 1},
		OnDial: func(addr, remote string) {
			mu.Lock()
			defer mu.Unlock()
			dials = append(dials, dial{addr: addr, remote: remote})
		},
	})

	_, err := cl.GetVersion(context.Background())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, dials, 1, "one connection, one report")
	require.Equal(t, strings.TrimPrefix(srv.URL, "http://"), dials[0].addr)
	require.Equal(t, strings.TrimPrefix(srv.URL, "http://"), dials[0].remote, "httptest listens on a literal address, so the resolved remote matches it")
}

// A failed dial must not be reported as an established connection.
func TestNew_OnDialNotCalledWhenDialFails(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	cl := New("http://127.0.0.1:1", Options{
		Retry:  &jsonrpc.RetryOptions{MaxAttempts: 1},
		OnDial: func(addr, remote string) { calls.Add(1) },
	})

	_, err := cl.GetVersion(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(0), calls.Load())
}
