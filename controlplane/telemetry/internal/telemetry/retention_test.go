package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxEpochStaleness(t *testing.T) {
	t.Parallel()

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("the default is inside the buffer at the default probe interval", func(t *testing.T) {
		t.Parallel()

		// If this stops holding, every agent logs a clamp warning on startup, which is the signal that
		// the default and the buffer have drifted apart.
		got := maxEpochStaleness(discard, DefaultMaxEpochStaleness, 10*time.Second, partitionBufferCapacity)
		assert.Equal(t, DefaultMaxEpochStaleness, got, "the default should not need clamping")
	})

	t.Run("a bound past the buffer's retention is clamped to it", func(t *testing.T) {
		t.Parallel()

		// 4096 samples at 10s is 11.4h, so 12h sits past the cliff — this is what shipped in the first
		// cut of the fallback.
		got := maxEpochStaleness(discard, 12*time.Hour, 10*time.Second, partitionBufferCapacity)
		assert.Less(t, got, 12*time.Hour, "should not trust the cache past what the buffer holds")
		assert.Equal(t, time.Duration(float64(partitionBufferCapacity)*float64(10*time.Second)*bufferRetentionHeadroom), got)
	})

	t.Run("a faster probe interval shrinks the bound", func(t *testing.T) {
		t.Parallel()

		// The same 4096 samples at 5s is only 5.7h, so the default has to come down with it.
		got := maxEpochStaleness(discard, DefaultMaxEpochStaleness, 5*time.Second, partitionBufferCapacity)
		assert.Less(t, got, DefaultMaxEpochStaleness)
	})
}

// The staleness bound is only worth what the buffer under it can hold: once the submitter finds a
// partition over capacity it drops the entire backlog rather than the oldest slice, and the samples
// written afterwards are backdated by the whole gap. So an outage lasting the full allowed window
// must cost nothing.
func TestSubmitter_RetainsEverySampleAcrossTheStalenessBound(t *testing.T) {
	t.Parallel()

	const (
		capacity      = 64
		probeInterval = 10 * time.Millisecond
		// Probes per submission cycle, mirroring production's 60s submission against a 10s probe.
		probesPerCycle = 6
	)

	// What the collector would allow the pinger to keep probing for, at this capacity and interval.
	staleness := maxEpochStaleness(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, probeInterval, capacity)
	probes := int(staleness / probeInterval)
	require.Positive(t, probes)
	require.Less(t, probes, capacity, "the bound must leave the buffer headroom, or this is testing nothing")

	program := &refusingProgramClient{}

	buf := buffer.NewMemoryPartitionedBuffer[PartitionKey, Sample](capacity)
	key := PartitionKey{OriginDevicePK: solana.PublicKey{1}, TargetDevicePK: solana.PublicKey{2}, LinkPK: solana.PublicKey{3}, Epoch: 42}

	submitter, err := NewSubmitter(slog.New(slog.NewTextHandler(io.Discard, nil)), &SubmitterConfig{
		Interval:        time.Hour, // driven by hand
		Buffer:          buf,
		ProgramClient:   program,
		ProbeInterval:   probeInterval,
		MaxAttempts:     2,
		MaxConcurrency:  1,
		BackoffFunc:     func(int) time.Duration { return 0 },
		GetCurrentEpoch: func(context.Context) (uint64, error) { return 42, nil },
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Probe for the whole window the bound allows while the ledger refuses every write.
	for probed := 0; probed < probes; probed += probesPerCycle {
		for range min(probesPerCycle, probes-probed) {
			buf.Add(key, Sample{Timestamp: time.Unix(123, 456), RTT: 42 * time.Millisecond})
		}
		submitter.Tick(ctx)
	}

	require.Zero(t, program.written.Load(), "nothing should have landed while the ledger was refusing writes")

	program.writing.Store(true)
	submitter.Tick(ctx)

	assert.Equal(t, int64(probes), program.written.Load(),
		"every sample taken inside the staleness bound should survive to the flush")
}

// refusingProgramClient refuses every write until writing is set, standing in for the ledger outage
// that made the epoch stale in the first place — the submitter cannot drain while the pinger cannot
// resolve the epoch.
type refusingProgramClient struct {
	writing atomic.Bool
	written atomic.Int64
}

func (c *refusingProgramClient) WriteDeviceLatencySamples(_ context.Context, config sdktelemetry.WriteDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	if !c.writing.Load() {
		return solana.Signature{}, nil, errors.New("ledger rpc unreachable")
	}
	c.written.Add(int64(len(config.Samples)))
	return solana.Signature{}, nil, nil
}

func (c *refusingProgramClient) InitializeDeviceLatencySamples(context.Context, sdktelemetry.InitializeDeviceLatencySamplesInstructionConfig) (solana.Signature, *solanarpc.GetTransactionResult, error) {
	return solana.Signature{}, nil, nil
}
