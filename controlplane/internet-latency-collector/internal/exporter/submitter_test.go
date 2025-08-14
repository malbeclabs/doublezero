package exporter_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Submitter(t *testing.T) {
	t.Parallel()

	t.Run("submits_buffered_samples", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		var received []exporter.Sample
		var receivedKey exporter.PartitionKey

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				receivedKey = exporter.PartitionKey{
					DataProvider:     "test",
					SourceExchangePK: config.OriginExchangePK,
					TargetExchangePK: config.TargetExchangePK,
					Epoch:            config.Epoch,
				}
				samples := make([]exporter.Sample, len(config.Samples))
				for i, sample := range config.Samples {
					samples[i] = exporter.Sample{
						Timestamp: time.Now(),
						RTT:       time.Duration(sample) * time.Microsecond,
					}
				}
				received = append(received, samples...)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		key := newTestPartitionKey()
		buffer.Add(key, newTestSample())

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		require.Len(t, received, 1)
		assert.Equal(t, key, receivedKey)
	})

	t.Run("retries_on_transient_error", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		var mu sync.Mutex
		var callCount int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				callCount++
				if callCount < 3 {
					return solana.Signature{}, nil, errors.New("temporary failure")
				}
				return solana.Signature{}, nil, nil
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		key := newTestPartitionKey()
		buffer.Add(key, newTestSample())

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, callCount, 3)
	})

	t.Run("aborts_retries_when_context_is_cancelled", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		var mu sync.Mutex
		var callCount int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				callCount++
				return solana.Signature{}, nil, errors.New("still failing")
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		key := newTestPartitionKey()
		buffer.Add(key, newTestSample())

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 10 * time.Millisecond },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately before retry starts

		submitter.Tick(ctx)

		assert.Less(t, callCount, 5, "should not retry full 5 times due to context cancel")
	})

	t.Run("preserves_samples_after_exhausted_retries", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, errors.New("permanent failure")
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		samplesAfter := buffer.CopyAndReset(key)
		require.Len(t, samplesAfter, 1)
		assert.Equal(t, sample.RTT, samplesAfter[0].RTT)
		assert.Equal(t, sample.Timestamp, samplesAfter[0].Timestamp)

		assert.Equal(t, int32(3), attempts, "should have retried exactly MaxAttempts times")
	})

	t.Run("drops_samples_after_successful_submission", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, nil
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after successful submission")
		assert.Equal(t, int32(1), attempts, "should not retry on successful submission")
	})

	t.Run("retries_then_drops_samples_on_eventual_success", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				n := atomic.AddInt32(&attempts, 1)
				if n < 2 {
					return solana.Signature{}, nil, errors.New("transient failure")
				}
				return solana.Signature{}, nil, nil
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 0, "samples should be discarded after eventual successful submission")
		assert.Equal(t, int32(2), attempts, "should have retried once before succeeding")
	})

	t.Run("preserves_samples_when_context_cancelled_mid_retry", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var attempts int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&attempts, 1)
				return solana.Signature{}, nil, errors.New("still failing")
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   5,
			BackoffFunc: func(_ int) time.Duration {
				cancel() // cancel immediately after first failure
				return 10 * time.Millisecond
			},
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(ctx)

		samplesAfter := buffer.CopyAndReset(key)
		assert.Len(t, samplesAfter, 1, "samples should be preserved if context cancels during retries")
		assert.Less(t, attempts, int32(5), "should stop retrying when context is cancelled")
	})

	t.Run("removes_account_key_for_past_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		pastEpoch := uint64(1)
		key := exporter.PartitionKey{
			DataProvider:     "dp",
			SourceExchangePK: solana.NewWallet().PublicKey(),
			TargetExchangePK: solana.NewWallet().PublicKey(),
			Epoch:            pastEpoch,
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, exporter.Sample{}) // Add a sample just to register the key
		_ = buffer.CopyAndReset(key)       // Now make it empty

		assert.True(t, buffer.Has(key), "buffer should contain key before tick")

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				t.Fatalf("should not call WriteInternetLatencySamples for empty samples")
				return solana.Signature{}, nil, nil
			},
		}

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return pastEpoch + 1, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		assert.False(t, buffer.Has(key), "key from past epoch should be removed if buffer is empty")
	})

	t.Run("keeps_account_key_for_current_epoch_with_no_samples", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		currentEpoch := uint64(1)
		key := exporter.PartitionKey{
			DataProvider:     "dp",
			SourceExchangePK: solana.NewWallet().PublicKey(),
			TargetExchangePK: solana.NewWallet().PublicKey(),
			Epoch:            currentEpoch,
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, exporter.Sample{})
		_ = buffer.CopyAndReset(key)

		assert.True(t, buffer.Has(key), "buffer should contain key before tick")

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				t.Fatalf("should not call WriteInternetLatencySamples for empty samples")
				return solana.Signature{}, nil, nil
			},
		}

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return currentEpoch, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		assert.True(t, buffer.Has(key), "buffer should retain key for current epoch even if empty")
	})

	t.Run("chunks_large_batches_into_multiple_submissions", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		const totalSamples = 5500

		var mu sync.Mutex
		var calls int
		var samplesPerCall []int

		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				samplesPerCall = append(samplesPerCall, len(config.Samples))
				return solana.Signature{}, nil, nil
			},
		}

		key := newTestPartitionKey()
		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](sdktelemetry.MaxSamplesPerBatch)

		// Set up the submitter.
		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return 0, nil
			}},
		})
		require.NoError(t, err)

		// Add samples concurrently to avoid blocking.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range totalSamples {
				buffer.Add(key, exporter.Sample{
					Timestamp: time.Now(),
					RTT:       time.Duration(i+1) * time.Microsecond,
				})
			}
		}()

		// Keep ticking until producer finishes.
		for !waitTimeout(&wg, 10*time.Millisecond) {
			submitter.Tick(t.Context())
		}

		// Final drain to catch remaining.
		submitter.Tick(t.Context())

		// Wait for producer to finish.
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()

		require.Equal(t, 23, calls, "expected 23 submission calls for 5500 samples with max 245 per call")
		for i := range 22 {
			assert.Equal(t, sdktelemetry.MaxSamplesPerBatch, samplesPerCall[i])
		}
		assert.Equal(t, 110, samplesPerCall[22], "last call should contain 110 samples")
	})

	t.Run("drops_samples_on_account_full", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var called int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				atomic.AddInt32(&called, 1)
				return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   3, // won't matter, should exit early
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return key.Epoch, nil
			}},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		assert.Equal(t, int32(1), called, "should attempt submission only once")
		assert.False(t, buffer.Has(key), "partition key should be removed after account full error")
	})

	t.Run("initializes_account_then_drops_samples_on_account_full", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())

		key := newTestPartitionKey()
		sample := newTestSample()

		var callCount int32
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				switch atomic.AddInt32(&callCount, 1) {
				case 1:
					return solana.Signature{}, nil, sdktelemetry.ErrAccountNotFound
				default:
					return solana.Signature{}, nil, sdktelemetry.ErrSamplesAccountFull
				}
			},
			InitializeInternetLatencySamplesFunc: func(ctx context.Context, config sdktelemetry.InitializeInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, nil
			},
		}

		buffer := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buffer.Add(key, sample)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buffer,
			Telemetry:     telemetryProgram,
			MaxAttempts:   3,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder: &mockEpochFinder{ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
				return key.Epoch, nil
			}},
			DataProviderSamplingIntervals: map[exporter.DataProviderName]time.Duration{
				key.DataProvider: time.Second,
			},
		})
		require.NoError(t, err)

		submitter.Tick(t.Context())

		assert.Equal(t, int32(2), callCount, "should retry once after init, then drop")
		assert.False(t, buffer.Has(key), "partition key should be removed after account full error")
	})

	t.Run("failed_retries_reinsert_at_front_preserving_order", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())
		key := newTestPartitionKey()

		first := exporter.Sample{Timestamp: time.Now(), RTT: 1 * time.Millisecond}
		second := exporter.Sample{Timestamp: time.Now().Add(1 * time.Second), RTT: 2 * time.Millisecond}

		// Always fail to force retry path (PriorityPrepend)
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				return solana.Signature{}, nil, errors.New("fail")
			},
		}

		buf := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](128)
		buf.Add(key, first)

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buf,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder:   &mockEpochFinder{ApproximateAtTimeFunc: func(context.Context, time.Time) (uint64, error) { return 0, nil }},
		})
		require.NoError(t, err)

		// Tick once; write fails; failed batch is PriorityPrepended to the FRONT
		submitter.Tick(t.Context())

		// Producer adds a newer sample after the failed tick
		buf.Add(key, second)

		// Order should be: first (failed + reinserted at front), then second
		got := buf.CopyAndReset(key)
		require.Equal(t, []exporter.Sample{first, second}, got)
	})

	t.Run("priority_prepend_backpressures_producers_until_submitter_drains", func(t *testing.T) {
		t.Parallel()

		log := logger.With("test", t.Name())
		key := newTestPartitionKey()

		writeCalled := make(chan struct{})
		proceed := make(chan struct{})

		// Block inside Write so we can inject a producer Add while submit is in-flight
		telemetryProgram := &mockTelemetryProgramClient{
			WriteInternetLatencySamplesFunc: func(ctx context.Context, _ sdktelemetry.WriteInternetLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
				close(writeCalled) // signal we're inside Write
				<-proceed          // wait until test says proceed
				return solana.Signature{}, nil, errors.New("fail")
			},
		}

		// Capacity=1 so we can force len > capacity after PriorityPrepend
		buf := buffer.NewMemoryPartitionedBuffer[exporter.PartitionKey, exporter.Sample](1)
		buf.Add(key, newTestSample()) // one record present

		submitter, err := exporter.NewSubmitter(log, &exporter.SubmitterConfig{
			OracleAgentPK: solana.NewWallet().PublicKey(),
			Interval:      time.Hour,
			Buffer:        buf,
			Telemetry:     telemetryProgram,
			MaxAttempts:   1,
			BackoffFunc:   func(_ int) time.Duration { return 0 },
			EpochFinder:   &mockEpochFinder{ApproximateAtTimeFunc: func(context.Context, time.Time) (uint64, error) { return key.Epoch, nil }},
		})
		require.NoError(t, err)

		// Run Tick concurrently; it will CopyAndReset(), then call Write...
		go func() { submitter.Tick(t.Context()) }()

		// Wait until we're inside Write
		<-writeCalled

		// While submit is in-flight, producers add a new record.
		// After Write returns an error, PriorityPrepend will reinsert the failed batch at front.
		buf.Add(key, newTestSample())

		// Let Write return error -> triggers PriorityPrepend; now len=2 > capacity=1
		close(proceed)

		// Now a producer Add should block until we drain
		done := make(chan struct{})
		go func() {
			buf.Add(key, newTestSample()) // should block
			close(done)
		}()

		select {
		case <-done:
			t.Fatal("producer Add should block while len > capacity after PriorityPrepend")
		case <-time.After(30 * time.Millisecond):
			// still blocked, as expected
		}

		// Drain; this should unblock the producer Add
		_ = buf.CopyAndReset(key)

		select {
		case <-done:
			// unblocked as expected
		case <-time.After(250 * time.Millisecond):
			t.Fatal("producer Add should unblock after drain")
		}
	})

}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()
	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

func newTestSample() exporter.Sample {
	return exporter.Sample{
		Timestamp: time.Unix(123, 456),
		RTT:       42 * time.Millisecond,
	}
}

func newTestPartitionKey() exporter.PartitionKey {
	return exporter.PartitionKey{
		DataProvider:     "test",
		SourceExchangePK: solana.PublicKey{1},
		TargetExchangePK: solana.PublicKey{2},
		Epoch:            42,
	}
}
