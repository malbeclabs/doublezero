package rpc

import (
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
	defaultTimeout             = 5 * time.Minute
	defaultKeepAlive           = 180 * time.Second

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

	// RequestTimeout bounds each individual HTTP request. Defaults to 5 minutes.
	RequestTimeout time.Duration

	// MaxConnsPerHost caps concurrent and idle connections to the endpoint.
	// Defaults to 9.
	MaxConnsPerHost int

	// Retry overrides the retry budget and classifier. Defaults apply when nil.
	Retry *jsonrpc.RetryOptions
}

// New creates a Solana JSON-RPC client that retries transient failures.
//
// This is the one retrying RPC constructor for Go code in this repo. Retry
// classification and backoff live in the jsonrpc package, so every caller agrees
// on what is retryable; non-idempotent methods (sendTransaction, requestAirdrop)
// are never retried regardless of the options passed.
func New(endpoint string, o Options) *solrpc.Client {
	opts := &soljsonrpc.RPCClientOpts{
		HTTPClient:    newHTTP(o.RequestTimeout, o.MaxConnsPerHost),
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
func newHTTP(requestTimeout time.Duration, maxConns int) *http.Client {
	if requestTimeout <= 0 {
		requestTimeout = defaultTimeout
	}

	return &http.Client{
		Timeout:   requestTimeout,
		Transport: gzhttp.Transport(newHTTPTransport(maxConns)),
	}
}

func newHTTPTransport(maxConns int) *http.Transport {
	if maxConns <= 0 {
		maxConns = defaultMaxIdleConnsPerHost
	}

	return &http.Transport{
		IdleConnTimeout:     defaultTimeout,
		MaxConnsPerHost:     maxConns,
		MaxIdleConns:        maxConns,
		MaxIdleConnsPerHost: maxConns,
		Proxy:               http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: defaultKeepAlive,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}
