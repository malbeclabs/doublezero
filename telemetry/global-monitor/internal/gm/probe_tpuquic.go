package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/metrics"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
)

var (
	ErrFailedToDial = errors.New("failed to dial")
)

const (
	defaultTPUQUICStatsReadyTimeout = 10 * time.Second
)

type TPUQUICProbeTarget struct {
	log *slog.Logger
	cfg *TPUQUICProbeTargetConfig

	iface string
	addr  string

	dialFunc tpuquicDialFunc
	conn     tpuquicConn
	mu       sync.Mutex
}

type TPUQUICProbeTargetConfig struct {
	MaxIdleTimeout       time.Duration
	HandshakeIdleTimeout time.Duration
	KeepAlivePeriod      time.Duration

	PreflightFunc func(ctx context.Context) (ProbeFailReason, bool)
}

type tpuquicDialFunc func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error)

type tpuquicConn interface {
	ConnectionStats() quic.ConnectionStats
	IsClosed() bool
	Close() error
}

func NewTPUQUICProbeTarget(log *slog.Logger, iface string, addr string, cfg *TPUQUICProbeTargetConfig) (*TPUQUICProbeTarget, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if cfg == nil {
		cfg = &TPUQUICProbeTargetConfig{}
	}
	return &TPUQUICProbeTarget{
		log:   log,
		cfg:   cfg,
		iface: iface,
		addr:  addr,
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			return tpuquic.DialWithRetry(ctx, addr, cfg, nil)
		},
	}, nil
}

func (t *TPUQUICProbeTarget) ID() ProbeTargetID {
	return ProbeTargetID(fmt.Sprintf("tpuquic/%s/%s", t.iface, t.addr))
}

func (t *TPUQUICProbeTarget) Interface() string {
	return t.iface
}

func (t *TPUQUICProbeTarget) Addr() string {
	return t.addr
}

func (t *TPUQUICProbeTarget) Close() {
	if t.conn == nil {
		return
	}
	_ = t.conn.Close()
	t.mu.Lock()
	t.conn = nil
	t.mu.Unlock()
}

// Probe dials the given address and returns the results.
// NOTE: This assumes the caller has configured a timeout context.
func (t *TPUQUICProbeTarget) Probe(ctx context.Context) (*ProbeResult, error) {
	if t.cfg.PreflightFunc != nil {
		res, ok := t.cfg.PreflightFunc(ctx)
		if !ok {
			return &ProbeResult{
				FailReason: res,
			}, nil
		}
	}

	_, err := t.dialIfNeeded(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "timeout: no recent network activity") {
			return &ProbeResult{
				FailReason: ProbeFailReasonTimeout,
				FailError:  err,
			}, nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &ProbeResult{
				FailReason: ProbeFailReasonTimeout,
				FailError:  err,
			}, nil
		}
		return &ProbeResult{
			FailReason: ProbeFailReasonOther,
			FailError:  err,
		}, nil
	}
	if t.conn == nil {
		return &ProbeResult{
			FailReason: ProbeFailReasonOther,
			FailError:  errors.New("connection is nil after dialing"),
		}, nil
	}

	stats := t.conn.ConnectionStats()
	packetsLost := max(stats.PacketsSent-stats.PacketsReceived, 0)
	res := &ProbeResult{
		Stats: &ProbeStats{
			PacketsSent: stats.PacketsSent,
			PacketsRecv: stats.PacketsReceived,
			PacketsLost: packetsLost,
			LossRatio:   safeDivide(float64(packetsLost), float64(stats.PacketsSent)),
			RTTMin:      stats.MinRTT,
			RTTAvg:      stats.SmoothedRTT,
			RTTStdDev:   stats.MeanDeviation,
		},
	}

	// Wait for stats to be ready for some bounded amount of time.
	var interval = 1 * time.Second
	if t.cfg.KeepAlivePeriod > 0 {
		interval = t.cfg.KeepAlivePeriod
	}
	var timeout time.Duration
	deadline, ok := ctx.Deadline()
	if !ok {
		timeout = defaultTPUQUICStatsReadyTimeout
	} else {
		timeout = time.Until(deadline)
	}
	err = Until(ctx, func() (bool, error) {
		stats = t.conn.ConnectionStats()
		if quicStatsNotReady(stats) {
			return false, nil
		}
		return true, nil
	}, timeout, interval)
	if err != nil {
		res.FailReason = ProbeFailReasonNotReady
		res.FailError = errors.New("stats not ready")
		return res, nil
	}

	// Return error if no packets were received; complete loss.
	if stats.PacketsSent > 0 && stats.PacketsReceived == 0 {
		res.FailReason = ProbeFailReasonPacketsLost
		res.FailError = errors.New("no packets received")
		return res, nil
	}

	res.OK = true
	return res, nil
}

func (t *TPUQUICProbeTarget) dialIfNeeded(ctx context.Context) (bool, error) {
	t.mu.Lock()
	if t.conn == nil || t.conn.IsClosed() {
		if t.conn != nil {
			_ = t.conn.Close()
		}
		t.mu.Unlock()
		var err error
		conn, err := t.dialFunc(ctx, t.addr, &tpuquic.DialConfig{
			Interface:            t.iface,
			MaxIdleTimeout:       t.cfg.MaxIdleTimeout,
			HandshakeIdleTimeout: t.cfg.HandshakeIdleTimeout,
			KeepAlivePeriod:      t.cfg.KeepAlivePeriod,
		})
		if err != nil {
			metrics.TPUQUICDialsTotal.WithLabelValues(metricsPathFromIface(t.iface), "error").Inc()
			return false, err
		}
		t.mu.Lock()
		t.conn = conn
		t.mu.Unlock()

		metrics.TPUQUICDialsTotal.WithLabelValues(metricsPathFromIface(t.iface), "ok").Inc()

		return true, nil
	}
	t.mu.Unlock()
	return false, nil
}

// statsNotReady returns true when the stats still look like the initial defaults:
func quicStatsNotReady(stats quic.ConnectionStats) bool {
	if stats.MeanDeviation == 0 && stats.LatestRTT == 100*time.Millisecond && stats.PacketsReceived < 15 {
		return true
	}
	if stats.LatestRTT == 0 || stats.PacketsSent == 0 {
		return true
	}
	return false
}
