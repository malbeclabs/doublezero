package serviceability

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const defaultMaxRetries = 5

// retryHTTPClient wraps an http.Client and retries on transient errors:
// network failures (EOF, connection reset), HTTP 429, and HTTP 5xx.
type retryHTTPClient struct {
	inner      *http.Client
	maxRetries int
}

func (c *retryHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// Buffer the request body so it can be replayed on retries.
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

		// Retry on network-level errors (EOF, connection reset, etc.).
		if err != nil {
			if attempt >= c.maxRetries {
				return nil, err
			}
			backoff := time.Duration(attempt+1) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}

		// Retry on 429 Too Many Requests and 5xx server errors.
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

// NewRPCClient creates a Solana RPC client with automatic retry on transient errors.
func NewRPCClient(url string) *rpc.Client {
	httpClient := &retryHTTPClient{
		inner:      http.DefaultClient,
		maxRetries: defaultMaxRetries,
	}
	rpcClient := jsonrpc.NewClientWithOpts(url, &jsonrpc.RPCClientOpts{
		HTTPClient: httpClient,
	})
	return rpc.NewWithCustomRPCClient(rpcClient)
}
