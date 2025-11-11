package retry

import (
	"context"
	"fmt"
	"time"
)

// Config holds retry configuration.
type Config struct {
	MaxAttempts int
	InitialWait time.Duration
}

// DefaultConfig returns the default retry configuration.
// Exponential backoff: 1s, 2s, 4s, 8s, 16s (5 attempts)
func DefaultConfig() Config {
	return Config{
		MaxAttempts: 5,
		InitialWait: 1 * time.Second,
	}
}

// Do executes the given function with exponential backoff retry.
// The backoff doubles after each attempt: 1s, 2s, 4s, 8s, 16s
func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't sleep after the last attempt
		if attempt < cfg.MaxAttempts {
			waitDuration := cfg.InitialWait * time.Duration(1<<(attempt-1))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitDuration):
				// Continue to next attempt
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}
