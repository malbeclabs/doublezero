package data_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Server(t *testing.T) {
	t.Run("GET /envs returns supported environments", func(t *testing.T) {
		addr, closeFn := startTestServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/envs")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)

		var envs []string
		require.NoError(t, json.NewDecoder(res.Body).Decode(&envs))
		assert.ElementsMatch(t, []string{"testnet", "devnet"}, envs)
	})

	t.Run("GET /device-circuits with valid env", func(t *testing.T) {
		var called bool
		addr, closeFn := startTestServer(t, &mockProvider{
			GetCircuitsFunc: func(context.Context) ([]data.Circuit, error) {
				called = true
				return []data.Circuit{{Code: "foo"}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuits?env=testnet")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, called)

		var circuits []data.Circuit
		require.NoError(t, json.NewDecoder(res.Body).Decode(&circuits))
		assert.Len(t, circuits, 1)
	})

	t.Run("GET /device-circuits with invalid env", func(t *testing.T) {
		addr, closeFn := startTestServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuits?env=mainnet")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-circuit-latencies with valid params", func(t *testing.T) {
		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from := now.Format(time.RFC3339)
		to := now.Add(10 * time.Second).Format(time.RFC3339)

		addr, closeFn := startTestServer(t, &mockProvider{
			GetCircuitLatenciesDownsampledFunc: func(_ context.Context, circuit string, _, _ time.Time, _ uint64) ([]data.CircuitLatencyStat, error) {
				return []data.CircuitLatencyStat{{Circuit: circuit, RTTMean: 42}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuit-latencies?env=testnet&from=" + from + "&to=" + to + "&circuit={foo}")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)

		var stats []data.CircuitLatencyStat
		require.NoError(t, json.NewDecoder(res.Body).Decode(&stats))
		assert.Len(t, stats, 1)
		assert.Equal(t, "foo", stats[0].Circuit)
	})

	t.Run("GET /device-circuit-latencies with invalid time range", func(t *testing.T) {
		addr, closeFn := startTestServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuit-latencies?env=testnet&from=notatime&to=alsonotatime&circuit={foo}")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-circuit-latencies with invalid max_points", func(t *testing.T) {
		now := time.Now().UTC()
		from := now.Format(time.RFC3339)
		to := now.Add(10 * time.Second).Format(time.RFC3339)

		addr, closeFn := startTestServer(t, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuit-latencies?env=testnet&from=" + from + "&to=" + to + "&circuit={foo}&max_points=xyz")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-circuit-latencies skips errored circuits", func(t *testing.T) {
		now := time.Now().UTC()
		from := now.Format(time.RFC3339)
		to := now.Add(10 * time.Second).Format(time.RFC3339)

		addr, closeFn := startTestServer(t, &mockProvider{
			GetCircuitLatenciesDownsampledFunc: func(_ context.Context, circuit string, _, _ time.Time, _ uint64) ([]data.CircuitLatencyStat, error) {
				return nil, errors.New("expected")
			},
		}, &mockProvider{})
		defer closeFn()

		res, err := http.Get(addr + "/device-circuit-latencies?env=testnet&from=" + from + "&to=" + to + "&circuit={a,b}")
		require.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)
		body, _ := io.ReadAll(res.Body)
		assert.JSONEq(t, "[]", string(body))
	})
}

func startTestServer(t *testing.T, testnet, devnet data.Provider) (addr string, closeFn func()) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := data.NewServer(logger, testnet, devnet)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(t.Context(), ln)
	}()

	return "http://" + ln.Addr().String(), func() {
		_ = ln.Close()
	}
}

func TestServer_envs(t *testing.T) {
	addr, closeFn := startTestServer(t, &mockProvider{}, &mockProvider{})
	defer closeFn()

	res, err := http.Get(addr + "/envs")
	require.NoError(t, err)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	var envs []string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&envs))
	assert.ElementsMatch(t, []string{"testnet", "devnet"}, envs)
}
