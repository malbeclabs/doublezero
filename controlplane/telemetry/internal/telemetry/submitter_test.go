package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Submitter(t *testing.T) {
	t.Parallel()

	t.Run("submits_buffered_samples", func(t *testing.T) {
		t.Parallel()

		var received []telemetry.Sample
		submit := func(ctx context.Context, samples []telemetry.Sample) error {
			received = append(received, samples...)
			return nil
		}

		buffer := telemetry.NewSampleBuffer(10)
		buffer.Add(telemetry.Sample{
			Timestamp: time.Now(),
			Link:      "link1",
			Device:    "device1",
			RTT:       42 * time.Millisecond,
			Loss:      false,
		})

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:    10 * time.Millisecond,
			Buffer:      buffer,
			SubmitFunc:  submit,
			MaxAttempts: 1,
			BackoffFunc: func(_ int) time.Duration { return 0 },
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		require.NoError(t, submitter.Run(ctx))
		require.Len(t, received, 1)
	})

	t.Run("retries_on_transient_error", func(t *testing.T) {
		t.Parallel()

		var mu sync.Mutex
		var callCount int

		submit := func(ctx context.Context, samples []telemetry.Sample) error {
			mu.Lock()
			defer mu.Unlock()
			callCount++
			if callCount < 3 {
				return errors.New("temporary failure")
			}
			return nil
		}

		buffer := telemetry.NewSampleBuffer(10)
		buffer.Add(telemetry.Sample{
			Timestamp: time.Now(),
			Link:      "linkX",
			Device:    "deviceX",
			RTT:       5 * time.Millisecond,
			Loss:      false,
		})

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:    10 * time.Millisecond,
			Buffer:      buffer,
			SubmitFunc:  submit,
			MaxAttempts: 5,
			BackoffFunc: func(_ int) time.Duration { return 0 },
		})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		require.NoError(t, submitter.Run(ctx))

		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, callCount, 3)
	})

	t.Run("aborts_retries_when_context_is_cancelled", func(t *testing.T) {
		t.Parallel()

		var callCount int
		submit := func(ctx context.Context, samples []telemetry.Sample) error {
			callCount++
			return errors.New("still failing")
		}

		buffer := telemetry.NewSampleBuffer(10)
		buffer.Add(telemetry.Sample{
			Timestamp: time.Now(),
			Link:      "linkY",
			Device:    "deviceY",
			RTT:       10 * time.Millisecond,
			Loss:      false,
		})

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:    10 * time.Millisecond,
			Buffer:      buffer,
			SubmitFunc:  submit,
			MaxAttempts: 5,
			BackoffFunc: func(_ int) time.Duration { return 10 * time.Millisecond },
		})

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(15 * time.Millisecond)
			cancel()
		}()

		require.NoError(t, submitter.Run(ctx))
		assert.Less(t, callCount, 5, "should not retry full 5 times due to context cancel")
	})
}
