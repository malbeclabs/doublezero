package qa

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeJSONRPCClient is a programmable jsonrpc.RPCClient for testing failover.
type fakeJSONRPCClient struct {
	name string
	// callForInto is invoked for CallForInto; if nil, returns errBoom.
	callForInto func(out any, method string, params []any) error
	mu          sync.Mutex
	calls       int
}

func (f *fakeJSONRPCClient) CallForInto(ctx context.Context, out any, method string, params []any) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.callForInto != nil {
		return f.callForInto(out, method, params)
	}
	return errors.New("boom")
}

func (f *fakeJSONRPCClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Unused methods needed to satisfy jsonrpc.RPCClient.
func (f *fakeJSONRPCClient) Call(ctx context.Context, method string, params ...any) (*jsonrpc.RPCResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeJSONRPCClient) CallRaw(ctx context.Context, request *jsonrpc.RPCRequest) (*jsonrpc.RPCResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeJSONRPCClient) CallFor(ctx context.Context, out any, method string, params ...any) error {
	return f.CallForInto(ctx, out, method, params)
}
func (f *fakeJSONRPCClient) CallWithCallback(ctx context.Context, method string, params []any, callback func(*http.Request, *http.Response) error) error {
	return errors.New("not implemented")
}
func (f *fakeJSONRPCClient) CallBatch(ctx context.Context, requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeJSONRPCClient) CallBatchRaw(ctx context.Context, requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeJSONRPCClient) Close() error { return nil }

func newTestPool(clients ...*fakeJSONRPCClient) *solanaRPCPool {
	urls := make([]string, len(clients))
	jc := make([]jsonrpc.RPCClient, len(clients))
	endpointRPC := make([]*rpc.Client, len(clients))
	for i, c := range clients {
		urls[i] = "http://endpoint-" + c.name
		jc[i] = c
		endpointRPC[i] = rpc.NewWithCustomRPCClient(c)
	}
	return &solanaRPCPool{
		log:         testLogger(),
		urls:        urls,
		budget:      rpcBudget{timeout: time.Second, maxSlotLag: defaultRPCMaxSlotLag},
		clients:     jc,
		endpointRPC: endpointRPC,
	}
}

// slotResponder returns a callForInto func that answers getSlot with the given
// slot and fails everything else.
func slotResponder(slot uint64, err error) func(any, string, []any) error {
	return func(out any, method string, params []any) error {
		if method != "getSlot" {
			return errors.New("unexpected method " + method)
		}
		if err != nil {
			return err
		}
		if p, ok := out.(*uint64); ok {
			*p = slot
		}
		return nil
	}
}

func TestSelectHealthiestEndpoint_FailsOverLaggingNode(t *testing.T) {
	// endpoint-0 is far behind, endpoint-1 is current.
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: slotResponder(1000, nil)}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: slotResponder(1000+defaultRPCMaxSlotLag+1, nil)}
	pool := newTestPool(ep0, ep1)

	pool.SelectHealthiestEndpoint(context.Background())
	if pool.CurrentURL() != "http://endpoint-1" {
		t.Fatalf("expected failover to fresher endpoint-1, got %s", pool.CurrentURL())
	}
}

func TestSelectHealthiestEndpoint_WithinThresholdStays(t *testing.T) {
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: slotResponder(1000, nil)}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: slotResponder(1000+defaultRPCMaxSlotLag-1, nil)}
	pool := newTestPool(ep0, ep1)

	pool.SelectHealthiestEndpoint(context.Background())
	if pool.CurrentURL() != "http://endpoint-0" {
		t.Fatalf("expected to stay on endpoint-0 within lag threshold, got %s", pool.CurrentURL())
	}
}

func TestSelectHealthiestEndpoint_AllProbesFailKeepsCurrent(t *testing.T) {
	probeErr := errors.New("probe failed")
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: slotResponder(0, probeErr)}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: slotResponder(0, probeErr)}
	pool := newTestPool(ep0, ep1)

	pool.SelectHealthiestEndpoint(context.Background())
	if pool.CurrentURL() != "http://endpoint-0" {
		t.Fatalf("expected to keep current endpoint when all probes fail, got %s", pool.CurrentURL())
	}
}

func TestFailover_AdvancesOnRetryableError(t *testing.T) {
	timeoutErr := &jsonrpc.HTTPError{Code: 503}
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: func(any, string, []any) error { return timeoutErr }}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: func(any, string, []any) error { return nil }}
	pool := newTestPool(ep0, ep1)

	if err := pool.CallForInto(context.Background(), nil, "getSlot", nil); err != nil {
		t.Fatalf("expected success after failover, got %v", err)
	}
	if pool.CurrentURL() != "http://endpoint-1" {
		t.Fatalf("expected sticky failover to endpoint-1, got %s", pool.CurrentURL())
	}
	// A subsequent call should stay on endpoint-1 (sticky), not retry endpoint-0.
	_ = pool.CallForInto(context.Background(), nil, "getSlot", nil)
	if ep0.callCount() != 1 {
		t.Fatalf("expected endpoint-0 called once (sticky), got %d", ep0.callCount())
	}
}

