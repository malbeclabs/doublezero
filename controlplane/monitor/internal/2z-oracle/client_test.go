package twozoracle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonitor_TwoZOracle_Client(t *testing.T) {
	t.Parallel()

	t.Run("SwapRate_OK", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)

		c := newTestClient(t, fs.BaseURL, 2*time.Second)

		out, _, err := c.SwapRate(context.Background())
		r.NoError(err)
		r.Equal(float64(2764713870.9234), out.SwapRate)
		r.Equal(int64(1758741874), out.Timestamp)
		r.NotEmpty(out.SOLPriceUSD)
		r.NotEmpty(out.TwoZPriceUSD)
		r.True(out.CacheHit)
	})

	t.Run("Health_OK", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)

		c := newTestClient(t, fs.BaseURL, 2*time.Second)

		out, _, err := c.Health(context.Background())
		r.NoError(err)
		r.True(out.Healthy)
		r.Len(out.HealthChecks, 1)
		r.Equal("pyth", out.HealthChecks[0].ServiceType)
		r.Equal("CLOSED", out.CircuitBreaker.State)
		r.NotEmpty(out.Timestamp)
	})

	t.Run("SwapRate_Non200", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)
		fs.SetSwap(503, `{"error":"unavailable"}`, 0)

		c := newTestClient(t, fs.BaseURL, 2*time.Second)

		_, _, err := c.SwapRate(context.Background())
		r.EqualError(err, "error getting swap rate: 503 Service Unavailable: {\"error\":\"unavailable\"}")
	})

	t.Run("Health_Non200", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)
		fs.SetHealth(503, `{"error":"unavailable"}`, 0)

		c := newTestClient(t, fs.BaseURL, 2*time.Second)

		_, _, err := c.Health(context.Background())
		r.EqualError(err, "error getting health: 503 Service Unavailable: {\"error\":\"unavailable\"}")
	})

	t.Run("Health_BadJSON", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)
		fs.SetHealth(200, `{"healthy": true, "healthChecks":[`, 0)

		c := newTestClient(t, fs.BaseURL, 2*time.Second)

		_, _, err := c.Health(context.Background())
		r.EqualError(err, "unexpected EOF")
	})

	t.Run("SwapRate_Timeout", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		fs := NewFakeTwoZOracleServer(t)
		t.Cleanup(fs.Close)
		fs.SetSwap(200, `{"swapRate":1,"timestamp":1,"signature":"x","solPriceUsd":"1","twozPriceUsd":"1","cacheHit":false}`, 300*time.Millisecond)

		c := newTestClient(t, fs.BaseURL, 100*time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		_, _, err := c.SwapRate(ctx)
		r.ErrorContains(err, "context deadline exceeded (Client.Timeout exceeded while awaiting headers)")
	})
}

func newTestClient(t *testing.T, baseURL string, timeout time.Duration) TwoZOracleClient {
	t.Helper()
	return NewTwoZOracleClient(&http.Client{Timeout: timeout}, baseURL)
}

type fakeResp struct {
	status int
	body   string
	delay  time.Duration
}

type FakeTwoZOracleServer struct {
	t            *testing.T
	srv          *httptest.Server
	swap, health atomic.Value // holds fakeResp
	BaseURL      string
}

func NewFakeTwoZOracleServer(t *testing.T) *FakeTwoZOracleServer {
	t.Helper()
	fs := &FakeTwoZOracleServer{t: t}

	// sane defaults mirroring the real API
	fs.swap.Store(fakeResp{
		status: 200,
		body:   `{"swapRate":2764713870.9234,"timestamp":1758741874,"signature":"sig","solPriceUsd":"213.9","twozPriceUsd":"7.73","cacheHit":true}`,
	})
	fs.health.Store(fakeResp{
		status: 200,
		body:   `{"healthy":true,"healthChecks":[{"serviceType":"pyth","status":"HEALTHY","hermes_connected":true,"cache_connected":true,"last_price_update":"2025-09-24T19:24:33.054Z"}],"circuitBreaker":{"state":"CLOSED","lastFailureReason":"none"},"timestamp":"2025-09-24T19:55:26.718Z"}`,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/swap-rate", func(w http.ResponseWriter, r *http.Request) {
		resp := fs.swap.Load().(fakeResp)
		if resp.delay > 0 {
			time.Sleep(resp.delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		resp := fs.health.Load().(fakeResp)
		if resp.delay > 0 {
			time.Sleep(resp.delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	})

	fs.srv = httptest.NewServer(mux)
	fs.BaseURL = fs.srv.URL
	return fs
}

func (f *FakeTwoZOracleServer) Close() { f.srv.Close() }
func (f *FakeTwoZOracleServer) SetSwap(status int, body string, delay time.Duration) {
	f.swap.Store(fakeResp{status: status, body: body, delay: delay})
}
func (f *FakeTwoZOracleServer) SetHealth(status int, body string, delay time.Duration) {
	f.health.Store(fakeResp{status: status, body: body, delay: delay})
}
