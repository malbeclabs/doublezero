package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Submitter(t *testing.T) {
	t.Parallel()

	t.Run("submits_buffered_samples", func(t *testing.T) {
		t.Parallel()

		var received []telemetry.Sample
		var receivedKey telemetry.AccountKey

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedKey = telemetry.AccountKey{
					OriginDevicePK: config.OriginDevicePK,
					TargetDevicePK: config.TargetDevicePK,
					LinkPK:         config.LinkPK,
					Epoch:          config.Epoch,
				}
				samples := make([]telemetry.Sample, len(config.Samples))
				for i, sample := range config.Samples {
					samples[i] = telemetry.Sample{
						Timestamp: time.Now(),
						RTT:       time.Duration(sample) * time.Microsecond,
						Loss:      sample == 0,
					}
				}
				received = append(received, samples...)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		key := newTestAccountKey()
		buffer.Add(key, newTestSample())

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		require.Len(t, received, 1)
		assert.Equal(t, key, receivedKey)
	})

	t.Run("retries_on_transient_error", func(t *testing.T) {
		t.Parallel()

		var mu sync.Mutex
		var callCount int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				callCount++
				if callCount < 3 {
					return solana.Signature{}, nil, errors.New("temporary failure")
				}
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(newTestAccountKey(), telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       5 * time.Microsecond,
			Loss:      false,
		})

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, callCount, 3)
	})

	t.Run("aborts_retries_when_context_is_cancelled", func(t *testing.T) {
		t.Parallel()

		var mu sync.Mutex
		var callCount int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				callCount++
				return solana.Signature{}, nil, errors.New("still failing")
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(newTestAccountKey(), telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       10 * time.Microsecond,
			Loss:      false,
		})

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 10 * time.Millisecond },
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately before retry starts

		submitter.Tick(ctx)

		assert.Less(t, callCount, 5, "should not retry full 5 times due to context cancel")
	})

	t.Run("preserves_samples_after_exhausted_retries", func(t *testing.T) {
		t.Parallel()

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       7 * time.Microsecond,
			Loss:      false,
		}

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, errors.New("permanent failure")
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		require.Len(t, samplesAfter, 1)
		assert.Equal(t, sample.RTT, samplesAfter[0].RTT)
		assert.Equal(t, sample.Timestamp, samplesAfter[0].Timestamp)

		assert.Equal(t, int32(3), attempts, "should have retried exactly MaxAttempts times")
	})

	t.Run("drops_samples_after_successful_submission", func(t *testing.T) {
		t.Parallel()

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       10 * time.Microsecond,
			Loss:      false,
		}

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after successful submission")
		assert.Equal(t, int32(1), attempts, "should not retry on successful submission")
	})

	t.Run("retries_then_drops_samples_on_eventual_success", func(t *testing.T) {
		t.Parallel()

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       15 * time.Microsecond,
			Loss:      false,
		}

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				n := atomic.AddInt32(&attempts, 1)
				if n < 2 {
					return solana.Signature{}, nil, errors.New("transient failure")
				}
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after eventual successful submission")
		assert.Equal(t, int32(2), attempts, "should have retried once before succeeding")
	})

	t.Run("preserves_samples_when_context_cancelled_mid_retry", func(t *testing.T) {
		t.Parallel()

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       20 * time.Microsecond,
			Loss:      false,
		}

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, errors.New("still failing")
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc: func(_ int) time.Duration {
				cancel() // cancel immediately after first failure
				return 10 * time.Millisecond
			},
		})

		submitter.Tick(ctx)

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 1, "samples should be preserved if context cancels during retries")
		assert.Less(t, attempts, int32(5), "should stop retrying when context is cancelled")
	})

	t.Run("removes_account_key_for_past_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		pastEpoch := telemetry.DeriveEpoch(time.Now().Add(-2 * 24 * time.Hour).UTC())
		key := telemetry.AccountKey{
			OriginDevicePK: solana.NewWallet().PublicKey(),
			TargetDevicePK: solana.NewWallet().PublicKey(),
			LinkPK:         solana.NewWallet().PublicKey(),
			Epoch:          pastEpoch,
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, telemetry.Sample{}) // Add a sample just to register the key
		_ = buffer.CopyAndReset(key)        // Now make it empty

		assert.True(t, buffer.Has(key), "buffer should contain key before tick")

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				t.Fatalf("should not call WriteDeviceLatencySamples for empty samples")
				return solana.Signature{}, nil, nil
			},
		}

		submitter := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		assert.False(t, buffer.Has(key), "key from past epoch should be removed if buffer is empty")
	})

	t.Run("keeps_account_key_for_current_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		currentEpoch := telemetry.DeriveEpoch(time.Now().UTC())
		key := telemetry.AccountKey{
			OriginDevicePK: solana.NewWallet().PublicKey(),
			TargetDevicePK: solana.NewWallet().PublicKey(),
			LinkPK:         solana.NewWallet().PublicKey(),
			Epoch:          currentEpoch,
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, telemetry.Sample{})
		_ = buffer.CopyAndReset(key)

		assert.True(t, buffer.Has(key), "buffer should contain key before tick")

		telemetryProgram := &mockTelemetryProgramClient{}

		submitter := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		assert.True(t, buffer.Has(key), "buffer should retain key for current epoch even if empty")
	})

	t.Run("chunks_large_batches_into_multiple_submissions", func(t *testing.T) {
		t.Parallel()

		const totalSamples = 5500

		var mu sync.Mutex
		var calls int
		var samplesPerCall []int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				samplesPerCall = append(samplesPerCall, len(config.Samples))
				return solana.Signature{}, nil, nil
			},
		}

		key := newTestAccountKey()
		buffer := telemetry.NewAccountsBuffer()

		for i := range totalSamples {
			buffer.Add(key, telemetry.Sample{
				Timestamp: time.Now(),
				RTT:       time.Duration(i+1) * time.Microsecond,
				Loss:      false,
			})
		}

		submitter := telemetry.NewSubmitter(slog.Default(), &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
		})

		submitter.Tick(context.Background())

		mu.Lock()
		defer mu.Unlock()

		require.Equal(t, 3, calls, "expected 3 submission calls for 5500 samples with max 2560 per call")
		assert.Equal(t, []int{sdktelemetry.MaxSamplesPerBatch, sdktelemetry.MaxSamplesPerBatch, 380}, samplesPerCall, "each call should contain at most 2560 samples")
	})
}
