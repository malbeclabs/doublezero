//go:build linux

package probing

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/tools/uping/pkg/uping"
	promprobing "github.com/prometheus-community/pro-bing"
)

// DefaultListenFunc returns a ListenFunc that starts an ICMP listener bound to iface/src.
// It blocks until the context is canceled or a fatal error occurs.
func DefaultListenFunc(log *slog.Logger, iface string, src net.IP) ListenFunc {
	return func(ctx context.Context) error {
		l, err := uping.NewListener(uping.ListenerConfig{
			Logger:    log,
			Interface: iface,
			IP:        src,
		})
		if err != nil {
			return err
		}
		return l.Listen(ctx)
	}
}
func DefaultListenFuncWithRetry(log *slog.Logger, iface string, src net.IP, opts ...backoff.ExponentialBackOffOpts) ListenFunc {
	base := DefaultListenFunc(log, iface, src)

	opts = append([]backoff.ExponentialBackOffOpts{
		backoff.WithInitialInterval(100 * time.Millisecond),
		backoff.WithMultiplier(2.0),
		backoff.WithMaxInterval(5 * time.Second),
		backoff.WithMaxElapsedTime(1 * time.Minute), // stop retrying after a minute…
		backoff.WithRandomizationFactor(0),          // deterministic (no jitter)
	}, opts...)
	return func(ctx context.Context) error {
		b := backoff.NewExponentialBackOff(opts...)

		bo := backoff.WithContext(b, ctx) // …and also stop on ctx cancel/timeout

		op := func() error {
			err := base(ctx)
			// If you detect a non-retryable error, wrap it:
			// if errors.Is(err, uping.ErrInvalidInterface) { return backoff.Permanent(err) }
			return err
		}

		return backoff.Retry(op, bo)
	}
}

// DefaultProbeFunc returns a ProbeFunc that sends a single ICMP echo request
// using the Prometheus pro-bing package. It uses the provided interface and
// per-probe timeout, returning basic packet statistics as a ProbeResult.
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
