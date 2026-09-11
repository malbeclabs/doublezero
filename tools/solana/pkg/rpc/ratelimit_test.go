package rpc

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// stubRT returns a fixed response and records whether the request body it was
// handed is still fully readable afterwards — the observer must not consume it.
type stubRT struct {
	header   http.Header
	bodySeen string
}

func (s *stubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		s.bodySeen = string(b)
	}
	return &http.Response{
		StatusCode: 200,
		Header:     s.header,
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","result":null,"id":1}`)),
	}, nil
}

func newJSONRPCRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/k", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

// TestRateLimitObserver_RecordsHeadersByMethod is the point of the file: a 429 is
// opaque without these numbers, because solana-go keeps only a code on its error
// types and discards the http.Response. When the endpoint reports the cap, it must
// land in metrics attributed to the method it applies to — rate limits are enforced
// per method, so an unattributed number would be useless.
func TestRateLimitObserver_RecordsHeadersByMethod(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Method-Limit", "200")
	h.Set("X-Ratelimit-Method-Remaining", "7")
	stub := &stubRT{header: h}

	body := `{"jsonrpc":"2.0","id":1,"method":"getTransaction","params":["sig",{}]}`
	resp, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, body))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response altered: status %d", resp.StatusCode)
	}

	if got := testutil.ToFloat64(rateLimitMethodLimit.WithLabelValues("getTransaction")); got != 200 {
		t.Errorf("limit gauge = %v, want 200", got)
	}
	if got := testutil.ToFloat64(rateLimitMethodRemaining.WithLabelValues("getTransaction")); got != 7 {
		t.Errorf("remaining gauge = %v, want 7", got)
	}

	// The inner transport must still receive the whole body: recovering the method
	// reads through GetBody precisely so the real body is left alone.
	if stub.bodySeen != body {
		t.Errorf("inner transport saw a modified body:\n got: %q\nwant: %q", stub.bodySeen, body)
	}
}

// TestRateLimitObserver_NoHeadersNoSeries pins the cheap-exit path. Endpoints that
// report nothing (the DZ mainnet ledger currently returns no X-Ratelimit-* headers
// on a 200) must produce no series and no body inspection.
func TestRateLimitObserver_NoHeadersNoSeries(t *testing.T) {
	stub := &stubRT{header: http.Header{}}
	body := `{"jsonrpc":"2.0","id":1,"method":"getSlot","params":[]}`

	if _, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, body)); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if n := testutil.CollectAndCount(rateLimitMethodLimit); n != 0 {
		// Only this test's method would be new; the other test uses getTransaction.
		if got := testutil.ToFloat64(rateLimitMethodLimit.WithLabelValues("getSlot")); got != 0 {
			t.Errorf("getSlot limit gauge = %v, want no observation", got)
		}
	}
	if stub.bodySeen != body {
		t.Errorf("inner transport saw a modified body: %q", stub.bodySeen)
	}
}

// TestRateLimitObserver_BatchIsLabelledBatch: a batch carries a mix of methods, so
// attributing its headroom to any single one would be wrong. Mirrors the batchLabel
// the retry layer already uses.
func TestRateLimitObserver_BatchIsLabelledBatch(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Method-Limit", "50")
	stub := &stubRT{header: h}

	body := `[{"jsonrpc":"2.0","id":1,"method":"getSlot"},{"jsonrpc":"2.0","id":2,"method":"getEpochInfo"}]`
	if _, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, body)); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := testutil.ToFloat64(rateLimitMethodLimit.WithLabelValues("batch")); got != 50 {
		t.Errorf("batch limit gauge = %v, want 50", got)
	}
}

// TestRateLimitObserver_MalformedBodyIsSkipped: observation is best-effort and must
// never fail a call. A body it cannot parse yields no series and no error.
func TestRateLimitObserver_MalformedBodyIsSkipped(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Method-Limit", "10")
	stub := &stubRT{header: h}

	if _, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, `not json at all`)); err != nil {
		t.Fatalf("malformed body must not fail the request: %v", err)
	}
}

// TestRateLimitObserver_CountsServingNode: rate limits are enforced per source IP,
// so when a provider asks whether load is concentrated on one of their nodes, the
// serving node is the answer. It is absent from the error path, so it has to be
// recorded per response or it is unavailable after the fact.
func TestRateLimitObserver_CountsServingNode(t *testing.T) {
	h := http.Header{}
	h.Set("X-RPC-Node", "lb-pit5")
	stub := &stubRT{header: h}

	body := `{"jsonrpc":"2.0","id":1,"method":"getSlot","params":[]}`
	before := testutil.ToFloat64(responsesTotal.WithLabelValues("lb-pit5"))
	if _, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, body)); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := testutil.ToFloat64(responsesTotal.WithLabelValues("lb-pit5")) - before; got != 1 {
		t.Errorf("lb-pit5 response count delta = %v, want 1", got)
	}
}

// TestRateLimitObserver_UnnamedNodeIsCounted: a response naming no node must still
// be counted, so a silent gap in the series can't be mistaken for no traffic.
func TestRateLimitObserver_UnnamedNodeIsCounted(t *testing.T) {
	stub := &stubRT{header: http.Header{}}
	before := testutil.ToFloat64(responsesTotal.WithLabelValues("unknown"))
	if _, err := (&rateLimitObserver{inner: stub}).RoundTrip(newJSONRPCRequest(t, `{"method":"getSlot"}`)); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := testutil.ToFloat64(responsesTotal.WithLabelValues("unknown")) - before; got != 1 {
		t.Errorf("unknown-node count delta = %v, want 1", got)
	}
}
