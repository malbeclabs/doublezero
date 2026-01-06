package twozoracle

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestTwoZOracleWatcher_isValidSwapRate(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	watcher := &TwoZOracleWatcher{
		log: logger,
	}

	tests := []struct {
		name     string
		swapRate float64
		want     bool
	}{
		{
			name:     "valid positive integer",
			swapRate: 100.0,
			want:     true,
		},
		{
			name:     "valid zero",
			swapRate: 0.0,
			want:     true,
		},
		{
			name:     "valid large integer",
			swapRate: 2764713870.0,
			want:     true,
		},
		{
			name:     "invalid negative integer",
			swapRate: -1.0,
			want:     false,
		},
		{
			name:     "invalid negative float",
			swapRate: -1.5,
			want:     false,
		},
		{
			name:     "invalid positive float",
			swapRate: 100.5,
			want:     false,
		},
		{
			name:     "invalid small positive float",
			swapRate: 0.1,
			want:     false,
		},
		{
			name:     "invalid large float",
			swapRate: 2764713870.9234,
			want:     false,
		},
		{
			name:     "invalid negative zero",
			swapRate: math.Copysign(0.0, -1.0),
			want:     true, // negative zero is still 0, which is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := watcher.isValidSwapRate(tt.swapRate)
			require.Equal(t, tt.want, got, "isValidSwapRate(%v) = %v, want %v", tt.swapRate, got, tt.want)
		})
	}
}

type mockClient struct {
	healthResp   HealthResponse
	healthCode   int
	healthErr    error
	swapRateResp SwapRateResponse
	swapRateCode int
	swapRateErr  error
}

func (m *mockClient) Health(ctx context.Context) (HealthResponse, int, error) {
	return m.healthResp, m.healthCode, m.healthErr
}

func (m *mockClient) SwapRate(ctx context.Context) (SwapRateResponse, int, error) {
	return m.swapRateResp, m.swapRateCode, m.swapRateErr
}

type testLogHandler struct {
	logs  []string
	level slog.Level
}

func (h *testLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *testLogHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key+"="+a.Value.String())
		return true
	})
	logMsg := r.Message
	if len(attrs) > 0 {
		logMsg += " " + strings.Join(attrs, " ")
	}
	h.logs = append(h.logs, logMsg)
	return nil
}

func (h *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *testLogHandler) WithGroup(name string) slog.Handler {
	return h
}

func TestTwoZOracleWatcher_Tick_MalformedSwapRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		swapRate float64
	}{
		{
			name:     "negative swap rate",
			swapRate: -1.0,
		},
		{
			name:     "fractional swap rate",
			swapRate: 100.5,
		},
		{
			name:     "large fractional swap rate",
			swapRate: 2764713870.9234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logHandler := &testLogHandler{
				logs:  []string{},
				level: slog.LevelDebug,
			}
			logger := slog.New(logHandler)

			mockClient := &mockClient{
				healthResp: HealthResponse{
					Healthy: true,
				},
				healthCode: 200,
				swapRateResp: SwapRateResponse{
					SwapRate:     tt.swapRate,
					Timestamp:    1234567890,
					Signature:    "test-sig",
					SOLPriceUSD:  "100.0",
					TwoZPriceUSD: "1.0",
					CacheHit:     false,
				},
				swapRateCode: 200,
			}

			cfg := &Config{
				Logger:   logger,
				Interval: time.Second,
				Client:   mockClient,
			}
			watcher, err := NewTwoZOracleWatcher(cfg)
			require.NoError(t, err)

			metricBefore := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeMalformedSwapRate, "200"))

			err = watcher.Tick(context.Background())
			require.NoError(t, err)

			var foundLog bool
			for _, log := range logHandler.logs {
				if strings.Contains(log, "swapRate is malformed") {
					foundLog = true
					require.Contains(t, log, "swapRate is malformed: expected unsigned integer, got")
					break
				}
			}
			require.True(t, foundLog, "expected warning log about malformed swap rate, but not found. logs: %v", logHandler.logs)

			metricAfter := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeMalformedSwapRate, "200"))
			require.Equal(t, metricBefore+1.0, metricAfter, "expected malformed swap rate metric to be incremented by 1")
		})
	}
}
