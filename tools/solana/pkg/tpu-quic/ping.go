package tpuquic

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/quic-go/quic-go"
)

const (
	DefaultCount           = 3
	DefaultInterval        = 3 * time.Second
	DefaultKeepAlivePeriod = 500 * time.Millisecond
)

type PingConfig struct {
	DialConfig

	Count    int
	Interval time.Duration

	Clock clockwork.Clock
}

type PingResult struct {
	ConnectionStats []quic.ConnectionStats `json:"connection_stats"`
	Success         bool                   `json:"success"`
	Error           error                  `json:"error"`
}

func (cfg *PingConfig) Validate() error {
	if err := cfg.DialConfig.Validate(); err != nil {
		return fmt.Errorf("failed to validate dial config: %w", err)
	}
	if cfg.DialConfig.KeepAlivePeriod == 0 {
		// This is necessary or else we won't see real stats on the connection.
		cfg.DialConfig.KeepAlivePeriod = DefaultKeepAlivePeriod
	}
	if cfg.Count == 0 {
		cfg.Count = DefaultCount
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

func Ping(ctx context.Context, log *slog.Logger, dst string, cfg PingConfig) (*PingResult, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate run config: %w", err)
	}

	conn, err := Dial(ctx, dst, &cfg.DialConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial TPU QUIC: %w", err)
	}
	defer conn.Close()

	res := &PingResult{
		ConnectionStats: make([]quic.ConnectionStats, 0, cfg.Count),
		Success:         true,
		Error:           nil,
	}

	ticker := NewMaybeTick(cfg.Interval, cfg.Clock)
	defer ticker.Stop()

	for i := 0; i < cfg.Count; i++ {
		select {
		case <-ctx.Done():
			log.Info("QUIC ping cancelled", "count", i+1)
			stats := conn.ConnectionStats()
			tick(log, i, stats)
			res.ConnectionStats = append(res.ConnectionStats, stats)
			return res, nil
		case <-ticker.C:
			stats := conn.ConnectionStats()
			tick(log, i, stats)
			res.ConnectionStats = append(res.ConnectionStats, stats)
		}
	}

	return res, nil
}

func tick(log *slog.Logger, i int, stats quic.ConnectionStats) {
	log.Info("QUIC stats",
		"interval", i+1,
		"rttMin", stats.MinRTT,
		"rttLatest", stats.LatestRTT,
		"rttSmoothed", stats.SmoothedRTT,
		"rttDev", stats.MeanDeviation,
		"sentBytes", stats.BytesSent,
		"sentPackets", stats.PacketsSent,
		"recvBytes", stats.BytesReceived,
		"recvPackets", stats.PacketsReceived,
		"lostBytes", stats.BytesLost,
		"lostPackets", stats.PacketsLost,
	)
}

type MaybeTick struct {
	C <-chan time.Time
	t clockwork.Ticker
}

func NewMaybeTick(d time.Duration, clock clockwork.Clock) *MaybeTick {
	if d == 0 {
		return &MaybeTick{C: nil, t: nil}
	}
	t := clock.NewTicker(d)
	return &MaybeTick{
		C: t.Chan(),
		t: t,
	}
}

func (m *MaybeTick) Stop() {
	if m.t != nil {
		m.t.Stop()
	}
}
