package telemetry_test

import (
	"context"
	"errors"
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

		log := log.With("test", t.Name())

		var received []telemetry.Sample
		var receivedKey telemetry.AccountKey

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedKey = telemetry.AccountKey{
					OriginDevicePK: config.OriginDevicePK,
					TargetDevicePK: config.TargetDevicePK,
					LinkPK:         config.LinkPK,
					Epoch:          *config.Epoch,
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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, callCount, 3)
	})

	t.Run("aborts_retries_when_context_is_cancelled", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 10 * time.Millisecond },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour, // unused
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		require.Len(t, samplesAfter, 1)
		assert.Equal(t, sample.RTT, samplesAfter[0].RTT)
		assert.Equal(t, sample.Timestamp, samplesAfter[0].Timestamp)

		assert.Equal(t, int32(3), attempts, "should have retried exactly MaxAttempts times")
	})

	t.Run("drops_samples_after_successful_submission", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after successful submission")
		assert.Equal(t, int32(1), attempts, "should not retry on successful submission")
	})

	t.Run("retries_then_drops_samples_on_eventual_success", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after eventual successful submission")
		assert.Equal(t, int32(2), attempts, "should have retried once before succeeding")
	})

	t.Run("preserves_samples_when_context_cancelled_mid_retry", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc: func(_ int) time.Duration {
				cancel() // cancel immediately after first failure
				return 10 * time.Millisecond
			},
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(ctx)

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 1, "samples should be preserved if context cancels during retries")
		assert.Less(t, attempts, int32(5), "should stop retrying when context is cancelled")
	})

	t.Run("removes_account_key_for_past_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		pastEpoch := uint64(90)
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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		assert.False(t, buffer.Has(key), "key from past epoch should be removed if buffer is empty")
	})

	t.Run("keeps_account_key_for_current_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		currentEpoch := uint64(100)
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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return currentEpoch, nil
			},
		})
		require.NoError(t, err)

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

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		mu.Lock()
		defer mu.Unlock()

		require.Equal(t, 23, calls, "expected 23 submission calls for 5500 samples with max 245 per call")
		for i := range 22 {
			assert.Equal(t, sdktelemetry.MaxSamplesPerBatch, samplesPerCall[i])
		}
		assert.Equal(t, 110, samplesPerCall[22], "last call should contain 110 samples")
	})

	t.Run("negative_rtts_are_submitted_as_one", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestAccountKey()
		now := time.Now()

		sample := telemetry.Sample{
			Timestamp: now,
			RTT:       0,
			Loss:      false,
		}

		var receivedRTTs []uint32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedRTTs = append(receivedRTTs, config.Samples...)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		require.Len(t, receivedRTTs, 1, "should have submitted one sample")
		assert.Equal(t, uint32(1), receivedRTTs[0], "RTT of 0 should be coerced to 1")
	})

	t.Run("getCurrentEpoch_retries_then_succeeds", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestAccountKey()
		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, newTestSample())
		_ = buffer.CopyAndReset(key) // trigger empty buffer path

		var attempts int
		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:    time.Hour,
			Buffer:      buffer,
			MaxAttempts: 1,
			BackoffFunc: func(_ int) time.Duration { return 0 },
			ProgramClient: &mockTelemetryProgramClient{
				WriteDeviceLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
					return solana.Signature{}, nil, nil
				},
			},
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				attempts++
				if attempts < 3 {
					return 0, errors.New("transient failure")
				}
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		assert.Equal(t, 3, attempts, "should retry GetCurrentEpoch 3 times before succeeding")
	})

	t.Run("getCurrentEpoch_fails_and_skips_tick", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestAccountKey()
		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, newTestSample())
		_ = buffer.CopyAndReset(key) // trigger empty buffer path

		var epochAttempts int
		var submissionCalled bool

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:    time.Hour,
			Buffer:      buffer,
			MaxAttempts: 1,
			BackoffFunc: func(_ int) time.Duration { return 0 },
			ProgramClient: &mockTelemetryProgramClient{
				WriteDeviceLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
					submissionCalled = true
					return solana.Signature{}, nil, nil
				},
			},
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				epochAttempts++
				return 0, errors.New("persistent failure")
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		assert.Equal(t, 5, epochAttempts, "should retry GetCurrentEpoch 5 times before giving up")
		assert.False(t, submissionCalled, "should skip submission if GetCurrentEpoch fails")
	})

	t.Run("drops_samples_if_account_full", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       30 * time.Microsecond,
			Loss:      false,
		}

		// This client always returns ErrSamplesAccountFull
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be dropped on account full")
	})

	t.Run("initializes_then_drops_samples_if_account_full", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestAccountKey()
		sample := telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       40 * time.Microsecond,
			Loss:      false,
		}

		var initCalled, writeCalled int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				if atomic.AddInt32(&writeCalled, 1) == 1 {
					return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
				}
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
			InitializeDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.StoreInt32(&initCalled, 1)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := telemetry.NewAccountsBuffer()
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:      time.Hour,
			Buffer:        buffer,
			ProgramClient: telemetryProgram,
			MaxAttempts:   2,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be dropped after account init + full")
		assert.Equal(t, int32(1), atomic.LoadInt32(&initCalled), "should initialize account before dropping")
		assert.Equal(t, int32(2), atomic.LoadInt32(&writeCalled), "should try write twice (before and after init)")
	})

}
