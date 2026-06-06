package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
)

// metricsListenerInitialBackoff is the first sleep between retry attempts; the
// delay doubles after each failure and is capped at metricsListenerMaxBackoff.
// Picked small so that a startup race against namespace-IP assignment (the case
// from the incident) recovers in ~1s without burning startup time.
const (
	metricsListenerInitialBackoff = 1 * time.Second
	metricsListenerMaxBackoff     = 30 * time.Second
)

// listenFunc is the surface listenWithRetry uses to open the metrics-server
// socket. In production it is plainListen / namespacedListen below; tests inject
// a fake so the retry/classifier logic can be exercised without root.
type listenFunc func() (net.Listener, error)

// isRetryableBindError reports whether err is a transient bind failure that
// should be retried. EADDRNOTAVAIL is the incident's failure mode: the
// namespace exists but its IP has not been assigned yet, and self-heals within
// seconds. EADDRINUSE is also retried because a fresh restart can race the
// kernel's teardown of the previous listener's socket (TIME_WAIT can take up
// to 2*MSL). The trade-off: a genuine port conflict (e.g., two daemons
// configured for the same metrics-addr) will retry forever, surfacing only as
// repeating WARN log lines rather than a hard exit — operators should monitor
// for sustained warnings on this path. Everything else surfaces immediately
// so a real configuration problem (parse error, unknown namespace, etc.) is
// not silently retried.
func isRetryableBindError(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EADDRINUSE)
}

// listenWithRetry opens the metrics-server listener, retrying transient bind
// failures with capped exponential backoff. It returns when the bind succeeds,
// when a non-retryable error occurs, or when ctx is cancelled.
//
// Backoff: 1s, 2s, 4s, 8s, 16s, 30s, 30s, ... until ctx.Done().
//
// Retries are unbounded by design. The agent has no useful action it can take
// if the namespace IP never appears; giving up would only restore the pre-fix
// behavior of running silently without /metrics.
func listenWithRetry(ctx context.Context, log *slog.Logger, listen listenFunc) (net.Listener, error) {
	delay := metricsListenerInitialBackoff
	var lastErr error
	for attempt := 1; ; attempt++ {
		listener, err := listen()
		if err == nil {
			return listener, nil
		}
		if !isRetryableBindError(err) {
			return nil, err
		}
		lastErr = err
		log.Warn("Failed to bind prometheus metrics listener, retrying",
			"attempt", attempt, "delay", delay, "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("context cancelled while retrying metrics listener bind (last error: %w): %w", lastErr, ctx.Err())
		case <-timer.C:
		}
		delay = nextBackoff(delay)
	}
}

// nextBackoff returns the next backoff delay: double the current one, clamped
// at metricsListenerMaxBackoff. Once the cap is reached, subsequent calls
// return the cap unchanged.
func nextBackoff(current time.Duration) time.Duration {
	if current >= metricsListenerMaxBackoff {
		return metricsListenerMaxBackoff
	}
	doubled := current * 2
	if doubled > metricsListenerMaxBackoff {
		return metricsListenerMaxBackoff
	}
	return doubled
}

// plainListen returns a listenFunc that binds addr in the current network
// namespace.
func plainListen(addr string) listenFunc {
	return func() (net.Listener, error) {
		return net.Listen("tcp", addr)
	}
}

// namespacedListen returns a listenFunc that binds addr inside nsName via
// netns.RunInNamespace.
func namespacedListen(addr, nsName string) listenFunc {
	return func() (net.Listener, error) {
		return netns.RunInNamespace(nsName, func() (net.Listener, error) {
			return net.Listen("tcp", addr)
		})
	}
}
