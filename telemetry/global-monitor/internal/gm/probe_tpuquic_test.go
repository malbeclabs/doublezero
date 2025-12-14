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

func TestGlobalMonitor_TPUQUICProbeTarget_New_NilLogger(t *testing.T) {
	t.Parallel()

	target, err := NewTPUQUICProbeTarget(nil, "eth0", "1.2.3.4:1111", nil)
	require.Error(t, err)
	require.Nil(t, target)
}

func TestGlobalMonitor_TPUQUICProbeTarget_New_OK(t *testing.T) {
	t.Parallel()

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)
	require.NotNil(t, target)
	require.Equal(t, "eth0", target.Interface())
	require.Equal(t, "1.2.3.4:1111", target.Addr())
	require.NotNil(t, target.cfg)
	require.NotNil(t, target.dialFunc)
}

func TestGlobalMonitor_TPUQUICProbeTarget_ID_Interface_Addr(t *testing.T) {
	t.Parallel()

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)

	require.Equal(t, "eth0", target.Interface())
	require.Equal(t, "1.2.3.4:1111", target.Addr())
	require.Equal(t, ProbeTargetID("tpuquic/eth0/1.2.3.4:1111"), target.ID())
}

func TestGlobalMonitor_TPUQUICProbeTarget_Close_NoConnIsNoop(t *testing.T) {
	t.Parallel()

	target := &TPUQUICProbeTarget{}
	require.NotPanics(t, func() { target.Close() })
}

func TestGlobalMonitor_TPUQUICProbeTarget_Close_ClosesConnAndClears(t *testing.T) {
	t.Parallel()

	closeCount := 0
	target := &TPUQUICProbeTarget{
		conn: &MockTPUQUICConn{
			CloseFunc: func() error {
				closeCount++
				return nil
			},
		},
	}
	target.Close()
	require.Equal(t, 1, closeCount)
	require.Nil(t, target.conn)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_PreflightBlocks(t *testing.T) {
	t.Parallel()

	cfg := &TPUQUICProbeTargetConfig{
		PreflightFunc: func(ctx context.Context) (ProbeFailReason, bool) {
			return ProbeFailReasonOther, false
		},
	}

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", cfg)
	require.NoError(t, err)

	// Ensure we never dial if preflight fails
	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		t.Fatalf("dialFunc should not be called when preflight fails")
		return nil, nil
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonOther, res.FailReason)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_DialTimeoutByString(t *testing.T) {
	t.Parallel()

	dialErr := errors.New("boom: timeout: no recent network activity here")
	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return nil, dialErr
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonTimeout, res.FailReason)
	require.ErrorIs(t, res.FailError, dialErr)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_DialTimeoutByDeadlineExceeded(t *testing.T) {
	t.Parallel()

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return nil, context.DeadlineExceeded
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonTimeout, res.FailReason)
	require.ErrorIs(t, res.FailError, context.DeadlineExceeded)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_DialOtherError(t *testing.T) {
	t.Parallel()

	dialErr := errors.New("boom")
	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return nil, dialErr
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonOther, res.FailReason)
	require.ErrorIs(t, res.FailError, dialErr)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_ConnNilAfterDial(t *testing.T) {
	t.Parallel()

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", nil)
	require.NoError(t, err)

	// dialFunc returns (nil, nil) – dialIfNeeded will "succeed" but target.conn stays nil
	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return nil, nil
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonOther, res.FailReason)
	require.Error(t, res.FailError)
	require.Contains(t, res.FailError.Error(), "connection is nil")
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_StatsReadySuccess(t *testing.T) {
	t.Parallel()

	readyStats := quic.ConnectionStats{
		PacketsSent:     20,
		PacketsReceived: 15,
		MinRTT:          1 * time.Millisecond,
		SmoothedRTT:     5 * time.Millisecond,
		MeanDeviation:   2 * time.Millisecond,
		LatestRTT:       5 * time.Millisecond,
	}

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", &TPUQUICProbeTargetConfig{})
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return &MockTPUQUICConn{
			ConnectionStatsFunc: func() quic.ConnectionStats { return readyStats },
		}, nil
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.True(t, res.OK)

	// FIX: compare with the correct type (or use require.Empty)
	require.Equal(t, ProbeFailReason(""), res.FailReason)
	require.Nil(t, res.FailError)

	require.NotNil(t, res.Stats)
	require.EqualValues(t, readyStats.PacketsSent, res.Stats.PacketsSent)
	require.EqualValues(t, readyStats.PacketsReceived, res.Stats.PacketsRecv)
	require.EqualValues(t, readyStats.PacketsSent-readyStats.PacketsReceived, res.Stats.PacketsLost)
	require.InDelta(t,
		float64(readyStats.PacketsSent-readyStats.PacketsReceived)/float64(readyStats.PacketsSent),
		res.Stats.LossRatio, 1e-9)
	require.Equal(t, readyStats.MinRTT, res.Stats.RTTMin)
	require.Equal(t, readyStats.SmoothedRTT, res.Stats.RTTAvg)
	require.Equal(t, readyStats.MeanDeviation, res.Stats.RTTStdDev)
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_StatsNotReady(t *testing.T) {
	t.Parallel()

	// Zero stats => definitely not ready by quicStatsNotReady
	notReadyStats := quic.ConnectionStats{}

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", &TPUQUICProbeTargetConfig{
		KeepAlivePeriod: 1 * time.Millisecond,
	})
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return &MockTPUQUICConn{
			ConnectionStatsFunc: func() quic.ConnectionStats { return notReadyStats },
		}, nil
	}

	// Use a short timeout so we don't sit in Until too long
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	res, probeErr := target.Probe(ctx)
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonNotReady, res.FailReason)
	require.Error(t, res.FailError)
	require.Equal(t, "stats not ready", res.FailError.Error())
}

