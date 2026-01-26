package retry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLake_Retry_DefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.BaseBackoff != 500*time.Millisecond {
		t.Errorf("expected BaseBackoff=500ms, got %v", cfg.BaseBackoff)
	}
	if cfg.MaxBackoff != 5*time.Second {
		t.Errorf("expected MaxBackoff=5s, got %v", cfg.MaxBackoff)
	}
}

func TestLake_Retry_Do_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := DefaultConfig()

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestLake_Retry_Do_SuccessAfterRetries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := Config{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  100 * time.Millisecond,
	}

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("connection reset")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestLake_Retry_Do_ExhaustsAllAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := Config{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  100 * time.Millisecond,
	}

	attempts := 0
	originalErr := errors.New("connection reset")
	err := Do(ctx, cfg, func() error {
		attempts++
		return originalErr
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if !errors.Is(err, originalErr) {
		t.Errorf("expected wrapped original error, got %v", err)
	}
}

func TestLake_Retry_Do_NonRetryableError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := Config{
		MaxAttempts: 3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  100 * time.Millisecond,
	}

	attempts := 0
	originalErr := errors.New("invalid input")
	err := Do(ctx, cfg, func() error {
		attempts++
		return originalErr
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (non-retryable), got %d", attempts)
	}
	if err != originalErr {
		t.Errorf("expected original error, got %v", err)
	}
}

func TestLake_Retry_Do_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cfg := Config{
		MaxAttempts: 5,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  1 * time.Second,
	}

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return errors.New("connection reset")
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts before cancellation, got %d", attempts)
	}
}

func TestLake_Retry_Do_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := Config{
		MaxAttempts: 5,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  1 * time.Second,
	}

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		time.Sleep(60 * time.Millisecond) // Longer than context timeout
		return errors.New("connection reset")
	})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestLake_Retry_IsRetryable_NetworkErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "timeout error",
			err:  &net.OpError{Op: "read", Err: errors.New("i/o timeout")},
			want: true,
		},
		{
			name: "connection reset",
			err:  errors.New("connection reset by peer"),
			want: true,
		},
		{
			name: "EOF",
			err:  errors.New("EOF"),
			want: true,
		},
		{
			name: "broken pipe",
			err:  errors.New("broken pipe"),
			want: true,
		},
		{
			name: "connection closed",
			err:  errors.New("connection closed"),
			want: true,
		},
		{
			name: "client is closing",
			err:  errors.New("client is closing"),
			want: true,
		},
		{
			name: "timeout in message",
			err:  errors.New("operation timeout"),
			want: true,
		},
		{
			name: "rate limit",
			err:  errors.New("rate limit exceeded"),
			want: true,
		},
		{
			name: "too many requests",
			err:  errors.New("too many requests"),
			want: true,
		},
		{
			name: "service unavailable",
			err:  errors.New("service unavailable"),
			want: true,
		},
		{
			name: "temporary failure",
			err:  errors.New("temporary failure"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLake_Retry_IsRetryable_HTTPStatusCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "429 Too Many Requests",
			err:  &httpError{statusCode: http.StatusTooManyRequests},
			want: true,
		},
		{
			name: "500 Internal Server Error",
			err:  &httpError{statusCode: http.StatusInternalServerError},
			want: true,
		},
		{
			name: "502 Bad Gateway",
			err:  &httpError{statusCode: http.StatusBadGateway},
			want: true,
		},
		{
			name: "503 Service Unavailable",
			err:  &httpError{statusCode: http.StatusServiceUnavailable},
			want: true,
		},
		{
			name: "504 Gateway Timeout",
			err:  &httpError{statusCode: http.StatusGatewayTimeout},
			want: true,
		},
		{
			name: "400 Bad Request",
			err:  &httpError{statusCode: http.StatusBadRequest},
			want: false,
		},
		{
			name: "404 Not Found",
			err:  &httpError{statusCode: http.StatusNotFound},
			want: false,
		},
		{
			name: "401 Unauthorized",
			err:  &httpError{statusCode: http.StatusUnauthorized},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLake_Retry_IsRetryable_ContextErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLake_Retry_IsRetryable_NilError(t *testing.T) {
	t.Parallel()
	if IsRetryable(nil) {
		t.Error("IsRetryable(nil) should return false")
	}
}

func TestLake_Retry_CalculateBackoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		base     time.Duration
		max      time.Duration
		attempt  int
		expected time.Duration
	}{
		{
			name:     "first retry (attempt 1)",
			base:     500 * time.Millisecond,
			max:      5 * time.Second,
			attempt:  1,
			expected: 1 * time.Second, // 500ms * 2^1
		},
		{
			name:     "second retry (attempt 2)",
			base:     500 * time.Millisecond,
			max:      5 * time.Second,
			attempt:  2,
			expected: 2 * time.Second, // 500ms * 2^2
		},
		{
			name:     "third retry (attempt 3)",
			base:     500 * time.Millisecond,
			max:      5 * time.Second,
			attempt:  3,
			expected: 4 * time.Second, // 500ms * 2^3
		},
		{
			name:     "exceeds max",
			base:     500 * time.Millisecond,
			max:      5 * time.Second,
			attempt:  4,
			expected: 5 * time.Second, // capped at max
		},
		{
			name:     "small base",
			base:     100 * time.Millisecond,
			max:      1 * time.Second,
			attempt:  1,
			expected: 200 * time.Millisecond, // 100ms * 2^1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calculateBackoff(tt.base, tt.max, tt.attempt)
			if got != tt.expected {
				t.Errorf("calculateBackoff(%v, %v, %d) = %v, want %v", tt.base, tt.max, tt.attempt, got, tt.expected)
			}
		})
	}
}

func TestLake_Retry_Do_BackoffTiming(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := Config{
		MaxAttempts: 3,
		BaseBackoff: 50 * time.Millisecond,
		MaxBackoff:  500 * time.Millisecond,
	}

	attempts := 0
	start := time.Now()
	err := Do(ctx, cfg, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("connection reset")
		}
		return nil
	})
	duration := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}

	// Should have waited for backoff between attempts
	// Attempt 1: immediate
	// Attempt 2: wait ~100ms (50ms * 2^1)
	// Attempt 3: wait ~200ms (50ms * 2^2)
	// Total: ~300ms minimum
	minExpected := 250 * time.Millisecond
	if duration < minExpected {
		t.Errorf("expected duration >= %v, got %v", minExpected, duration)
	}

	// Should not wait too long (with some buffer for test execution)
	maxExpected := 500 * time.Millisecond
	if duration > maxExpected {
		t.Errorf("expected duration <= %v, got %v", maxExpected, duration)
	}
}

// httpError is a test helper that implements StatusCode() for testing HTTP error detection
type httpError struct {
	statusCode int
}

func (e *httpError) Error() string {
	return http.StatusText(e.statusCode)
}

func (e *httpError) StatusCode() int {
	return e.statusCode
}
