package rpc

import (
	"context"
	"net"
	"net/http"
	"time"

	solrpc "github.com/gagliardetto/solana-go/rpc"
	soljsonrpc "github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/klauspost/compress/gzhttp"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/jsonrpc"
)

const (
	defaultMaxIdleConnsPerHost = 9
	defaultKeepAlive           = 180 * time.Second

	// defaultRequestTimeout bounds a single HTTP request, so it also bounds a single
	// retry attempt. It must be short enough that an exhausted retry budget still
	// fits inside a caller's poll interval: retry multiplies a hang by MaxAttempts,
	// so a long per-attempt bound amplifies an endpoint stall instead of containing
	// it. At the package retry defaults (4 attempts, ~3s of total jittered backoff)
	// 10s puts the worst case at ~43s.
	//
	// 10s is chosen against the heaviest call we actually make, an unfiltered
	// getProgramAccounts over the serviceability program. Measured against mainnet
	// (2.9MB raw, ~1.0MB gzipped) that completes in ~0.4s wall with ~0.11s to first
	// byte, so 10s leaves more than an order of magnitude of headroom and will not
	// manufacture failures on a full program scan from a poorly connected host.
	//
	// It also fits the callers. state-ingest is the case that needs this default
	// most: it refreshes every 60s and passes its root context straight to
	// GetProgramData with no call-site bound, so nothing else stands between it and
	// a wedged refresh goroutine; ~43s worst case stays inside its tick. The controller
	// (cacheFetchTimeout) and doublezerod (onchain.DefaultRPCTimeout) already cap a
	// whole fetch at 30s, so their own bound still wins — but a 10s attempt means
	// they get real retries inside it rather than one attempt that eats the budget.
	// 10s also matches dialTimeout and TLSHandshakeTimeout below, keeping connect
	// and request bounds on one number.
	//
	// Callers with a different tradeoff override it: sdk/shreds and the
	// internet-latency-collector pass 15s, buying patience for transaction sends
	// while staying well inside the ~56s blockhash validity window.
	defaultRequestTimeout = 10 * time.Second

	// defaultIdleConnTimeout is how long an unused pooled connection is kept alive.
	// Unrelated to the per-request bound: reconnecting costs a TCP and TLS handshake
	// on every poll tick, so idle connections are held far longer than any single
	// request is allowed to run.
	defaultIdleConnTimeout = 5 * time.Minute

	// dialTimeout bounds TCP connect only. It has to be far shorter than the
	// per-request timeout: a dial that can hang for minutes makes the retry budget
	// meaningless, since a single attempt would outlast the caller's poll interval.
	// Matches the TLS handshake bound below.
	dialTimeout = 10 * time.Second
)

// Options tunes the client returned by New. The zero value is valid and yields the
// same client as NewWithRetries(endpoint, nil).
type Options struct {
	// Headers are sent on every request.
	Headers map[string]string

	// RequestTimeout bounds each individual HTTP request, and therefore each retry
	// attempt. Defaults to defaultRequestTimeout.
	RequestTimeout time.Duration

	// MaxConnsPerHost caps concurrent and idle connections to the endpoint.
	// Defaults to 9.
	MaxConnsPerHost int

	// Retry overrides the retry budget and classifier. Defaults apply when nil.
	Retry *jsonrpc.RetryOptions

	// OnDial, when set, is called after each new connection to the endpoint is
	// established, with the dial target and the resolved remote address. It exists so a
	// caller can record which endpoint behind a hostname it is actually talking to,
	// which nothing else in the stack reports. It runs on the dial path, so it must not
	// block, and it is not called when the dial fails: the error reaches the caller.
	OnDial func(addr, remote string)
}

// New creates a Solana JSON-RPC client that retries transient failures.
//
// This is the one retrying RPC constructor for Go code in this repo. Retry
// classification and backoff live in the jsonrpc package, so every caller agrees
// on what is retryable; non-idempotent methods (sendTransaction, requestAirdrop)
// are never retried regardless of the options passed.
func New(endpoint string, o Options) *solrpc.Client {
	opts := &soljsonrpc.RPCClientOpts{
		HTTPClient:    newHTTP(o.RequestTimeout, o.MaxConnsPerHost, o.OnDial),
		CustomHeaders: o.Headers,
	}
	soljsonrpcClient := soljsonrpc.NewClientWithOpts(endpoint, opts)
	return solrpc.NewWithCustomRPCClient(jsonrpc.WithRetry(soljsonrpcClient, o.Retry))
}

// NewWithRetries creates a new Solana JSON RPC client with retrying request behavior.
func NewWithRetries(rpcEndpoint string, retryOpt *jsonrpc.RetryOptions) *solrpc.Client {
	return New(rpcEndpoint, Options{Retry: retryOpt})
}

// NewWithHeadersAndRetries creates a new Solana JSON RPC client with custom headers
// and retrying request behavior.
func NewWithHeadersAndRetries(rpcEndpoint string, headers map[string]string, retryOpt *jsonrpc.RetryOptions) *solrpc.Client {
	return New(rpcEndpoint, Options{Headers: headers, Retry: retryOpt})
}

// newHTTP returns a new Client from the provided config. Zero values fall back to
// the package defaults. Client is safe for concurrent use by multiple goroutines.
func newHTTP(requestTimeout time.Duration, maxConns int, onDial func(addr, remote string)) *http.Client {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}

	return &http.Client{
		Timeout: requestTimeout,
		// The rate-limit observer wraps the outermost transport so it sees the final
		// response headers. It is passive: nothing is altered, and an endpoint that
		// reports no rate-limit headers costs nothing (see ratelimit.go).
		Transport: &rateLimitObserver{inner: gzhttp.Transport(newHTTPTransport(maxConns, onDial))},
	}
}

func newHTTPTransport(maxConns int, onDial func(addr, remote string)) *http.Transport {
	if maxConns <= 0 {
		maxConns = defaultMaxIdleConnsPerHost
	}

	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: defaultKeepAlive,
		DualStack: true,
	}

	dialContext := dialer.DialContext
	if onDial != nil {
		dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			onDial(addr, conn.RemoteAddr().String())
			return conn, nil
		}
	}

	return &http.Transport{
		IdleConnTimeout:     defaultIdleConnTimeout,
		MaxConnsPerHost:     maxConns,
		MaxIdleConns:        maxConns,
		MaxIdleConnsPerHost: maxConns,
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}
