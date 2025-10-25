package probing

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/tools/uping/pkg/uping"
	promprobing "github.com/prometheus-community/pro-bing"
)

func DefaultListenFunc(log *slog.Logger, iface string, src net.IP) ListenFunc {
	return func(ctx context.Context) error {
		listener, err := uping.NewListener(uping.ListenerConfig{
			Logger:    log,
			Interface: iface,
			IP:        src,
		})
		if err != nil {
			return err
		}
		return listener.Listen(ctx)
	}
}

func DefaultProbeFunc(log *slog.Logger, iface string, timeout time.Duration) ProbeFunc {
	return func(ctx context.Context, route *routing.Route) (ProbeResult, error) {
		log.Debug("probing: sending route probe", "route", route.String())

		pinger, err := promprobing.NewPinger(route.Dst.IP.String())
		if err != nil {
			return ProbeResult{}, fmt.Errorf("error creating route probe pinger: %w", err)
		}
		pinger.Count = 1
		pinger.Timeout = timeout
		pinger.Source = route.Src.String()
		pinger.InterfaceName = iface

		err = pinger.RunWithContext(ctx)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("probing: error probing route: %w", err)
		}

		stats := pinger.Statistics()
		ok := stats.PacketsSent > 0 && stats.PacketsRecv == stats.PacketsSent
		return ProbeResult{
			OK:       ok,
			Sent:     stats.PacketsSent,
			Received: stats.PacketsRecv,
		}, nil
	}
}
