package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/solana/pkg/jsonrpc"
	"github.com/stretchr/testify/require"
)

// refusalShape is how a provider delivers a rate limit. Both shapes are real: our
// mainnet ledger's sustained refusals arrived as HTTP 200 with the limit inside the
// JSON-RPC envelope, which no status-code check would have matched.
type refusalShape struct {
	name  string
	write func(w http.ResponseWriter)
}

var refusalShapes = []refusalShape{
	{
		name: "http 429 status",
		write: func(w http.ResponseWriter) {
			http.Error(w, "Too many requests from your IP", http.StatusTooManyRequests)
		},
	},
	{
		name: "jsonrpc 429 inside a 200",
		write: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":429,`+
				`"message":"Too many requests for a specific RPC call"}}`)
		},
	},
}

// refusingServer refuses the first call with shape, then serves getVersion. It records
// how many requests arrived and always sets Retry-After, as our provider confirms it
// does on every refusal.
func refusingServer(t *testing.T, shape refusalShape, retryAfter string, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			shape.write(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"solana-core":"2.0.0"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTools_Solana_RPC_RetryAfter_IsHonoredEndToEnd is the regression, and it has to
// run through the real constructor: the number is on the response, and solana-go
// discards the response before the error reaches the retry loop. Only the transport
// the constructor installs can carry it across that gap, so a unit test of the retry
// helper would pass with the wiring absent.
//
// The default backoff is ~500ms on the second attempt while the provider fronting our
// mainnet ledger enforces limits over a rolling 10s window. Retrying inside that
// window is a guaranteed second refusal — four attempts spent to be told the same
// thing, against an endpoint already shedding load.
func TestTools_Solana_RPC_RetryAfter_IsHonoredEndToEnd(t *testing.T) {
	t.Parallel()

	for _, shape := range refusalShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int64
			srv := refusingServer(t, shape, "1", &calls)

			client := New(srv.URL, Options{Retry: &jsonrpc.RetryOptions{
				MaxAttempts:   4,
				BaseBackoff:   time.Millisecond,
				MaxBackoff:    time.Millisecond,
				MaxRetryAfter: 5 * time.Second,
			}})
			defer client.Close()

			start := time.Now()
			got, err := client.GetVersion(context.Background())
			elapsed := time.Since(start)

			require.NoError(t, err)
			require.Equal(t, "2.0.0", got.SolanaCore)
			require.EqualValues(t, 2, calls.Load())
			require.GreaterOrEqual(t, elapsed, time.Second,
				"Retry-After: 1 must hold the retry for a second; the 1ms backoff configured "+
					"here would have retried inside the window that just refused us")
		})
	}
}

// TestTools_Solana_RPC_RetryAfter_OffKeepsTheOldBackoff is the other half of the
// regression. It proves the timing above comes from the header rather than from
// anything else in the stack: same server, same refusal, allowance turned off, and the
// retry lands immediately.
func TestTools_Solana_RPC_RetryAfter_OffKeepsTheOldBackoff(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := refusingServer(t, refusalShapes[1], "1", &calls)

	client := New(srv.URL, Options{Retry: &jsonrpc.RetryOptions{
		MaxAttempts:   4,
		BaseBackoff:   time.Millisecond,
		MaxBackoff:    time.Millisecond,
		MaxRetryAfter: -1, // off
	}})
	defer client.Close()

	start := time.Now()
	_, err := client.GetVersion(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Less(t, elapsed, time.Second,
		"with the allowance off the wait must come from BaseBackoff alone")
}

// TestTools_Solana_RPC_RetryAfter_TooLongEndsTheCall: an endpoint asking for longer
// than one call may be held gets no further attempts. Retrying sooner than it asked is
// a refusal already paid for, and the caller's own next poll arrives after the window
// anyway. The caller must see the rate limit, not a deadline.
func TestTools_Solana_RPC_RetryAfter_TooLongEndsTheCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := refusingServer(t, refusalShapes[1], "600", &calls)

	client := New(srv.URL, Options{Retry: &jsonrpc.RetryOptions{
		MaxAttempts:   4,
		BaseBackoff:   time.Millisecond,
		MaxBackoff:    time.Millisecond,
		MaxRetryAfter: 5 * time.Second,
	}})
	defer client.Close()

	start := time.Now()
	_, err := client.GetVersion(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorContains(t, err, "429", "the rate limit itself must reach the caller")
	require.EqualValues(t, 1, calls.Load(), "no attempt may follow a wait we cannot afford")
	require.Less(t, elapsed, time.Second, "and it must not be held while deciding that")
}

// TestTools_Solana_RPC_RetryAfter_AbsentHeaderStillRetries: most endpoints send no
// Retry-After, and the existing backoff has to keep working for them untouched.
func TestTools_Solana_RPC_RetryAfter_AbsentHeaderStillRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := refusingServer(t, refusalShapes[0], "", &calls)

	client := New(srv.URL, Options{Retry: &jsonrpc.RetryOptions{
		MaxAttempts: 4,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  time.Millisecond,
	}})
	defer client.Close()

	got, err := client.GetVersion(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2.0.0", got.SolanaCore)
	require.EqualValues(t, 2, calls.Load())
}
