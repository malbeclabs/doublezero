package shreds

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const (
	defaultMaxRetries = 5

	// defaultRequestTimeout bounds each individual RPC request. http.DefaultClient has no timeout,
	// so against a slow or degraded RPC endpoint a request can block indefinitely — long enough
	// for a transaction's recent blockhash to expire before it is sent, surfacing as
	// BlockhashNotFound. A short timeout fails fast so the caller can retry with a fresh blockhash.
	defaultRequestTimeout = 15 * time.Second

	// defaultMaxConns caps concurrent connections to the RPC host. http.DefaultClient's transport
	// keeps only 2 idle connections per host, which throttles concurrent callers.
	defaultMaxConns = 128
)

// retryHTTPClient wraps an http.Client and retries on transient errors:
// network failures (EOF, connection reset), HTTP 429, and HTTP 5xx.
type retryHTTPClient struct {
	inner      *http.Client
	maxRetries int
}

func (c *retryHTTPClient) Do(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	for attempt := 0; ; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := c.inner.Do(req)

		if err != nil {
			if attempt >= c.maxRetries {
				return nil, err
			}
			backoff := time.Duration(attempt+1) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < c.maxRetries {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			backoff := time.Duration(attempt+1) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}

		return resp, nil
	}
}

func (c *retryHTTPClient) CloseIdleConnections() {
	c.inner.CloseIdleConnections()
}

// newHTTPClient returns an http.Client with a bounded per-request timeout and a connection pool
// sized for concurrent use, instead of the unbounded, lightly-pooled http.DefaultClient.
func newHTTPClient(timeout time.Duration, maxConns int) *http.Client {
	transport := &http.Transport{
		MaxConnsPerHost:     maxConns,
		MaxIdleConns:        maxConns,
		MaxIdleConnsPerHost: maxConns,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// NewRPCClient creates a Solana RPC client with a bounded request timeout and automatic retry on
// transient errors.
func NewRPCClient(url string) *rpc.Client {
	httpClient := &retryHTTPClient{
		inner:      newHTTPClient(defaultRequestTimeout, defaultMaxConns),
		maxRetries: defaultMaxRetries,
	}
	rpcClient := jsonrpc.NewClientWithOpts(url, &jsonrpc.RPCClientOpts{
		HTTPClient: httpClient,
	})
	return rpc.NewWithCustomRPCClient(rpcClient)
}
