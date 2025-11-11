package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoSuccess(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig()

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("Do() failed: %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestDoRetryAndSucceed(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.InitialWait = 10 * time.Millisecond // Speed up test

	attempts := 0
	err := Do(ctx, cfg, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Do() failed: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoMaxAttemptsExceeded(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig()
	cfg.InitialWait = 10 * time.Millisecond // Speed up test

	attempts := 0
	testErr := errors.New("persistent error")
	err := Do(ctx, cfg, func() error {
		attempts++
		return testErr
	})

	if err == nil {
		t.Error("Do() should have failed after max attempts")
	}

	if attempts != cfg.MaxAttempts {
		t.Errorf("expected %d attempts, got %d", cfg.MaxAttempts, attempts)
	}

	if !errors.Is(err, testErr) {
		t.Errorf("expected error to wrap original error, got %v", err)
	}
}

func TestDoContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := DefaultConfig()
	cfg.InitialWait = 100 * time.Millisecond

	attempts := 0
	errChan := make(chan error, 1)

	go func() {
		err := Do(ctx, cfg, func() error {
			attempts++
			return errors.New("error")
		})
		errChan <- err
	}()

	// Cancel after first attempt
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errChan

	if err == nil {
		t.Error("Do() should have returned an error")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestDoExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		MaxAttempts: 3,
		InitialWait: 100 * time.Millisecond,
	}

	attempts := 0
	startTime := time.Now()
	var attemptTimes []time.Duration

	_ = Do(ctx, cfg, func() error {
		attempts++
		attemptTimes = append(attemptTimes, time.Since(startTime))
		return errors.New("error")
	})

	// Check that backoff is approximately exponential
	// First attempt: ~0ms
	// Second attempt: ~100ms (after 1x wait)
	// Third attempt: ~300ms (after 1x + 2x wait)

	if len(attemptTimes) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(attemptTimes))
	}

	// First attempt should be immediate
	if attemptTimes[0] > 50*time.Millisecond {
		t.Errorf("first attempt should be immediate, got %v", attemptTimes[0])
	}

	// Second attempt should be after ~100ms
	if attemptTimes[1] < 80*time.Millisecond || attemptTimes[1] > 150*time.Millisecond {
		t.Errorf("second attempt should be after ~100ms, got %v", attemptTimes[1])
	}

	// Third attempt should be after ~300ms (100 + 200)
	if attemptTimes[2] < 250*time.Millisecond || attemptTimes[2] > 400*time.Millisecond {
		t.Errorf("third attempt should be after ~300ms, got %v", attemptTimes[2])
	}
}
