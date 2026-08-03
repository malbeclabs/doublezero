package serviceability_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	telemetrysvc "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	fn func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *mockProvider) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.fn(ctx)
}

func TestAgentTelemetry_Serviceability_CachingFetcher_StaleLogging(t *testing.T) {
	t.Parallel()

	t.Run("logs the first stale read and the recovery, not every refresh", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

		fresh := &serviceability.ProgramData{}
		failing := false
		provider := &mockProvider{fn: func(context.Context) (*serviceability.ProgramData, error) {
			if failing {
				return nil, errors.New("ledger unreachable")
			}
			return fresh, nil
		}}

		// Zero TTL so every call takes the fetch path, as a consumer refresh does.
		f := telemetrysvc.NewCachingFetcher(log, provider, 0, time.Second)

		// Populate the cache.
		data, err := f.GetProgramData(context.Background())
		require.NoError(t, err)
		require.Same(t, fresh, data)
		require.Empty(t, logs.String(), "a healthy fetch should log nothing")

		// A sustained outage: stale data is served throughout, and warned about once.
		failing = true
		for range 10 {
			data, err = f.GetProgramData(context.Background())
			require.NoError(t, err, "stale cached data should still be served")
			require.Same(t, fresh, data)
		}

		out := logs.String()
		assert.Equal(t, 1, strings.Count(out, "Program data fetch failed, serving stale cached data"),
			"an outage should warn on its first stale read only")
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "error=\"ledger unreachable\"")

		// Recovery reports how long stale data was served and how often.
		logs.Reset()
		failing = false
		_, err = f.GetProgramData(context.Background())
		require.NoError(t, err)

		out = logs.String()
		assert.Contains(t, out, "Program data fetch recovered, serving fresh data")
		assert.Contains(t, out, "staleReads=10")
		assert.Contains(t, out, "downtime=")

		// And the recovery is not repeated on every healthy fetch after it.
		logs.Reset()
		_, err = f.GetProgramData(context.Background())
		require.NoError(t, err)
		assert.Empty(t, logs.String())
	})

	t.Run("warns again after recovering and failing a second time", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

		failing := false
		provider := &mockProvider{fn: func(context.Context) (*serviceability.ProgramData, error) {
			if failing {
				return nil, errors.New("ledger unreachable")
			}
			return &serviceability.ProgramData{}, nil
		}}

		f := telemetrysvc.NewCachingFetcher(log, provider, 0, time.Second)

		_, err := f.GetProgramData(context.Background())
		require.NoError(t, err)

		for _, fail := range []bool{true, true, false, true, true} {
			failing = fail
			_, err := f.GetProgramData(context.Background())
			require.NoError(t, err)
		}

		out := logs.String()
		assert.Equal(t, 2, strings.Count(out, "Program data fetch failed, serving stale cached data"),
			"each distinct outage should warn once")
		assert.Equal(t, 1, strings.Count(out, "Program data fetch recovered"))
	})

	t.Run("returns the error when there is no cache to fall back on", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

		provider := &mockProvider{fn: func(context.Context) (*serviceability.ProgramData, error) {
			return nil, errors.New("ledger unreachable")
		}}

		f := telemetrysvc.NewCachingFetcher(log, provider, 0, time.Second)

		_, err := f.GetProgramData(context.Background())
		require.Error(t, err)
		assert.NotContains(t, logs.String(), "serving stale cached data")
	})
}