func TestGlobalMonitor_TPUQUICProbeTarget_Probe_PacketsLost(t *testing.T) {
	t.Parallel()

	lossyStats := quic.ConnectionStats{
		PacketsSent:     10,
		PacketsReceived: 0,
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
	}

	target, err := NewTPUQUICProbeTarget(newTestLogger(), "eth0", "1.2.3.4:1111", &TPUQUICProbeTargetConfig{})
	require.NoError(t, err)

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		return &MockTPUQUICConn{
			ConnectionStatsFunc: func() quic.ConnectionStats { return lossyStats },
		}, nil
	}

	res, probeErr := target.Probe(context.Background())
	require.NoError(t, probeErr)
	require.NotNil(t, res)
	require.False(t, res.OK)
	require.Equal(t, ProbeFailReasonPacketsLost, res.FailReason)
	require.Error(t, res.FailError)
	require.Equal(t, "no packets received", res.FailError.Error())
	require.NotNil(t, res.Stats)
	require.EqualValues(t, lossyStats.PacketsSent, res.Stats.PacketsSent)
	require.EqualValues(t, lossyStats.PacketsReceived, res.Stats.PacketsRecv)
	require.EqualValues(t, lossyStats.PacketsSent, res.Stats.PacketsLost)
	require.InDelta(t, 1.0, res.Stats.LossRatio, 1e-9)
}

func TestGlobalMonitor_TPUQUICProbeTarget_dialIfNeeded_DialsAndCaches(t *testing.T) {
	t.Parallel()

	target := &TPUQUICProbeTarget{
		log:   newTestLogger(),
		cfg:   &TPUQUICProbeTargetConfig{},
		iface: "eth0",
		addr:  "1.2.3.4:1111",
	}

	mockConn := &MockTPUQUICConn{}
	dialCount := 0

	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		dialCount++
		require.Equal(t, "1.2.3.4:1111", addr)
		require.NotNil(t, cfg)
		return mockConn, nil
	}

	ctx := context.Background()

	dialed1, err := target.dialIfNeeded(ctx)
	require.NoError(t, err)
	require.True(t, dialed1)
	require.Equal(t, mockConn, target.conn)
	require.Equal(t, 1, dialCount)

	dialed2, err := target.dialIfNeeded(ctx)
	require.NoError(t, err)
	require.False(t, dialed2)
	require.Equal(t, mockConn, target.conn)
	require.Equal(t, 1, dialCount, "should not dial again when conn is open")
}

func TestGlobalMonitor_TPUQUICProbeTarget_dialIfNeeded_RedialsWhenClosed(t *testing.T) {
	t.Parallel()

	closeCount := 0

	// Existing conn is marked closed
	conn1 := &MockTPUQUICConn{
		IsClosedFunc: func() bool { return true },
		CloseFunc: func() error {
			closeCount++
			return nil
		},
	}

	// New conn that will be returned by dial
	conn2 := &MockTPUQUICConn{}

	target := &TPUQUICProbeTarget{
		log:   newTestLogger(),
		cfg:   &TPUQUICProbeTargetConfig{},
		iface: "eth0",
		addr:  "1.2.3.4:1111",
		conn:  conn1,
	}

	dialCount := 0
	target.dialFunc = func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
		dialCount++
		require.Equal(t, "1.2.3.4:1111", addr)
		require.NotNil(t, cfg)
		// Always return conn2 as the "new" connection
		return conn2, nil
	}

	ctx := context.Background()

	// First call: sees closed conn1, closes it, and dials conn2
	dialed1, err := target.dialIfNeeded(ctx)
	require.NoError(t, err)
	require.True(t, dialed1, "should report that it dialed")
	require.Equal(t, conn2, target.conn, "should replace closed conn with new one")
	require.Equal(t, 1, dialCount, "dialFunc should be called once")
	require.Equal(t, 1, closeCount, "closed conn should be closed once")

	// Second call: conn2 is open; no redial
	dialed2, err := target.dialIfNeeded(ctx)
	require.NoError(t, err)
	require.False(t, dialed2, "should not dial again when conn is open")
	require.Equal(t, conn2, target.conn, "conn should stay the same")
	require.Equal(t, 1, dialCount, "no additional dials")
	require.Equal(t, 1, closeCount, "no additional closes")
}

func TestGlobalMonitor_TPUQUICProbeTarget_quicStatsNotReady_InitialDefaults(t *testing.T) {
	t.Parallel()

	stats := quic.ConnectionStats{
		MeanDeviation:   0,
		LatestRTT:       100 * time.Millisecond,
		PacketsReceived: 10,
		PacketsSent:     10,
	}
	require.True(t, quicStatsNotReady(stats))
}

func TestGlobalMonitor_TPUQUICProbeTarget_quicStatsNotReady_ZeroRTTOrPacketsSent(t *testing.T) {
	t.Parallel()

	stats1 := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       0,
		PacketsReceived: 20,
		PacketsSent:     20,
	}
	require.True(t, quicStatsNotReady(stats1))

	stats2 := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     0,
	}
	require.True(t, quicStatsNotReady(stats2))
}

func TestGlobalMonitor_TPUQUICProbeTarget_quicStatsNotReady_ReadyStats(t *testing.T) {
	t.Parallel()

	stats := quic.ConnectionStats{
		MeanDeviation:   1,
		LatestRTT:       5 * time.Millisecond,
		PacketsReceived: 20,
		PacketsSent:     20,
	}
	require.False(t, quicStatsNotReady(stats))
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
