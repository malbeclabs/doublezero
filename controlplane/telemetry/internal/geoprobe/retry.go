package geoprobe

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// isBindError reports whether err is a transient socket bind failure. The
// kernel can return EINVAL from bind(2) under load when many goroutines
// create ephemeral sockets concurrently; retrying typically succeeds.
func isBindError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "bind:")
}

// retryOnBindError calls fn up to senderRetries times, retrying with
// exponential backoff (senderRetryMin * 2^attempt) only when fn returns an
// error classified by isBindError. Non-bind errors are returned immediately.
func retryOnBindError[T any](ctx context.Context, log *slog.Logger, fn func() (T, error)) (T, error) {
	var (
		zero    T
		lastErr error
	)
	for attempt := range senderRetries {
		v, err := fn()
		if err == nil {
			return v, nil
		}
		lastErr = err
		if !isBindError(err) {
			return zero, err
		}
		delay := senderRetryMin * time.Duration(1<<attempt)
		log.Warn("Bind failed, retrying", "attempt", attempt+1, "delay", delay, "error", err)
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
	return zero, lastErr
}
