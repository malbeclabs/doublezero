package rpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

func TestNewHTTPTransport_ConfigMatchesConstants(t *testing.T) {
	t.Parallel()

	tr := newHTTPTransport()
	require.NotNil(t, tr)

	require.Equal(t, defaultTimeout, tr.IdleConnTimeout)
	require.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxConnsPerHost)
	require.Equal(t, defaultMaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
	require.True(t, tr.ForceAttemptHTTP2)
	require.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)

	// Dialer settings are embedded in DialContext; we can at least assert it's set.
	require.NotNil(t, tr.DialContext)
}
