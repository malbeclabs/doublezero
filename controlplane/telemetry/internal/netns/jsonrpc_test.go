package netns_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/stretchr/testify/require"
)

func TestNetNS_SingleThreadTransport(t *testing.T) {
	t.Run("no body", func(t *testing.T) {
		tp := &netns.SingleThreadTransport{}
		req, _ := http.NewRequest("GET", "http://example.com", nil)

		_, err := tp.RoundTrip(req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "request body required")
	})

	t.Run("dial error", func(t *testing.T) {
		tp := &netns.SingleThreadTransport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, errors.New("dial failed")
			},
		}
		body := io.NopCloser(strings.NewReader("test"))
		req, _ := http.NewRequest("POST", "http://example.com", body)

		_, err := tp.RoundTrip(req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "dial failed")
	})

	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(204)
		}))
		defer srv.Close()

		req, _ := http.NewRequest("POST", srv.URL, io.NopCloser(strings.NewReader("hi")))

		tp := &netns.SingleThreadTransport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("tcp", addr)
			},
		}
		resp, err := tp.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, 204, resp.StatusCode)
	})

	t.Run("response error", func(t *testing.T) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		go func() {
			conn, _ := ln.Accept()
			conn.Write([]byte("invalid response"))
			conn.Close()
		}()
		tp := &netns.SingleThreadTransport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("tcp", ln.Addr().String())
			},
		}
		req, _ := http.NewRequest("POST", "http://fake", io.NopCloser(strings.NewReader("x")))
		_, err := tp.RoundTrip(req)
		require.Error(t, err)
	})
}
