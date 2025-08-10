package data_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/stats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Device_Server(t *testing.T) {
	t.Parallel()

	t.Run("GET /device-link/circuits with valid env", func(t *testing.T) {
		t.Parallel()

		var called bool
		baseURL, closeFn := startServer(t, &mockProvider{
			GetCircuitsFunc: func(context.Context) ([]data.Circuit, error) {
				called = true
				return []data.Circuit{{Code: "foo"}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuits", url.Values{"env": {"testnet"}})
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, called)

		var circuits []data.Circuit
		require.NoError(t, json.Unmarshal(body, &circuits))
		assert.Len(t, circuits, 1)
	})

	t.Run("GET /device-link/circuits with invalid env", func(t *testing.T) {
		t.Parallel()

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuits", url.Values{"env": {"mainnet"}})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies with valid params", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				return []stats.CircuitLatencyStat{{Circuit: cfg.Circuit, RTTMean: 42}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":     {"testnet"},
			"from":    {from},
			"to":      {to},
			"circuit": {"{foo}"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		require.Len(t, out, 1)
		assert.Equal(t, "foo", out[0].Circuit)
	})

	t.Run("GET /device-link/circuit-latencies with invalid time range", func(t *testing.T) {
		t.Parallel()

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":     {"testnet"},
			"from":    {"notatime"},
			"to":      {"alsonotatime"},
			"circuit": {"{foo}"},
		})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies with invalid max_points", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":        {"testnet"},
			"from":       {from},
			"to":         {to},
			"circuit":    {"{foo}"},
			"max_points": {"xyz"},
		})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies skips errored circuits", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, _ data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				return nil, errors.New("expected")
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":     {"testnet"},
			"from":    {from},
			"to":      {to},
			"circuit": {"{a,b}"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.JSONEq(t, "[]", string(body))
	})
}

func startServer(t *testing.T, testnet, devnet data.Provider) (baseURL string, closeFn func()) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := data.NewServer(logger, testnet, devnet)
	require.NoError(t, err)

	ts := httptest.NewServer(srv.Mux)
	return ts.URL, ts.Close
}

func get(t *testing.T, baseURL, path string, q url.Values) (*http.Response, []byte) {
	t.Helper()
	u := baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	res, err := http.Get(u)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, b
}
