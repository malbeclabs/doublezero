package serviceability

import (
	"io"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

const defaultMaxRetries = 5

// retryHTTPClient wraps an http.Client and retries on 429 Too Many Requests.
type retryHTTPClient struct {
	inner      *http.Client
	maxRetries int
}

func (c *retryHTTPClient) Do(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.inner.Do(req)
		if err != nil {
			return resp, err
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= c.maxRetries {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		backoff := time.Duration(attempt+1) * 2 * time.Second
		time.Sleep(backoff)
	}
}

func (c *retryHTTPClient) CloseIdleConnections() {
	c.inner.CloseIdleConnections()
}

// NewRPCClient creates a Solana RPC client with automatic retry on 429 responses.
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
