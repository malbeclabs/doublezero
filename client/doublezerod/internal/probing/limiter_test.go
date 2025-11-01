package probing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProbing_SemaphoreLimiter_New_ValidationAndString(t *testing.T) {
	t.Parallel()

	_, err := NewSemaphoreLimiter(0)
	require.Error(t, err)

	l, err := NewSemaphoreLimiter(3)
	require.NoError(t, err)
	require.Contains(t, l.String(), "SemaphoreLimiter")
	require.Contains(t, l.String(), "maxConcurrency=3")
}

func TestProbing_SemaphoreLimiter_BasicAcquireRelease(t *testing.T) {
	t.Parallel()

	l, err := NewSemaphoreLimiter(2)
	require.NoError(t, err)

	// Acquire twice should succeed immediately.
	rel1, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel1)

	rel2, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel2)

	// Third acquire should block until a release happens.
	acquired := make(chan struct{})
	var rel3 func()

	go func() {
		r, ok := l.Acquire(context.Background())
		if ok {
			rel3 = r
			close(acquired)
		}
	}()

	// Ensure it does NOT acquire within a short window (still blocked).
	select {
	case <-acquired:
		t.Fatalf("third Acquire should be blocked until a release")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	// Release one permit; now the goroutine should acquire shortly.
	rel1()

	select {
	case <-acquired:
		// got the third permit
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("third Acquire did not succeed after a release")
	}

	// Cleanup remaining permits
	rel2()
	if rel3 != nil {
		rel3()
	}
}

func TestProbing_SemaphoreLimiter_CancelWhileBlocked(t *testing.T) {
	t.Parallel()

	l, err := NewSemaphoreLimiter(1)
	require.NoError(t, err)

	// Fill the single permit.
	rel, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel)

	// Attempt another acquire with a short timeout; it should fail when context expires.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var gotOK bool
	var gotRel func()

	go func() {
		defer close(done)
		gotRel, gotOK = l.Acquire(ctx)
	}()

	select {
	case <-done:
		require.False(t, gotOK, "Acquire should return ok=false when context times out")
		require.Nil(t, gotRel)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Acquire did not return after context timeout")
	}

	// Release original permit to avoid leaks.
	rel()
}

func TestProbing_SemaphoreLimiter_SequentialAcquireReleaseCycles(t *testing.T) {
	t.Parallel()

	l, err := NewSemaphoreLimiter(2)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		relA, ok := l.Acquire(context.Background())
		require.True(t, ok)
		require.NotNil(t, relA)

		relB, ok := l.Acquire(context.Background())
		require.True(t, ok)
		require.NotNil(t, relB)

		// A third should block; verify it doesn’t acquire within 20ms.
		acquired := make(chan struct{})
		go func() {
			if r, ok := l.Acquire(context.Background()); ok {
				r() // immediately release if we ever acquire (we shouldn't before the test releases)
				close(acquired)
			}
		}()
		select {
		case <-acquired:
			t.Fatalf("Acquire should not succeed before a release (iteration %d)", i)
		case <-time.After(20 * time.Millisecond):
		}

		// Release both and ensure we can acquire twice again.
		relA()
		relB()

		relC, ok := l.Acquire(context.Background())
		require.True(t, ok)
		require.NotNil(t, relC)
		relD, ok := l.Acquire(context.Background())
		require.True(t, ok)
		require.NotNil(t, relD)
		relC()
		relD()
	}
}
