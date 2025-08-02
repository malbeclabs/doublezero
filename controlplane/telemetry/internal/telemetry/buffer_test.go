package telemetry_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestAgentTelemetry_Buffer_AccountBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add and Read returns expected sample", func(t *testing.T) {
		t.Parallel()
		buf := telemetry.NewAccountBuffer(10)
		s := newTestSample()
		buf.Add(s)

		read := buf.Read()
		require.Len(t, read, 1)
		require.Equal(t, s, read[0])
	})

	t.Run("Read returns copy not shared with buffer", func(t *testing.T) {
		t.Parallel()
		buf := telemetry.NewAccountBuffer(10)
		buf.Add(newTestSample())

		copy1 := buf.Read()
		copy1[0].RTT = 123 * time.Millisecond

		copy2 := buf.Read()
		require.Equal(t, 42*time.Millisecond, copy2[0].RTT)
	})

	t.Run("CopyAndReset clears buffer and returns full copy", func(t *testing.T) {
		buf := telemetry.NewAccountBuffer(10)
		buf.Add(newTestSample())
		out := buf.CopyAndReset()

		require.Len(t, out, 1)
		require.Equal(t, 0, buf.Len())
	})

	t.Run("FlushWithoutReset returns non-mutating copy", func(t *testing.T) {
		buf := telemetry.NewAccountBuffer(10)
		buf.Add(newTestSample())
		out := buf.FlushWithoutReset()

		require.Len(t, out, 1)
		require.Equal(t, 1, buf.Len())
	})
}

func TestAgentTelemetry_Buffer_AccountsBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add stores sample under key", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		s := newTestSample()
		k := newTestAccountKey()
		buf.Add(k, s)

		snap := buf.FlushWithoutReset()
		samples := snap[k]
		require.Len(t, samples, 1)
		require.Equal(t, s, samples[0])
	})

	t.Run("Recycle reuses memory buffer", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		k := newTestAccountKey()
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
		buf := telemetry.NewAccountsBuffer()
		k := newTestAccountKey()
		buf.Add(k, newTestSample())
		buf.Remove(k)
		require.False(t, buf.Has(k))
	})

	t.Run("Has returns true if account key exists", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		k := newTestAccountKey()
		buf.Add(k, newTestSample())
		require.True(t, buf.Has(k))
	})

	t.Run("Has returns false if account key does not exist", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		k := newTestAccountKey()
		require.False(t, buf.Has(k))
	})

	t.Run("Recycle does nothing if account key does not exist", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		k := newTestAccountKey()
		buf.Recycle(k, []telemetry.Sample{})
		require.False(t, buf.Has(k))
	})

	t.Run("CopyAndReset returns nil if key not found", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		out := buf.CopyAndReset(newTestAccountKey())
		require.Nil(t, out)
	})

	t.Run("Read returns nil if key not found", func(t *testing.T) {
		buf := telemetry.NewAccountsBuffer()
		out := buf.Read(newTestAccountKey())
		require.Nil(t, out)
	})
}
