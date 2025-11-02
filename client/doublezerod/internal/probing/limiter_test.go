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

	rel1, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel1)

	rel2, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel2)

	acquired := make(chan struct{})
	var rel3 func()

	go func() {
		r, ok := l.Acquire(context.Background())
		if ok {
			rel3 = r
			close(acquired)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-acquired:
		case <-ctx.Done():
		}
		close(done)
	}()
	select {
	case <-done:
		require.NotNil(t, ctx.Err())
	default:
	}

	rel1()

	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("third Acquire did not succeed after a release")
	}

	rel2()
	if rel3 != nil {
		rel3()
	}
}

func TestProbing_SemaphoreLimiter_CancelWhileBlocked(t *testing.T) {
	t.Parallel()

	l, err := NewSemaphoreLimiter(1)
	require.NoError(t, err)

	rel, ok := l.Acquire(context.Background())
	require.True(t, ok)
	require.NotNil(t, rel)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	got := make(chan struct{})
	var gotOK bool
	var gotRel func()

	go func() {
		defer close(got)
		gotRel, gotOK = l.Acquire(ctx)
	}()

	select {
	case <-got:
		require.False(t, gotOK)
		require.Nil(t, gotRel)
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Acquire did not return after context timeout")
	}

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

		blockCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		acqDone := make(chan struct{})
		var acqOK bool
		go func() {
			defer close(acqDone)
			_, acqOK = l.Acquire(blockCtx)
		}()
		select {
		case <-acqDone:
			require.False(t, acqOK, "Acquire should not succeed before a release (iteration %d)", i)
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("Acquire did not return within timeout (iteration %d)", i)
		}

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
