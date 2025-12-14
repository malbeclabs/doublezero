package gm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

const (
	defaultICMPCount    = 3
	defaultICMPInterval = 1 * time.Second
	defaultICMPSize     = 56 // 64 bytes - 8 byte ICMP header
)

type ICMPProbeTarget struct {
	log *slog.Logger
	cfg *ICMPProbeTargetConfig

	iface string
	ip    net.IP
}

type ICMPProbeTargetConfig struct {
	Count    int
	Interval time.Duration

	PreflightFunc func(ctx context.Context) (ProbeFailReason, bool)
}

func NewICMPProbeTarget(log *slog.Logger, iface string, ip net.IP, cfg *ICMPProbeTargetConfig) (*ICMPProbeTarget, error) {
	if log == nil {
		return nil, fmt.Errorf("log is nil")
	}
	if iface == "" {
		return nil, fmt.Errorf("iface is required")
	}
	if ip == nil {
		return nil, fmt.Errorf("ip is required")
	}
	if cfg == nil {
		cfg = &ICMPProbeTargetConfig{}
	}
	return &ICMPProbeTarget{
		log:   log,
		cfg:   cfg,
		iface: iface,
		ip:    ip,
	}, nil
}

func (t *ICMPProbeTarget) ID() ProbeTargetID {
	return ProbeTargetID(fmt.Sprintf("icmp/%s/%s", t.iface, t.ip.String()))
}

func (t *ICMPProbeTarget) Interface() string {
	return t.iface
}

func (t *ICMPProbeTarget) IP() net.IP {
	return t.ip
}

// Probe pings the given address and returns the results.
// NOTE: This assumes the caller has configured a timeout context.
func (t *ICMPProbeTarget) Probe(ctx context.Context) (*ProbeResult, error) {
	if t.cfg.PreflightFunc != nil {
		res, ok := t.cfg.PreflightFunc(ctx)
		if !ok {
			return &ProbeResult{
				FailReason: res,
			}, nil
		}
	}

	pinger, err := probing.NewPinger(t.ip.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger: %w", err)
	}
	defer pinger.Stop()
	pinger.SetPrivileged(true)

	if t.cfg.Count <= 0 {
		t.cfg.Count = defaultICMPCount
	}
	if t.cfg.Interval <= 0 {
		t.cfg.Interval = defaultICMPInterval
	}

	pinger.InterfaceName = t.iface
	pinger.Count = t.cfg.Count
	pinger.Interval = t.cfg.Interval
	pinger.Size = defaultICMPSize

	if err := pinger.RunWithContext(ctx); err != nil {
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

	stats := pinger.Statistics()
	packetsLost := max(stats.PacketsSent-stats.PacketsRecv, 0)
	res := &ProbeResult{
		Stats: &ProbeStats{
			PacketsSent: uint64(stats.PacketsSent),
			PacketsRecv: uint64(stats.PacketsRecv),
			PacketsLost: uint64(packetsLost),
			LossRatio:   safeDivide(float64(packetsLost), float64(stats.PacketsSent)),
			RTTMin:      stats.MinRtt,
			RTTMax:      stats.MaxRtt,
			RTTAvg:      stats.AvgRtt,
			RTTStdDev:   stats.StdDevRtt,
		},
	}

	if icmpStatsNotReady(stats) {
		res.FailReason = ProbeFailReasonNotReady
		res.FailError = errors.New("stats not ready")
		return res, nil
	}

	// Return error if no packets were received; complete loss.
	if stats.PacketsSent > 0 && stats.PacketsRecv == 0 {
		res.FailReason = ProbeFailReasonPacketsLost
		res.FailError = errors.New("no packets received")
		return res, nil
	}

	res.OK = true
	return res, nil
}

func (t *ICMPProbeTarget) Close() {}

// statsNotReady returns true when the stats still look like the initial defaults:
func icmpStatsNotReady(stats *probing.Statistics) bool {
	if stats == nil {
		return true
	}
	return stats.PacketsSent == 0 || (stats.PacketsRecv > 0 && stats.AvgRtt == 0)
}
