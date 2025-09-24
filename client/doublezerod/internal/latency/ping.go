package latency

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	probing "github.com/prometheus-community/pro-bing"
)

func udpPing(ctx context.Context, log *slog.Logger, d serviceability.Device) LatencyResult {
	addr := net.IP(d.PublicIp[:]).String()
	p, err := probing.NewPinger(addr)
	if err != nil {
		log.Error("pinger create", "addr", addr, "err", err)
		return LatencyResult{Device: d}
	}

	p.SetPrivileged(true)

	p.Count = 3
	p.Interval = 1 * time.Second
	p.Timeout = 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		rem := time.Until(deadline)
		if rem <= 0 {
			return LatencyResult{Device: d}
		}
		p.Timeout = rem
		minInterval := 100 * time.Millisecond
		if rem < time.Second {
			p.Count = 1
			p.Interval = minInterval
		} else {
			iv := rem / time.Duration(p.Count+1)
			if iv < minInterval {
				iv = minInterval
			}
			p.Interval = iv
		}
	}

	done := make(chan struct{})
	go func() { _ = p.Run(); close(done) }()
	select {
	case <-ctx.Done():
		p.Stop()
		<-done
	case <-done:
	}

	stats := p.Statistics()
	res := LatencyResult{Device: d}
	res.Reachable = stats.PacketsRecv > 0
	res.Avg, res.Min, res.Max, res.Loss = int64(stats.AvgRtt), int64(stats.MinRtt), int64(stats.MaxRtt), stats.PacketLoss
	return res
}
