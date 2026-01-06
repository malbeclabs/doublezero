package duck

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	maxRetries         = 8
	initialRetryDelay  = 50 * time.Millisecond
	maxRetryDelay      = 5 * time.Second
	retryBackoffFactor = 2.0
)

// isTransactionConflictError checks if an error is a transaction conflict error that should be retried
func isTransactionConflictError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "Transaction conflict") ||
		strings.Contains(errStr, "Failed to commit DuckLake transaction") ||
		strings.Contains(errStr, "but another transaction has compacted it")
}

// retryWithBackoff retries a function with exponential backoff if it returns a transaction conflict error
func retryWithBackoff(ctx context.Context, log *slog.Logger, operation string, fn func() error) error {
	var lastErr error
	delay := initialRetryDelay

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check context cancellation before each attempt
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("context cancelled after %d retries, last error: %w", attempt, lastErr)
			}
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		err := fn()
		if err == nil {
			if attempt > 0 {
				log.Info("operation succeeded after retries", "operation", operation, "attempts", attempt+1)
			}
			return nil
		}

		// Only retry on transaction conflict errors
		if !isTransactionConflictError(err) {
			return err
		}

		lastErr = err
		if attempt < maxRetries-1 {
			log.Warn("transaction conflict detected, retrying", "operation", operation, "attempt", attempt+1, "max_attempts", maxRetries, "delay", delay, "error", err)
			// Use context-aware sleep
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			case <-timer.C:
				// Timer expired, continue with retry
			}
			// Exponential backoff with max cap
			delay = time.Duration(float64(delay) * retryBackoffFactor)
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", maxRetries, lastErr)
}
