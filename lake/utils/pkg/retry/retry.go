package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"
)

// Config holds retry configuration.
type Config struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

// DefaultConfig returns the default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts: 3,
		BaseBackoff: 500 * time.Millisecond,
		MaxBackoff:  5 * time.Second,
	}
}

// Do executes the given function with exponential backoff retry.
// Returns the last error if all attempts fail.
func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if attempt > 1 {
			backoff := calculateBackoff(cfg.BaseBackoff, cfg.MaxBackoff, attempt-1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Don't retry if error is not retryable
		if !IsRetryable(lastErr) {
			return lastErr
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation is not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Network errors are retryable
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
		// Connection errors are retryable
		if strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "EOF") ||
			strings.Contains(err.Error(), "broken pipe") ||
			strings.Contains(err.Error(), "connection reset") {
			return true
		}
	}

	// Check for HTTP status codes
	type hasStatusCode interface {
		StatusCode() int
	}
	var sc hasStatusCode
	if errors.As(err, &sc) {
		code := sc.StatusCode()
		switch code {
		case http.StatusTooManyRequests, // 429
			http.StatusInternalServerError, // 500
			http.StatusBadGateway,          // 502
			http.StatusServiceUnavailable,  // 503
			http.StatusGatewayTimeout:      // 504
			return true
		}
	}

	// Check error message for common retryable patterns
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"connection closed",
		"eof",
		"client is closing",
		"broken pipe",
		"connection reset",
		"timeout",
		"temporary failure",
		"service unavailable",
		"rate limit",
		"too many requests",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// calculateBackoff calculates exponential backoff with jitter.
// Formula: base * 2^attempt * (0.5 + rand(0, 0.5))
// Jitter prevents thundering herd when multiple clients retry simultaneously.
func calculateBackoff(base, max time.Duration, attempt int) time.Duration {
	// Exponential backoff: base * 2^attempt
	backoff := base * time.Duration(1<<uint(attempt))
	if backoff > max {
		backoff = max
	}
	// Add jitter: multiply by 0.5 to 1.0 (random factor)
	// This spreads out retries to prevent thundering herd
	jitter := 0.5 + rand.Float64()*0.5
	return time.Duration(float64(backoff) * jitter)
}
