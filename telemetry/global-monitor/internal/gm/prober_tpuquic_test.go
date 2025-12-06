package gm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_TPUQUICProber_NewTPUQUICProber_NilLogger(t *testing.T) {
	t.Parallel()

	prober, err := NewTPUQUICProber(nil)
	require.Error(t, err)
	require.Nil(t, prober)
}

func TestGlobalMonitor_TPUQUICProber_NewTPUQUICProber_OK(t *testing.T) {
	t.Parallel()

	prober, err := NewTPUQUICProber(newTestLogger())
	require.NoError(t, err)
	require.NotNil(t, prober)
	require.NotNil(t, prober.dialFunc)
	require.NotNil(t, prober.connsByAddr)
}

func TestGlobalMonitor_TPUQUICProber_statsNotReady_InitialDefaults(t *testing.T) {
	t.Parallel()

	stats := quic.ConnectionStats{
		MeanDeviation:   0,
		LatestRTT:       100 * time.Millisecond,
		PacketsReceived: 10,
		PacketsSent:     10,
	}
	require.True(t, statsNotReady(stats))
}

func TestGlobalMonitor_TPUQUICProber_statsNotReady_ZeroRTTOrPacketsSent(t *testing.T) {
	t.Parallel()

	// LatestRTT == 0
	stats1 := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       0,
		PacketsReceived: 20,
		PacketsSent:     20,
	}
	require.True(t, statsNotReady(stats1))

	// PacketsSent == 0
	stats2 := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     0,
	}
	require.True(t, statsNotReady(stats2))
}

func TestGlobalMonitor_TPUQUICProber_statsNotReady_ReadyStats(t *testing.T) {
	t.Parallel()

	stats := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     20,
	}
	require.False(t, statsNotReady(stats))
}

func TestGlobalMonitor_TPUQUICProber_Probe_StatsReady(t *testing.T) {
	t.Parallel()

	readyStats := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     20,
	}

	dialCount := 0
	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			dialCount++
			return &MockTPUQUICConn{
				ConnectionStatsFunc: func() quic.ConnectionStats { return readyStats },
			}, nil
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	cfg := TPUQUICProbeConfig{
		Timeout:        0,
		DialConfig:     nil,
		DelayAfterDial: 0,
	}

	stats, err := p.Probe(context.Background(), "validator-1", cfg)
	require.NoError(t, err)
	require.NotNil(t, stats)
	require.Equal(t, readyStats, *stats)
	require.Equal(t, 1, dialCount)
}

func TestGlobalMonitor_TPUQUICProber_Probe_StatsNotReady(t *testing.T) {
	t.Parallel()

	notReadyStats := quic.ConnectionStats{} // zero => definitely not ready

	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			return &MockTPUQUICConn{
				ConnectionStatsFunc: func() quic.ConnectionStats { return notReadyStats },
			}, nil
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	cfg := TPUQUICProbeConfig{
		Timeout:        0,
		DialConfig:     nil,
		DelayAfterDial: 0,
	}

	stats, err := p.Probe(context.Background(), "validator-1", cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrStatsNotReady)
	require.NotNil(t, stats)
	require.Equal(t, notReadyStats, *stats)
}

func TestGlobalMonitor_TPUQUICProber_Probe_FailedToDial(t *testing.T) {
	t.Parallel()

	dialErr := errors.New("boom")
	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			return nil, dialErr
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	cfg := TPUQUICProbeConfig{
		Timeout:        0,
		DialConfig:     nil,
		DelayAfterDial: 0,
	}

	stats, err := p.Probe(context.Background(), "validator-1", cfg)
	require.Error(t, err)
	require.Nil(t, stats)
	require.ErrorIs(t, err, ErrFailedToDial)
	require.ErrorIs(t, err, dialErr)
}

func TestGlobalMonitor_TPUQUICProber_Probe_DelayHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	readyStats := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     20,
	}

	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			return &MockTPUQUICConn{
				ConnectionStatsFunc: func() quic.ConnectionStats { return readyStats },
			}, nil
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before Probe

	cfg := TPUQUICProbeConfig{
		Timeout:        0, // use caller's ctx
		DialConfig:     nil,
		DelayAfterDial: 50 * time.Millisecond, // would sleep, but ctx is canceled
	}

	stats, err := p.Probe(ctx, "validator-1", cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stats)
}

func TestGlobalMonitor_TPUQUICProber_DialOrGet_ReusesExistingConn(t *testing.T) {
	t.Parallel()

	readyStats := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     20,
	}

	dialCount := 0
	mockConn := &MockTPUQUICConn{
		ConnectionStatsFunc: func() quic.ConnectionStats { return readyStats },
	}

	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			dialCount++
			return mockConn, nil
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	ctx := context.Background()

	conn1, dialed1, err := p.dialOrGet(ctx, "validator-1", nil)
	require.NoError(t, err)
	require.True(t, dialed1)
	require.Equal(t, mockConn, conn1)
	require.Equal(t, 1, dialCount)

	conn2, dialed2, err := p.dialOrGet(ctx, "validator-1", nil)
	require.NoError(t, err)
	require.False(t, dialed2)
	require.Equal(t, mockConn, conn2)
	require.Equal(t, 1, dialCount, "should not dial again for same addr")
}

