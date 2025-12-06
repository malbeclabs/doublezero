package gm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/iterutil"
	tpuquic "github.com/malbeclabs/doublezero/tools/solana/pkg/tpu-quic"
	"github.com/quic-go/quic-go"
)

var (
	ErrFailedToDial = errors.New("failed to dial validator tpu quic")

	// ErrStatsNotReady is returned when the QUIC connection has only initial,
	// placeholder statistics (e.g., immediately after dialing). Probe still
	// returns the partial ConnectionStats so callers can inspect them or retry.
	ErrStatsNotReady = errors.New("stats are not ready yet")
)

// tpuquicProber maintains cached QUIC connections to validator TPU addresses
// and exposes a Probe method for retrieving connection-level stats.
type TPUQUICProber struct {
	log *slog.Logger

	dialFunc    tpuquicDialFunc
	connsByAddr map[string]tpuquicConn
	mu          sync.Mutex
}

type tpuquicDialFunc func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error)

type tpuquicConn interface {
	ConnectionStats() quic.ConnectionStats
	IsClosed() bool
	Close() error
}

func NewTPUQUICProber(log *slog.Logger) (*TPUQUICProber, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	return &TPUQUICProber{
		log: log,
		dialFunc: func(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, error) {
			return tpuquic.DialWithRetry(ctx, addr, cfg, nil)
		},
		connsByAddr: make(map[string]tpuquicConn),
	}, nil
}

// Close closes all connections.
func (p *TPUQUICProber) Close() error {
	p.mu.Lock()
	conns := make([]tpuquicConn, 0, len(p.connsByAddr))
	for _, c := range p.connsByAddr {
		conns = append(conns, c)
	}
	p.connsByAddr = make(map[string]tpuquicConn)
	p.mu.Unlock()

	var err error
	for _, c := range conns {
		if e := c.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Prune closes and removes connections that are no longer in the given list of addresses.
func (p *TPUQUICProber) Prune(addrs iter.Seq[string]) {
	addrSet := iterutil.CollectSet(addrs)

	var connsToClose []tpuquicConn

	p.mu.Lock()
	for addr, conn := range p.connsByAddr {
		if _, ok := addrSet[addr]; !ok {
			connsToClose = append(connsToClose, conn)
			delete(p.connsByAddr, addr)
		}
	}
	p.mu.Unlock()

	for _, c := range connsToClose {
		_ = c.Close()
	}
}

type TPUQUICProbeConfig struct {
	Timeout        time.Duration
	DialConfig     *tpuquic.DialConfig
	DelayAfterDial time.Duration // Sleep after dialing to avoid getting stats that are not yet ready.
}

// Probe dials the given address if not already connected and returns the connection stats.
func (p *TPUQUICProber) Probe(ctx context.Context, addr string, cfg TPUQUICProbeConfig) (*quic.ConnectionStats, error) {
	timeoutCtx := ctx
	cancel := func() {}
	if cfg.Timeout > 0 {
		timeoutCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
	}
	defer cancel()

	conn, dialed, err := p.dialOrGet(timeoutCtx, addr, cfg.DialConfig)
	if err != nil {
		return nil, errors.Join(ErrFailedToDial, err)
	}

	if dialed && cfg.DelayAfterDial > 0 {
		select {
		case <-time.After(cfg.DelayAfterDial):
		case <-timeoutCtx.Done():
			return nil, timeoutCtx.Err()
		}
	}

	stats := conn.ConnectionStats()

	if statsNotReady(stats) {
		return &stats, ErrStatsNotReady
	}

	return &stats, nil
}

// dialOrGet dials the given address if not already connected and returns the connection.
// If the connection is already established, it is returned without dialing again.
// The bool return value indicates if the connection was dialed (true) or retrieved (false).
func (p *TPUQUICProber) dialOrGet(ctx context.Context, addr string, cfg *tpuquic.DialConfig) (tpuquicConn, bool, error) {
	p.mu.Lock()
	conn, ok := p.connsByAddr[addr]
	if !ok || conn.IsClosed() {
		if conn != nil {
			_ = conn.Close()
		}
		p.mu.Unlock()
		var err error
		conn, err = p.dialFunc(ctx, addr, cfg)
		if err != nil {
			return nil, false, err
		}
		p.mu.Lock()
		p.connsByAddr[addr] = conn
		p.mu.Unlock()

		return conn, true, nil
	}
	p.mu.Unlock()
	return conn, false, nil
}

// statsNotReady returns true when the stats still look like the initial defaults:
func statsNotReady(stats quic.ConnectionStats) bool {
	if stats.MeanDeviation == 0 && stats.LatestRTT == 100*time.Millisecond && stats.PacketsReceived < 15 {
		return true
	}
	if stats.LatestRTT == 0 || stats.PacketsSent == 0 {
		return true
	}
	return false
}
