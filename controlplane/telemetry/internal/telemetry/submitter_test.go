package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Submitter(t *testing.T) {
	t.Parallel()

	t.Run("submits_buffered_samples", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		var received []telemetry.Sample
		var receivedKey telemetry.PartitionKey

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedKey = telemetry.PartitionKey{
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		key := newTestPartitionKey()
		buffer.Add(key, newTestSample())

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour, // unused
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(newTestPartitionKey(), telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       5 * time.Microsecond,
			Loss:      false,
		})

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour, // unused
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    5,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(newTestPartitionKey(), telemetry.Sample{
			Timestamp: time.Now(),
			RTT:       10 * time.Microsecond,
			Loss:      false,
		})

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour, // unused
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    5,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 10 * time.Millisecond },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour, // unused
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    3,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    3,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    5,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    5,
			MaxConcurrency: 10,
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
		key := telemetry.PartitionKey{
			OriginDevicePK: solana.NewWallet().PublicKey(),
			TargetDevicePK: solana.NewWallet().PublicKey(),
			LinkPK:         solana.NewWallet().PublicKey(),
			Epoch:          pastEpoch,
		}

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
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
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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
		key := telemetry.PartitionKey{
			OriginDevicePK: solana.NewWallet().PublicKey(),
			TargetDevicePK: solana.NewWallet().PublicKey(),
			LinkPK:         solana.NewWallet().PublicKey(),
			Epoch:          currentEpoch,
		}

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, telemetry.Sample{})
		_ = buffer.CopyAndReset(key)

		assert.True(t, buffer.Has(key), "buffer should contain key before tick")

		telemetryProgram := &mockTelemetryProgramClient{}

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](totalSamples)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
		})
		require.NoError(t, err)

		// Add all samples before ticking to ensure deterministic chunking.
		for i := range totalSamples {
			buffer.Add(key, telemetry.Sample{
				Timestamp: time.Now(),
				RTT:       time.Duration(i+1) * time.Microsecond,
			})
		}

		submitter.Tick(t.Context())

		mu.Lock()
		defer mu.Unlock()

		// 5500 / 239 = 23 full batches + 3 remaining = 24 calls.
		require.Equal(t, 24, calls, "expected 24 submission calls for 5500 samples")
		for i := range 24 {
			if i == 23 {
				assert.Equal(t, 3, samplesPerCall[i])
			} else {
				assert.Equal(t, sdktelemetry.MaxDeviceLatencySamplesPerBatch, samplesPerCall[i])
			}
		}
	})

	t.Run("negative_rtts_are_submitted_as_one", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, newTestSample())
		_ = buffer.CopyAndReset(key) // trigger empty buffer path

		var attempts int
		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, newTestSample())
		_ = buffer.CopyAndReset(key) // trigger empty buffer path

		var epochAttempts int
		var submissionCalled bool

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    3,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

		key := newTestPartitionKey()
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

		buffer := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buffer.Add(key, sample)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buffer,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    2,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
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

	t.Run("failed_retries_reinsert_at_front_preserving_order", func(t *testing.T) {
		t.Parallel()

		key := newTestPartitionKey()
		first := telemetry.Sample{Timestamp: time.Now(), RTT: 1 * time.Millisecond}
		second := telemetry.Sample{Timestamp: time.Now().Add(1 * time.Second), RTT: 2 * time.Millisecond}

		// Program always fails
		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("permanent failure")
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, first)

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   telemetryProgram,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		// First tick: fails to submit first sample, so it's reinserted
		submitter.Tick(context.Background())

		// Add another sample that would be "newer"
		buf.Add(key, second)

		// Next tick: CopyAndReset should yield first then second
		got := buf.CopyAndReset(key)
		require.Equal(t, []telemetry.Sample{first, second}, got)
	})

	t.Run("failed_retries_keep_newest_when_over_capacity", func(t *testing.T) {
		t.Parallel()

		key := newTestPartitionKey()
		older := telemetry.Sample{Timestamp: time.Now(), RTT: time.Millisecond}
		newer := telemetry.Sample{Timestamp: time.Now().Add(time.Second), RTT: 2 * time.Millisecond}

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("perm fail")
			},
		}

		// Capacity 1, but two samples fail to submit (seeded via PriorityPrepend since Add
		// would block past capacity). Only one fits: keep the newer, drop the older.
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1)
		buf.PriorityPrepend(key, []telemetry.Sample{older, newer})

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		s.Tick(context.Background())

		got := buf.CopyAndReset(key)
		assert.Equal(t, []telemetry.Sample{newer}, got, "should keep the newest sample that fits and drop the older one")
	})

	t.Run("kept_samples_never_exceed_capacity_after_partial_drop", func(t *testing.T) {
		t.Parallel()

		key := newTestPartitionKey()
		samples := []telemetry.Sample{
			{Timestamp: time.Now(), RTT: 1 * time.Millisecond},
			{Timestamp: time.Now().Add(time.Second), RTT: 2 * time.Millisecond},
			{Timestamp: time.Now().Add(2 * time.Second), RTT: 3 * time.Millisecond},
		}

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("fail")
			},
		}

		// Capacity 2, three samples fail to submit: only the two newest fit.
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2)
		buf.PriorityPrepend(key, samples)

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		s.Tick(context.Background())

		got := buf.CopyAndReset(key)
		assert.Equal(t, samples[1:], got, "buffer should hold exactly the newest `capacity` samples, never more")
	})

	t.Run("passes_agent_version_and_commit_to_write", func(t *testing.T) {
		t.Parallel()

		log := log.With("test", t.Name())

		var receivedConfig sdktelemetry.WriteDeviceLatencySamplesInstructionConfig

		telemetryProgram := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedConfig = config
				return solana.Signature{}, nil, nil
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(newTestPartitionKey(), newTestSample())

		submitter, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:       time.Hour,
			Buffer:         buf,
			ProgramClient:  telemetryProgram,
			MaxAttempts:    1,
			MaxConcurrency: 10,
			BackoffFunc:    func(_ int) time.Duration { return 0 },
			GetCurrentEpoch: func(ctx context.Context) (uint64, error) {
				return 100, nil
			},
			AgentVersion: "1.2.3",
			AgentCommit:  "aabbccdd",
		})
		require.NoError(t, err)

		submitter.Tick(context.Background())

		assert.Equal(t, "1.2.3", receivedConfig.AgentVersion)
		assert.Equal(t, "aabbccdd", receivedConfig.AgentCommit)
	})

	t.Run("requeues_failed_samples_when_they_exactly_meet_capacity", func(t *testing.T) {
		t.Parallel()

		key := newTestPartitionKey()
		first := telemetry.Sample{Timestamp: time.Now(), RTT: time.Millisecond}
		second := telemetry.Sample{Timestamp: time.Now().Add(time.Second), RTT: 2 * time.Millisecond}

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("perm fail")
			},
		}

		// Capacity == len(tmp) (2). Since Len(key) is 0 after CopyAndReset, the two failed
		// samples exactly fit (0+2 == 2) and should be kept in full, not dropped.
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2)
		buf.Add(key, first)
		buf.Add(key, second)

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     2,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		s.Tick(context.Background())

		got := buf.CopyAndReset(key)
		assert.Equal(t, []telemetry.Sample{first, second}, got, "failed samples that exactly fill capacity should be fully requeued, not dropped")
	})

	// Deliberately not parallel: the drop counters are package-level prometheus metrics shared with
	// the sibling over-capacity subtests, so the deltas are only exact while nothing else is running.
	t.Run("logs_and_counts_dropped_samples_when_over_capacity", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()
		samples := []telemetry.Sample{
			{Timestamp: time.Now(), RTT: 1 * time.Millisecond},
			{Timestamp: time.Now().Add(time.Second), RTT: 2 * time.Millisecond},
			{Timestamp: time.Now().Add(2 * time.Second), RTT: 3 * time.Millisecond},
		}

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("perm fail")
			},
		}

		// Capacity 2, three samples fail: only the newest two fit, the oldest is dropped.
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2)
		buf.PriorityPrepend(key, samples)

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonBufferFull))
		errorsBefore := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterBufferFull))

		s.Tick(context.Background())

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonBufferFull)) - droppedBefore
		assert.Equal(t, float64(1), dropped, "drop counter should increment by the number of discarded samples")

		errs := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterBufferFull)) - errorsBefore
		assert.Equal(t, float64(1), errs, "buffer-full error counter should increment once per affected batch")

		out := logs.String()
		assert.Contains(t, out, "Partition buffer at capacity after failed submission, dropping oldest samples")
		assert.Contains(t, out, "droppedSamples=1")
		assert.Contains(t, out, "keptSamples=2")
		assert.Contains(t, out, "capacity=2")

		assert.Equal(t, samples[1:], buf.CopyAndReset(key), "only the oldest sample should be dropped; the newest two should be requeued")
	})

	t.Run("does_not_count_drops_when_samples_are_requeued", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("perm fail")
			},
		}

		// Capacity well above len(tmp), so the failed batch is requeued rather than dropped.
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, newTestSample())

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonBufferFull))

		s.Tick(context.Background())

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonBufferFull)) - droppedBefore
		assert.Equal(t, float64(0), dropped, "requeued samples should not be counted as dropped")
		assert.NotContains(t, logs.String(), "dropping samples")
		assert.Len(t, buf.CopyAndReset(key), 1, "failed samples should be requeued below capacity")
	})

	// Deliberately not parallel: see the note above, the account-full drop counters are shared with
	// the sibling account-full subtests.
	t.Run("counts_dropped_samples_when_account_is_full", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, newTestSample())
		buf.Add(key, newTestSample())

		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				// Collected after the flush, so this sample goes down with the partition.
				buf.Add(key, newTestSample())
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
		}

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     3,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull))
		errorsBefore := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterAccountFull))

		s.Tick(context.Background())

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull)) - droppedBefore
		assert.Equal(t, float64(3), dropped, "drop counter should cover the unsubmitted batch and the buffered samples going down with the partition")

		errs := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterAccountFull)) - errorsBefore
		assert.Equal(t, float64(1), errs, "account-full error counter should increment once")

		out := logs.String()
		assert.Contains(t, out, "Partition account is full, dropping partition")
		assert.Contains(t, out, "unsubmittedSamples=2")
		assert.Contains(t, out, "bufferedSamples=1")
		assert.Len(t, buf.CopyAndReset(key), 0, "partition should be removed on account full")
	})

	t.Run("counts_only_unsubmitted_samples_when_account_fills_mid_partition", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		// Two batches: the first is written, the second finds the account full.
		total := sdktelemetry.MaxDeviceLatencySamplesPerBatch + 61
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2048)
		for range total {
			buf.Add(key, newTestSample())
		}

		var writes int32
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				if atomic.AddInt32(&writes, 1) == 1 {
					return solana.Signature{}, nil, nil
				}
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
		}

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     1,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull))

		s.Tick(context.Background())

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull)) - droppedBefore
		assert.Equal(t, float64(61), dropped, "the batch already written should not be counted as dropped")
		assert.Contains(t, logs.String(), "unsubmittedSamples=61")
		assert.NotContains(t, logs.String(), "unsubmittedSamples="+strconv.Itoa(total))
	})

	// Deliberately not parallel: see the note above, the account-full drop counters are shared with
	// the sibling account-full subtests.
	t.Run("counts_only_unsubmitted_samples_when_account_fills_after_a_retry", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		// Two batches: the first is written, the second takes a transient error, and the retry
		// finds the account full.
		total := sdktelemetry.MaxDeviceLatencySamplesPerBatch + 61
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2048)
		for range total {
			buf.Add(key, newTestSample())
		}

		var mu sync.Mutex
		var batchSizes []int
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(_ context.Context, cfg sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				batchSizes = append(batchSizes, len(cfg.Samples))
				n := len(batchSizes)
				mu.Unlock()

				switch n {
				case 1:
					return solana.Signature{}, nil, nil
				case 2:
					return solana.Signature{}, nil, errors.New("transient rpc error")
				default:
					return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
				}
			},
		}

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     2,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull))

		s.Tick(context.Background())

		// The retry resumes at the second batch instead of re-sending the first, which is both why
		// the count is right and why the already-written samples are not appended twice.
		assert.Equal(t, []int{sdktelemetry.MaxDeviceLatencySamplesPerBatch, 61, 61}, batchSizes,
			"the retry should resume at the first unwritten sample")

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull)) - droppedBefore
		assert.Equal(t, float64(61), dropped, "the batch written by the earlier attempt should not be counted as dropped")
		assert.Contains(t, logs.String(), "unsubmittedSamples=61")
		assert.NotContains(t, logs.String(), "unsubmittedSamples="+strconv.Itoa(total))
	})

	t.Run("requeues_only_unwritten_samples_after_a_failed_submission", func(t *testing.T) {
		t.Parallel()

		log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		// Two batches: the first is written, the second fails on every attempt.
		total := sdktelemetry.MaxDeviceLatencySamplesPerBatch + 61
		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](2048)
		for range total {
			buf.Add(key, newTestSample())
		}

		var writes int32
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				if atomic.AddInt32(&writes, 1) == 1 {
					return solana.Signature{}, nil, nil
				}
				return solana.Signature{}, nil, errors.New("transient rpc error")
			},
		}

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     2,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		s.Tick(context.Background())

		assert.Len(t, buf.CopyAndReset(key), 61,
			"only the samples never written should be requeued, or the next tick appends them twice")
	})

	t.Run("counts_dropped_samples_when_account_is_full_after_initialize", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		var writes int32
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				if atomic.AddInt32(&writes, 1) == 1 {
					return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
				}
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
			InitializeDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, nil
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, newTestSample())

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     2,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		droppedBefore := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull))
		errorsBefore := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterAccountFull))

		s.Tick(context.Background())

		dropped := testutil.ToFloat64(metrics.SamplesDropped.WithLabelValues(metrics.DropReasonAccountFull)) - droppedBefore
		assert.Equal(t, float64(1), dropped, "the post-initialize account-full path should count its drop too")

		errs := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterAccountFull)) - errorsBefore
		assert.Equal(t, float64(1), errs, "account-full error counter should increment once")
		assert.Contains(t, logs.String(), "Partition account is full, dropping partition")
	})

	// An init the program rejects because the account already exists still leaves the write able to
	// proceed, so it must not end the submission. Reachable when a previous init landed onchain
	// without the agent seeing it succeed.
	t.Run("writes_anyway_when_the_account_already_exists", func(t *testing.T) {
		t.Parallel()

		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		var writes int32
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				// Preflight reports the account missing, then the post-init write finds it there.
				if atomic.AddInt32(&writes, 1) == 1 {
					return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
				}
				return solana.Signature{}, nil, nil
			},
			InitializeDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, &sdktelemetry.ProgramError{
					Err:  map[string]any{"InstructionError": []any{0, map[string]any{"Custom": 1010}}},
					Logs: []string{"Program log: Latency samples account already exists"},
				}
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, newTestSample())

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     3,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		s.Tick(context.Background())

		assert.Equal(t, int32(2), atomic.LoadInt32(&writes), "the write should be attempted after the rejected init")
		assert.Len(t, buf.CopyAndReset(key), 0, "the samples were written, so nothing should be requeued")
		assert.Contains(t, logs.String(), "attempting the write anyway")
		assert.NotContains(t, logs.String(), "Submission rejected by the telemetry program",
			"an account that already exists is not a submission failure")
	})

	// The chi-dn-dzd4 case (malbeclabs/infra#1703): the agent key is not the device's
	// metrics_publisher, so the program rejects the init. Deliberately not parallel: the
	// program-error delta is on a package-level prometheus counter.
	t.Run("does_not_retry_a_submission_the_program_rejected", func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

		key := newTestPartitionKey()

		var writes, inits int32
		prog := &mockTelemetryProgramClient{
			WriteDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&writes, 1)
				return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
			},
			InitializeDeviceLatencySamplesFunc: func(context.Context, sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&inits, 1)
				return solana.Signature{}, nil, &sdktelemetry.ProgramError{
					Err: map[string]any{"InstructionError": []any{0, map[string]any{"Custom": 1001}}},
					Logs: []string{
						"Program log: Instruction: InitializeDeviceLatencySamples",
						"Program log: Agent BA14eqpRNmkcQhjsH5abfvaUxRi7RcGGuQVeQuJdwPZc is not authorized for origin device FYkmttUmox6kZVjVNCATXEdGt3bfLicn5fJ8fnGfF4fZ",
					},
				}
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[telemetry.PartitionKey, telemetry.Sample](1024)
		buf.Add(key, newTestSample())

		s, err := telemetry.NewSubmitter(log, &telemetry.SubmitterConfig{
			Interval:        time.Hour,
			Buffer:          buf,
			ProgramClient:   prog,
			MaxAttempts:     5,
			MaxConcurrency:  10,
			BackoffFunc:     func(int) time.Duration { return 0 },
			GetCurrentEpoch: func(context.Context) (uint64, error) { return 100, nil },
		})
		require.NoError(t, err)

		programErrsBefore := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterProgramError))

		s.Tick(context.Background())

		// One write to find the account missing, one to confirm the rejected init left it missing.
		assert.Equal(t, int32(2), atomic.LoadInt32(&writes), "the rejection should end the tick, not spend the remaining attempts")
		assert.Equal(t, int32(1), atomic.LoadInt32(&inits), "an init the program rejected should not be re-sent unchanged")

		programErrs := testutil.ToFloat64(metrics.Errors.WithLabelValues(metrics.ErrorTypeSubmitterProgramError)) - programErrsBefore
		assert.Equal(t, float64(1), programErrs, "program-error counter should increment once")

		out := logs.String()
		// Asserted on the log rather than a delta of submitter_retries_exhausted: that counter is
		// package-level and TestSubmitter_RetainsEverySampleAcrossTheStalenessBound drives it from a
		// sibling parallel test, so a zero delta there would be racy.
		assert.NotContains(t, out, "Submission failed after all retries", "the attempts should be skipped, not exhausted")
		assert.Contains(t, out, "Submission rejected by the telemetry program")
		assert.Contains(t, out, "is not authorized for origin device", "the reason the program gave has to reach the log")

		assert.Len(t, buf.CopyAndReset(key), 1, "samples should be requeued for the next tick")
	})
}