func TestFailover_ExhaustionReturnsLastError(t *testing.T) {
	httpErr := &jsonrpc.HTTPError{Code: 500}
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: func(any, string, []any) error { return httpErr }}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: func(any, string, []any) error { return httpErr }}
	pool := newTestPool(ep0, ep1)

	err := pool.CallForInto(context.Background(), nil, "getSlot", nil)
	if err == nil {
		t.Fatal("expected error after all endpoints exhausted")
	}
	if ep0.callCount() != 1 || ep1.callCount() != 1 {
		t.Fatalf("expected each endpoint tried once, got ep0=%d ep1=%d", ep0.callCount(), ep1.callCount())
	}
}

func TestFailover_NonRetryableDoesNotAdvance(t *testing.T) {
	businessErr := errors.New("invalid params")
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: func(any, string, []any) error { return businessErr }}
	ep1 := &fakeJSONRPCClient{name: "1", callForInto: func(any, string, []any) error { return nil }}
	pool := newTestPool(ep0, ep1)

	err := pool.CallForInto(context.Background(), nil, "getSlot", nil)
	if err == nil {
		t.Fatal("expected non-retryable error to surface")
	}
	if pool.CurrentURL() != "http://endpoint-0" {
		t.Fatalf("expected to stay on endpoint-0 for non-retryable error, got %s", pool.CurrentURL())
	}
	if ep1.callCount() != 0 {
		t.Fatalf("expected endpoint-1 untouched, got %d", ep1.callCount())
	}
}

func TestFailover_SingleEndpointPassthrough(t *testing.T) {
	httpErr := &jsonrpc.HTTPError{Code: 503}
	ep0 := &fakeJSONRPCClient{name: "0", callForInto: func(any, string, []any) error { return httpErr }}
	pool := newTestPool(ep0)

	err := pool.CallForInto(context.Background(), nil, "getSlot", nil)
	if err == nil {
		t.Fatal("expected error with single endpoint")
	}
	if ep0.callCount() != 1 {
		t.Fatalf("expected single attempt, got %d", ep0.callCount())
	}
	if pool.CurrentURL() != "http://endpoint-0" {
		t.Fatalf("single endpoint should not change, got %s", pool.CurrentURL())
	}
}

func TestResolveSolanaRPCEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		primary       string
		publicDefault string
		fallbackEnv   string
		want          []string
	}{
		{
			name:          "public default appended when fallback unset",
			primary:       "https://private",
			publicDefault: "https://public",
			want:          []string{"https://private", "https://public"},
		},
		{
			name:          "no public default on testnet/devnet",
			primary:       "https://dz-ledger",
			publicDefault: "",
			want:          []string{"https://dz-ledger"},
		},
		{
			name:          "explicit fallbacks override default and dedupe",
			primary:       "https://private",
			publicDefault: "https://public",
			fallbackEnv:   "https://a, https://b ,https://a",
			want:          []string{"https://private", "https://a", "https://b"},
		},
		{
			name:          "primary equal to public default collapses",
			primary:       "https://public",
			publicDefault: "https://public",
			want:          []string{"https://public"},
		},
		{
			name:          "blank fallback entries dropped",
			primary:       "https://private",
			publicDefault: "",
			fallbackEnv:   ",  ,https://b,",
			want:          []string{"https://private", "https://b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fallbackEnv != "" {
				t.Setenv("SOLANA_RPC_FALLBACK_URLS", tt.fallbackEnv)
			} else {
				t.Setenv("SOLANA_RPC_FALLBACK_URLS", "")
			}
			got := resolveSolanaRPCEndpoints(tt.primary, tt.publicDefault)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestIsRetryableRPCErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"http 429", &jsonrpc.HTTPError{Code: 429}, true},
		{"http 503", &jsonrpc.HTTPError{Code: 503}, true},
		{"http 400", &jsonrpc.HTTPError{Code: 400}, false},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"eof", errors.New("unexpected EOF"), true},
		{"business error", errors.New("invalid params: bad pubkey"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableRPCErr(tt.err); got != tt.want {
				t.Fatalf("isRetryableRPCErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWriteFailover_RetriesThenSucceeds(t *testing.T) {
	pool := newTestPool(
		&fakeJSONRPCClient{name: "0"},
		&fakeJSONRPCClient{name: "1"},
	)
	c := &Client{log: testLogger(), solanaRPC: pool, SolanaRPCURL: "http://endpoint-0"}

	var seenURLs []string
	err := c.withWriteFailover(func(url string) error {
		seenURLs = append(seenURLs, url)
		if url == "http://endpoint-0" {
			return errors.New("rpc unreachable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after failover, got %v", err)
	}
	if len(seenURLs) != 2 || seenURLs[0] != "http://endpoint-0" || seenURLs[1] != "http://endpoint-1" {
		t.Fatalf("unexpected URL sequence: %v", seenURLs)
	}
}

func TestWriteFailover_NoPoolRunsOnce(t *testing.T) {
	c := &Client{log: testLogger(), SolanaRPCURL: "http://only"}
	calls := 0
	err := c.withWriteFailover(func(url string) error {
		calls++
		if url != "http://only" {
			t.Fatalf("expected static URL, got %s", url)
		}
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected single attempt without pool, got %d", calls)
	}
}
