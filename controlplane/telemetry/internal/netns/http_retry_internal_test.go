package netns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/require"
)

// The netns path is how the telemetry agent reads the ledger on devices — the
// widest fleet of readers. It builds its own transport, so it cannot use the shared
// retrying constructor; this pins that it still gets the retry layer. Internal test:
// it exercises the JSON-RPC seam directly, since the namespaced dialer needs root.
func TestNetNS_RetryingJSONRPCClient_RetriesServiceUnavailable(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":503,"message":"Service unavailable"},"id":"1"}`))
	}))
	defer srv.Close()

	cl := solanarpc.NewWithCustomRPCClient(newRetryingJSONRPCClient(srv.URL, srv.Client()))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := cl.GetVersion(ctx)
	require.Error(t, err, "retry exhaustion must surface as an error")
	require.Greater(t, hits.Load(), int32(1), "netns JSON-RPC client did not retry a 503")
}