func TestGlobalMonitor_TPUQUICProber_DialOrGet_RedialsWhenClosed(t *testing.T) {
	t.Parallel()

	closeCount := 0

	conn1 := &MockTPUQUICConn{
		IsClosedFunc: func() bool { return true },
		CloseFunc: func() error {
			closeCount++
			return nil
		},
	}
	conn2 := &MockTPUQUICConn{}

	dialCount := 0
	p := &TPUQUICProber{
		log: newTestLogger(),
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			dialCount++
			if dialCount == 1 {
				return conn1, nil
			}
			return conn2, nil
		},
		connsByAddr: make(map[string]tpuquicConn),
	}

	ctx := context.Background()

	first, dialed1, err := p.dialOrGet(ctx, "validator-1", nil)
	require.NoError(t, err)
	require.True(t, dialed1)
	require.Equal(t, conn1, first)
	require.Equal(t, 1, dialCount)

	second, dialed2, err := p.dialOrGet(ctx, "validator-1", nil)
	require.NoError(t, err)
	require.True(t, dialed2, "closed conn should force re-dial")
	require.Equal(t, conn2, second)
	require.Equal(t, 2, dialCount)
	require.Equal(t, 1, closeCount, "closed conn should be closed once")
}

func TestGlobalMonitor_TPUQUICProber_Close_ClosesAllConnsAndClearsMap(t *testing.T) {
	t.Parallel()

	closeCount1 := 0
	closeCount2 := 0

	conn1 := &MockTPUQUICConn{
		CloseFunc: func() error {
			closeCount1++
			return nil
		},
	}
	conn2 := &MockTPUQUICConn{
		CloseFunc: func() error {
			closeCount2++
			return nil
		},
	}

	p := &TPUQUICProber{
		log:         newTestLogger(),
		dialFunc:    nil,
		connsByAddr: map[string]tpuquicConn{"a": conn1, "b": conn2},
	}

	err := p.Close()
	require.NoError(t, err)
	require.Equal(t, 1, closeCount1)
	require.Equal(t, 1, closeCount2)
	require.Empty(t, p.connsByAddr)
}

func TestGlobalMonitor_TPUQUICProber_Close_PropagatesFirstError(t *testing.T) {
	t.Parallel()

	err1 := errors.New("first")
	err2 := errors.New("second")

	conn1 := &MockTPUQUICConn{
		CloseFunc: func() error { return err1 },
	}
	conn2 := &MockTPUQUICConn{
		CloseFunc: func() error { return err2 },
	}

	p := &TPUQUICProber{
		log:         newTestLogger(),
		dialFunc:    nil,
		connsByAddr: map[string]tpuquicConn{"a": conn1, "b": conn2},
	}

	err := p.Close()
	require.Error(t, err)
	require.ErrorIs(t, err, err1, "should return first non-nil error")
	require.Empty(t, p.connsByAddr)
}

func TestGlobalMonitor_TPUQUICProber_Prune_ClosesAndRemovesStaleAddrs(t *testing.T) {
	t.Parallel()

	closeCountStale := 0
	closeCountKeep := 0

	staleConn := &MockTPUQUICConn{
		CloseFunc: func() error {
			closeCountStale++
			return nil
		},
	}
	keepConn := &MockTPUQUICConn{
		CloseFunc: func() error {
			closeCountKeep++
			return nil
		},
	}

	p := &TPUQUICProber{
		log: newTestLogger(),
		connsByAddr: map[string]tpuquicConn{
			"addr-stale": staleConn,
			"addr-keep":  keepConn,
		},
	}

	seqOf := func(vals ...string) func(yield func(string) bool) {
		return func(yield func(string) bool) {
			for _, v := range vals {
				if !yield(v) {
					return
				}
			}
		}
	}

	p.Prune(seqOf("addr-keep"))

	require.Equal(t, 1, closeCountStale)
	require.Equal(t, 0, closeCountKeep)
	require.Len(t, p.connsByAddr, 1)
	_, ok := p.connsByAddr["addr-keep"]
	require.True(t, ok)
}

type MockTPUQUICConn struct {
	ConnectionStatsFunc func() quic.ConnectionStats
	IsClosedFunc        func() bool
	CloseFunc           func() error
}

func (c *MockTPUQUICConn) ConnectionStats() quic.ConnectionStats {
	if c.ConnectionStatsFunc == nil {
		return quic.ConnectionStats{}
	}
	return c.ConnectionStatsFunc()
}

func (c *MockTPUQUICConn) IsClosed() bool {
	if c.IsClosedFunc == nil {
		return false
	}
	return c.IsClosedFunc()
}

func (c *MockTPUQUICConn) Close() error {
	if c.CloseFunc == nil {
		return nil
	}
	return c.CloseFunc()
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}
