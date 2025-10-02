package serviceability

import (
	"bytes"
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type mockInfluxWriter struct {
	WriteRecordFunc func(string)
	FlushFunc       func()
	ErrorsFunc      func() <-chan error
	writeCount      atomic.Int32
	flushCount      atomic.Int32
}

func (m *mockInfluxWriter) WriteRecord(s string) {
	if m.WriteRecordFunc != nil {
		m.WriteRecordFunc(s)
	}
	m.writeCount.Add(1)
}

func (m *mockInfluxWriter) Flush() {
	if m.FlushFunc != nil {
		m.FlushFunc()
	}
	m.flushCount.Add(1)
}

func (m *mockInfluxWriter) Errors() <-chan error {
	if m.ErrorsFunc != nil {
		return m.ErrorsFunc()
	}
	ch := make(chan error)
	close(ch)
	return ch
}

func TestMonitor_Serviceability_Watcher(t *testing.T) {
	t.Parallel()

	t.Run("new_watcher_validates_config", func(t *testing.T) {
		t.Parallel()
		_, err := NewServiceabilityWatcher(&Config{Logger: nil, Serviceability: nil, Interval: 0})
		require.Error(t, err)

		cfg := &Config{Logger: newTestLogger(t), Serviceability: &mockServiceabilityClient{}, Interval: 10 * time.Millisecond}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)
		require.NotNil(t, w)
		require.Equal(t, watcherName, w.Name())
	})

	t.Run("tick_success_sets_build_info", func(t *testing.T) {
		t.Parallel()
		version := serviceability.ProgramVersion{Major: 1, Minor: 2, Patch: 3}
		got := &serviceability.ProgramData{ProgramConfig: serviceability.ProgramConfig{Version: version}}
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:       10 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		err = w.Tick(context.Background())
		require.NoError(t, err)

		lbl := programVersionString(version)
		val := testutil.ToFloat64(MetricProgramBuildInfo.WithLabelValues(lbl))
		require.Equal(t, float64(1), val)
	})

	t.Run("tick_error_increments_error_metric", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return nil, errors.New("boom") }},
			Interval:       10 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		before := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		err = w.Tick(context.Background())
		require.Error(t, err)
		after := testutil.ToFloat64(MetricErrors.WithLabelValues(MetricErrorTypeGetProgramData))
		require.GreaterOrEqual(t, after-before, float64(1))
	})

	t.Run("tick_with_influx_writer_writes_metrics", func(t *testing.T) {
		t.Parallel()
		mockWriter := &mockInfluxWriter{}
		devices := []serviceability.Device{
			{Code: "dev1"},
			{Code: "dev2"},
		}
		programData := &serviceability.ProgramData{
			Devices: devices,
		}

		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return programData, nil }},
			Interval:       10 * time.Millisecond,
			InfluxWriter:   mockWriter,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		require.NoError(t, w.Tick(context.Background()))
		require.Equal(t, int32(len(devices)), mockWriter.writeCount.Load(), "WriteRecord should be called for each device")
		require.Equal(t, int32(1), mockWriter.flushCount.Load(), "Flush should be called once per tick")
	})

	t.Run("run_stops_on_context_cancel", func(t *testing.T) {
		t.Parallel()
		got := &serviceability.ProgramData{ProgramConfig: serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 9, Minor: 9, Patch: 9}}}
		cfg := &Config{
			Logger:         newTestLogger(t),
			Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) { return got, nil }},
			Interval:       5 * time.Millisecond,
		}
		w, err := NewServiceabilityWatcher(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()

		time.Sleep(15 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(250 * time.Millisecond):
			t.Fatal("Run did not return after cancel")
		}
	})

	t.Run("programVersionString_formats", func(t *testing.T) {
		t.Parallel()
		s := programVersionString(serviceability.ProgramVersion{Major: 0, Minor: 10, Patch: 7})
		require.Equal(t, "0.10.7", s)
	})
}

// mockRoundTripper allows us to control HTTP responses for epoch RPC calls.
type mockRoundTripper struct {
	responses [][]byte
	call      int
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       ioutil.NopCloser(bytes.NewReader(m.responses[m.call])),
		Header:     make(http.Header),
	}
	m.call++
	return resp, nil
}

func TestWatcher_EpochChangeDetection(t *testing.T) {
	mockRT := &mockRoundTripper{
		responses: [][]byte{
			[]byte(`{"result":{"epoch":1,"absoluteSlot":0,"blockHeight":0,"slotIndex":0,"slotsInEpoch":0,"transactionCount":0,"leaderScheduleEpoch":0,"startSlot":0,"warmup":false}}`), // doublezero, first tick
			[]byte(`{"result":{"epoch":1,"absoluteSlot":0,"blockHeight":0,"slotIndex":0,"slotsInEpoch":0,"transactionCount":0,"leaderScheduleEpoch":0,"startSlot":0,"warmup":false}}`), // solana, first tick
			[]byte(`{"result":{"epoch":2,"absoluteSlot":0,"blockHeight":0,"slotIndex":0,"slotsInEpoch":0,"transactionCount":0,"leaderScheduleEpoch":0,"startSlot":0,"warmup":false}}`), // doublezero, second tick
			[]byte(`{"result":{"epoch":2,"absoluteSlot":0,"blockHeight":0,"slotIndex":0,"slotsInEpoch":0,"transactionCount":0,"leaderScheduleEpoch":0,"startSlot":0,"warmup":false}}`), // solana, second tick
		},
	}

	mockHTTP := &http.Client{Transport: mockRT}

	cfg := &Config{
		Logger: newTestLogger(t),
		Serviceability: &mockServiceabilityClient{GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{
				ProgramConfig: serviceability.ProgramConfig{Version: serviceability.ProgramVersion{Major: 1, Minor: 0, Patch: 0}},
			}, nil
		}},
		Interval:           10 * time.Millisecond,
		LedgerPublicRPCURL: "http://mock-dz",
		SolanaRPCURL:       "http://mock-sol",
	}
	w, err := NewServiceabilityWatcher(cfg)
	require.NoError(t, err)
	w.rpcClient = mockHTTP

	// first tick: should set both epochs to 1
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, uint64(1), w.currDZEpoch)
	require.Equal(t, uint64(1), w.currSolanaEpoch)

	// second tick: should detect epoch change to 2
	require.NoError(t, w.Tick(context.Background()))
	require.Equal(t, uint64(2), w.currDZEpoch)
	require.Equal(t, uint64(2), w.currSolanaEpoch)
}
