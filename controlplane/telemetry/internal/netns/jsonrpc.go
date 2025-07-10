package netns

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/klauspost/compress/gzhttp"
)

type JSONRPCClientOptions struct {
	DialTimeout   time.Duration
	DialKeepAlive time.Duration
	HTTPTimeout   time.Duration
}

var (
	defaultJSONRPCClientOptions = &JSONRPCClientOptions{
		DialTimeout:   5 * time.Second,
		DialKeepAlive: 5 * time.Second,
		HTTPTimeout:   10 * time.Second,
	}
)

// NewNamespaceSafeJSONRPCClient creates a JSONRPC client that is safe to use in a
// network namespace.
func NewNamespaceSafeJSONRPCClient(url string, namespace string, opts *JSONRPCClientOptions) (jsonrpc.RPCClient, error) {
	if opts == nil {
		opts = defaultJSONRPCClientOptions
	}

	transport := &SingleThreadTransport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var conn net.Conn
			err := RunInNamespace(namespace, func() error {
				var dialErr error
				conn, dialErr = (&net.Dialer{
					Timeout:       opts.DialTimeout,
					KeepAlive:     opts.DialKeepAlive,
					DualStack:     false,
					FallbackDelay: -1,
				}).DialContext(ctx, network, address)
				return dialErr
			})
			return conn, err
		},
	}

	httpClient := &http.Client{
		Transport: gzhttp.Transport(transport),
		Timeout:   opts.HTTPTimeout,
	}

	client := jsonrpc.NewClientWithOpts(url, &jsonrpc.RPCClientOpts{
		HTTPClient: httpClient,
	})

	return client, nil
}

type SingleThreadTransport struct {
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (t *SingleThreadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, errors.New("request body required")
	}

	addr := canonicalAddr(req.URL)

	conn, err := t.DialContext(req.Context(), "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Write the HTTP request manually
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	// Parse response
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Hook conn into resp.Body so it's closed properly
	resp.Body = &readCloserWithConn{ReadCloser: resp.Body, conn: conn}
	return resp, nil
}

type readCloserWithConn struct {
	io.ReadCloser
	conn net.Conn
}

func (r *readCloserWithConn) Close() error {
	r.ReadCloser.Close()
	return r.conn.Close()
}

func canonicalAddr(u *url.URL) string {
	if strings.IndexByte(u.Host, ':') == -1 {
		if u.Scheme == "https" {
			return u.Host + ":443"
		}
		return u.Host + ":80"
	}
	return u.Host
}
