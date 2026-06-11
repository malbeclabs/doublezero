// Package abort polls a sentinel file on disk and cancels a context when the
// file appears. The orchestrator uses this for cooperative shutdown: an
// operator drops a file at the path passed via --abort-file and the running
// sweep finishes the current user iteration before exiting.
package abort

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// Default polling cadence. The sweep loop only checks the cancellation between
// user iterations, so the abort signal latency is bounded by min(this, one
// user iteration).
const DefaultPollInterval = 250 * time.Millisecond

// Watch returns a derived context that cancels as soon as `path` exists on
// disk. If path is empty the returned context is the parent verbatim and the
// returned stop is a no-op. The watcher goroutine exits when parent or the
// returned context is cancelled.
//
// Pass log=nil for silent operation.
func Watch(parent context.Context, path string, interval time.Duration, log *slog.Logger) (context.Context, context.CancelFunc) {
	if path == "" {
		return parent, func() {}
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if exists(path) {
					if log != nil {
						log.Warn("abort file detected; cancelling sweep", "path", path)
					}
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

// exists reports whether path refers to an existing filesystem entry. Any
// stat error other than ENOENT is treated as "exists" so a permission error
// doesn't silently leave the orchestrator running past an operator abort.
func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !errors.Is(err, os.ErrNotExist)
}
