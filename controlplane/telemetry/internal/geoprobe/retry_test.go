package geoprobe

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIsBindError(t *testing.T) {
	t.Parallel()

	assert.False(t, isBindError(nil))
	assert.False(t, isBindError(errors.New("connection refused")))
	assert.True(t, isBindError(errors.New("listen udp4 0.0.0.0:0: bind: invalid argument")))
	assert.True(t, isBindError(errors.New("bind: address already in use")))
}

func TestRetryOnBindError_SucceedsAfterTransientBindFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	fn := func() (string, error) {
		n := calls.Add(1)
		if n == 1 {
			return "", errors.New("listen udp4 0.0.0.0:0: bind: invalid argument")
		}
		return "ok", nil
	}

	got, err := retryOnBindError(context.Background(), quietLogger(), fn)
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
	assert.Equal(t, int32(2), calls.Load())
}

func TestRetryOnBindError_FailsFastOnNonBindError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	nonBind := errors.New("connection refused")
	fn := func() (string, error) {
		calls.Add(1)
		return "", nonBind
	}

	got, err := retryOnBindError(context.Background(), quietLogger(), fn)
	require.ErrorIs(t, err, nonBind)
	assert.Empty(t, got)
	assert.Equal(t, int32(1), calls.Load(), "non-bind errors must not be retried")
}

func TestRetryOnBindError_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	bind := errors.New("bind: invalid argument")
	fn := func() (string, error) {
		calls.Add(1)
		return "", bind
	}

	_, err := retryOnBindError(context.Background(), quietLogger(), fn)
	require.ErrorIs(t, err, bind)
	assert.Equal(t, int32(senderRetries), calls.Load())
}

func TestRetryOnBindError_HonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fn := func() (string, error) {
		return "", errors.New("bind: invalid argument")
	}

	start := time.Now()
	_, err := retryOnBindError(ctx, quietLogger(), fn)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), senderRetryMin, "should return promptly when context is already cancelled")
}
