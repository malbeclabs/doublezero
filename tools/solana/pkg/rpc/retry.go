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
)

// NewWithRetries creates a new Solana JSON RPC client with retrying request behavior.
func NewWithRetries(rpcEndpoint string, retryOpt *jsonrpc.RetryOptions) *solrpc.Client {
	opts := &soljsonrpc.RPCClientOpts{
		HTTPClient: newHTTP(),
	}
	soljsonrpcClient := soljsonrpc.NewClientWithOpts(rpcEndpoint, opts)
	jsonrpcClient := jsonrpc.WithRetry(soljsonrpcClient, retryOpt)
	return solrpc.NewWithCustomRPCClient(jsonrpcClient)
}

// NewWithHeadersAndRetries creates a new Solana JSON RPC client with custom headers
// and retrying request behavior.
func NewWithHeadersAndRetries(rpcEndpoint string, headers map[string]string, retryOpt *jsonrpc.RetryOptions) *solrpc.Client {
	opts := &soljsonrpc.RPCClientOpts{
		HTTPClient:    newHTTP(),
		CustomHeaders: headers,
	}
	soljsonrpcClient := soljsonrpc.NewClientWithOpts(rpcEndpoint, opts)
	jsonrpcClient := jsonrpc.WithRetry(soljsonrpcClient, retryOpt)
	return solrpc.NewWithCustomRPCClient(jsonrpcClient)
}

// newHTTP returns a new Client from the provided config.
// Client is safe for concurrent use by multiple goroutines.
func newHTTP() *http.Client {
	tr := newHTTPTransport()

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: gzhttp.Transport(tr),
	}
}

func newHTTPTransport() *http.Transport {
	return &http.Transport{
		IdleConnTimeout:     defaultTimeout,
		MaxConnsPerHost:     defaultMaxIdleConnsPerHost,
		MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
		Proxy:               http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultTimeout,
			KeepAlive: defaultKeepAlive,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2: true,
		// MaxIdleConns:          100,
		TLSHandshakeTimeout: 10 * time.Second,
		// ExpectContinueTimeout: 1 * time.Second,
	}
}
