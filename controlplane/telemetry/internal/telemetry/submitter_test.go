package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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
}
