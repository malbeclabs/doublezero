package probing

import (
	"context"
	"errors"
	"fmt"
)

// Limiter controls concurrent access to a shared resource.
// Acquire blocks (or respects ctx cancellation) until a slot is available,
// returning a release function to free the slot once work is complete.
type Limiter interface {
	Acquire(ctx context.Context) (release func(), ok bool)
	String() string
}

// SemaphoreLimiter implements Limiter using a bounded semaphore.
// It enforces a maximum number of concurrent operations.
type SemaphoreLimiter struct {
	maxConcurrency uint
	sem            chan struct{}
}

// String returns a descriptive name for the limiter, including its capacity.
func (l *SemaphoreLimiter) String() string {
	return fmt.Sprintf("SemaphoreLimiter(maxConcurrency=%d)", l.maxConcurrency)
}

// NewSemaphoreLimiter constructs a new semaphore-based limiter.
// maxConcurrency must be > 0 or an error is returned.
func NewSemaphoreLimiter(maxConcurrency uint) (*SemaphoreLimiter, error) {
	if maxConcurrency == 0 {
		return nil, errors.New("maxConcurrency must be > 0")
	}
	return &SemaphoreLimiter{
		maxConcurrency: maxConcurrency,
		sem:            make(chan struct{}, int(maxConcurrency)),
	}, nil
}

// Acquire reserves one concurrency slot, blocking until available or ctx is canceled.
// It returns a release function that must be called to free the slot.
func (l *SemaphoreLimiter) Acquire(ctx context.Context) (func(), bool) {
	select {
	case l.sem <- struct{}{}:
		return func() { <-l.sem }, true
	case <-ctx.Done():
		return nil, false
	}
}
