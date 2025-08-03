package exporter_test

import (
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Buffer_PartitionBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add and Read returns expected sample", func(t *testing.T) {
		t.Parallel()
		buf := exporter.NewPartitionBuffer(10)
		s := newTestSample()
		buf.Add(s)

		read := buf.Read()
		require.Len(t, read, 1)
		require.Equal(t, s, read[0])
	})

	t.Run("Read returns copy not shared with buffer", func(t *testing.T) {
		t.Parallel()
		buf := exporter.NewPartitionBuffer(10)
		buf.Add(newTestSample())

		copy1 := buf.Read()
		copy1[0].RTT = 123 * time.Millisecond

		copy2 := buf.Read()
		require.Equal(t, 42*time.Millisecond, copy2[0].RTT)
	})

	t.Run("CopyAndReset clears buffer and returns full copy", func(t *testing.T) {
		buf := exporter.NewPartitionBuffer(10)
		buf.Add(newTestSample())
		out := buf.CopyAndReset()

		require.Len(t, out, 1)
		require.Equal(t, 0, buf.Len())
	})

	t.Run("FlushWithoutReset returns non-mutating copy", func(t *testing.T) {
		buf := exporter.NewPartitionBuffer(10)
		buf.Add(newTestSample())
		out := buf.FlushWithoutReset()

		require.Len(t, out, 1)
		require.Equal(t, 1, buf.Len())
	})
}

func TestInternetLatency_Buffer_AccountsBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add stores sample under key", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		s := newTestSample()
		k := newTestPartitionKey()
		buf.Add(k, s)

		snap := buf.FlushWithoutReset()
		samples := snap[k]
		require.Len(t, samples, 1)
		require.Equal(t, s, samples[0])
	})

	t.Run("Recycle reuses memory buffer", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		k := newTestPartitionKey()
		buf.Add(k, newTestSample())

		s := buf.FlushWithoutReset()[k]
		require.Len(t, s, 1)

		// Properly reset buffer before recycling
		s = buf.CopyAndReset(k)
		buf.Recycle(k, s)

		buf.Add(k, newTestSample())
		require.Len(t, buf.FlushWithoutReset()[k], 1)
	})

	t.Run("Remove removes account key", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		k := newTestPartitionKey()
		buf.Add(k, newTestSample())
		buf.Remove(k)
		require.False(t, buf.Has(k))
	})

	t.Run("Has returns true if account key exists", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		k := newTestPartitionKey()
		buf.Add(k, newTestSample())
		require.True(t, buf.Has(k))
	})

	t.Run("Has returns false if account key does not exist", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		k := newTestPartitionKey()
		require.False(t, buf.Has(k))
	})

	t.Run("CopyAndReset returns nil if key not found", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		out := buf.CopyAndReset(newTestPartitionKey())
		require.Nil(t, out)
	})

	t.Run("Read returns nil if key not found", func(t *testing.T) {
		buf := exporter.NewPartitionedBuffer(128)
		out := buf.Read(newTestPartitionKey())
		require.Nil(t, out)
	})
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
		SourceLocationPK: solana.PublicKey{1},
		TargetLocationPK: solana.PublicKey{2},
		Epoch:            42,
	}
}
