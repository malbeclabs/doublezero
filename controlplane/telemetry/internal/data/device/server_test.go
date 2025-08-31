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
	"sort"
	"sync"
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
		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
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

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuits", url.Values{"env": {"invalid"}})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies with valid params", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
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

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{}, &mockProvider{})
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

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{}, &mockProvider{})
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

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
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

	t.Run("GET /device-link/circuit-latencies with interval (happy path)", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		var gotCfg data.GetCircuitLatenciesConfig
		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				gotCfg = cfg
				return []stats.CircuitLatencyStat{{Circuit: cfg.Circuit, RTTMean: 7}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":      {"testnet"},
			"from":     {from},
			"to":       {to},
			"circuit":  {"{foo}"},
			"interval": {"2s"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		require.Len(t, out, 1)
		assert.Equal(t, "foo", out[0].Circuit)

		assert.Equal(t, "foo", gotCfg.Circuit)
		require.NotNil(t, gotCfg.Time)
		assert.True(t, gotCfg.Time.From.Equal(now))
		assert.True(t, gotCfg.Time.To.Equal(now.Add(10*time.Second)))
		assert.Equal(t, 2*time.Second, gotCfg.Interval)
		assert.Equal(t, uint64(0), gotCfg.MaxPoints)       // ignored when interval provided
		assert.Equal(t, data.UnitMicrosecond, gotCfg.Unit) // default when unit not provided
	})

	t.Run("GET /device-link/circuit-latencies with invalid interval", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":      {"testnet"},
			"from":     {from},
			"to":       {to},
			"circuit":  {"{foo}"},
			"interval": {"not-a-duration"},
		})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies with both interval and max_points set", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{}, &mockProvider{})
		defer closeFn()

		res, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":        {"testnet"},
			"from":       {from},
			"to":         {to},
			"circuit":    {"{foo}"},
			"interval":   {"5s"},
			"max_points": {"10"},
		})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies expands circuits via GetCircuits when circuit=all", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		var got []string
		var mu sync.Mutex

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitsFunc: func(context.Context) ([]data.Circuit, error) {
				// Intentionally unsorted to ensure handler sorts before partitioning/querying
				return []data.Circuit{{Code: "b"}, {Code: "a"}, {Code: "c"}}, nil
			},
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				mu.Lock()
				got = append(got, cfg.Circuit)
				mu.Unlock()
				return []stats.CircuitLatencyStat{{Circuit: cfg.Circuit, RTTMean: 1}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":     {"testnet"},
			"from":    {from},
			"to":      {to},
			"circuit": {"all"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		assert.Len(t, out, 3)

		// Order is not guaranteed (timestamps may tie); compare as sets.
		toSet := func(ss []string) map[string]struct{} {
			m := make(map[string]struct{}, len(ss))
			for _, s := range ss {
				m[s] = struct{}{}
			}
			return m
		}
		outSet := make([]string, 0, len(out))
		for _, s := range out {
			outSet = append(outSet, s.Circuit)
		}

		assert.Equal(t, toSet([]string{"a", "b", "c"}), toSet(outSet))
		assert.Equal(t, toSet([]string{"a", "b", "c"}), toSet(got))
	})

	t.Run("GET /device-link/circuit-latencies expands circuits via GetCircuits when circuit omitted", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		var got []string
		var mu sync.Mutex

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitsFunc: func(context.Context) ([]data.Circuit, error) {
				return []data.Circuit{{Code: "x"}, {Code: "y"}}, nil
			},
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				mu.Lock()
				got = append(got, cfg.Circuit)
				mu.Unlock()
				return []stats.CircuitLatencyStat{{Circuit: cfg.Circuit, RTTMean: 2}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":  {"testnet"},
			"from": {from},
			"to":   {to},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		assert.Len(t, out, 2)

		gotSet := map[string]struct{}{}
		for _, c := range got {
			gotSet[c] = struct{}{}
		}
		outSet := map[string]struct{}{}
		for _, s := range out {
			outSet[s.Circuit] = struct{}{}
		}
		want := map[string]struct{}{"x": {}, "y": {}}

		assert.Equal(t, want, gotSet)
		assert.Equal(t, want, outSet)
	})

	t.Run("GET /device-link/circuit-latencies circuit list retrieval error", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitsFunc: func(context.Context) ([]data.Circuit, error) {
				return nil, errors.New("boom")
			},
		}, &mockProvider{})
		defer closeFn()

		// Using "all" triggers the GetCircuits path.
		res, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":     {"testnet"},
			"from":    {from},
			"to":      {to},
			"circuit": {"all"},
		})
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies partitions circuits deterministically", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		var mu sync.Mutex
		var gotCircuits []string
		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				mu.Lock()
				gotCircuits = append(gotCircuits, cfg.Circuit)
				mu.Unlock()
				return []stats.CircuitLatencyStat{{Circuit: cfg.Circuit, RTTMean: 1}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		// circuits unsorted; handler should sort -> a,b,c,d,e
		// total_partitions=3 => sizes: [2,2,1], partition=1 => [c,d]
		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":              {"testnet"},
			"from":             {from},
			"to":               {to},
			"circuit":          {"{e,d,c,b,a}"},
			"partition":        {"1"},
			"total_partitions": {"3"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		require.Len(t, out, 2)

		wantSet := map[string]struct{}{"c": {}, "d": {}}
		for _, s := range out {
			_, ok := wantSet[s.Circuit]
			assert.True(t, ok, "unexpected circuit %q", s.Circuit)
			delete(wantSet, s.Circuit)
		}
		assert.Empty(t, wantSet, "missing expected circuits")

		sort.Strings(gotCircuits)
		assert.Equal(t, []string{"c", "d"}, gotCircuits)
	})

	t.Run("GET /device-link/circuit-latencies with only one of partition/total_partitions errors", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, _ data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				return []stats.CircuitLatencyStat{{Circuit: "should-not-be-called"}}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res1, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":       {"testnet"},
			"from":      {from},
			"to":        {to},
			"circuit":   {"{a,b}"},
			"partition": {"0"},
		})
		assert.Equal(t, http.StatusBadRequest, res1.StatusCode)

		res2, _ := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":              {"testnet"},
			"from":             {from},
			"to":               {to},
			"circuit":          {"{a,b}"},
			"total_partitions": {"2"},
		})
		assert.Equal(t, http.StatusBadRequest, res2.StatusCode)
	})

	t.Run("GET /device-link/circuit-latencies handles out-of-range partition with empty series (no panic)", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		// Return an empty series to exercise the branch that used to panic
		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, _ data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				return nil, nil
			},
		}, &mockProvider{})
		defer closeFn()

		// part=5 with only 2 circuits -> triggers the "partOutOfRange" hack path
		// tparts can be any valid number > part
		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":              {"testnet"},
			"from":             {from},
			"to":               {to},
			"circuit":          {"{a,b}"},
			"partition":        {"5"},
			"total_partitions": {"10"},
		})

		assert.Equal(t, http.StatusOK, res.StatusCode, "expected OK even when partition is out of range")

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out), "response should be valid JSON")
		assert.Len(t, out, 0, "with empty provider series, handler should return an empty result without panicking")
	})

	t.Run("GET /device-link/circuit-latencies out-of-range with zero-sample first series", func(t *testing.T) {
		t.Parallel()

		now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := now.Format(time.RFC3339), now.Add(10*time.Second).Format(time.RFC3339)

		zero := stats.CircuitLatencyStat{Circuit: "a"} // assumes zero/empty samples is allowed

		baseURL, closeFn := startServer(t, &mockProvider{}, &mockProvider{
			GetCircuitLatenciesFunc: func(_ context.Context, cfg data.GetCircuitLatenciesConfig) ([]stats.CircuitLatencyStat, error) {
				_ = cfg
				return []stats.CircuitLatencyStat{zero}, nil
			},
		}, &mockProvider{})
		defer closeFn()

		res, body := get(t, baseURL, "/device-link/circuit-latencies", url.Values{
			"env":              {"testnet"},
			"from":             {from},
			"to":               {to},
			"circuit":          {"{a,b}"},
			"partition":        {"5"},
			"total_partitions": {"10"},
		})
		assert.Equal(t, http.StatusOK, res.StatusCode)

		var out []stats.CircuitLatencyStat
		require.NoError(t, json.Unmarshal(body, &out))
		require.Len(t, out, 1)
		assert.Equal(t, "a", out[0].Circuit)
	})
}

func startServer(t *testing.T, mainnet, testnet, devnet data.Provider) (baseURL string, closeFn func()) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := data.NewServer(logger, mainnet, testnet, devnet)
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
