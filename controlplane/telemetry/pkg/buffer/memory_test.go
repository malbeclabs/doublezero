package buffer_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/buffer"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Buffer_PartitionBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add and Read returns expected sample", func(t *testing.T) {
		t.Parallel()
		buf := buffer.NewMemoryBuffer[testRecord](10)
		s := testRecord{value: "test"}
		buf.Add(s)

		read := buf.Read()
		require.Len(t, read, 1)
		require.Equal(t, s, read[0])
	})

	t.Run("Read returns copy not shared with buffer", func(t *testing.T) {
		t.Parallel()
		buf := buffer.NewMemoryBuffer[testRecord](10)
		buf.Add(testRecord{value: "test"})

		copy1 := buf.Read()
		copy1[0].value = "test2"

		copy2 := buf.Read()
		require.Equal(t, "test", copy2[0].value)
	})

	t.Run("CopyAndReset clears buffer and returns full copy", func(t *testing.T) {
		buf := buffer.NewMemoryBuffer[testRecord](10)
		buf.Add(testRecord{value: "test"})
		out := buf.CopyAndReset()

		require.Len(t, out, 1)
		require.Equal(t, 0, buf.Len())
	})

	t.Run("FlushWithoutReset returns non-mutating copy", func(t *testing.T) {
		buf := buffer.NewMemoryBuffer[testRecord](10)
		buf.Add(testRecord{value: "test"})
		out := buf.FlushWithoutReset()

		require.Len(t, out, 1)
		require.Equal(t, 1, buf.Len())
	})

	t.Run("PriorityPrepend preserves order and exceeds capacity", func(t *testing.T) {
		buf := buffer.NewMemoryBuffer[testRecord](2)
		buf.Add(testRecord{"a"})
		buf.Add(testRecord{"b"})
		// capacity reached with 2/3, then exceed to 5
		buf.PriorityPrepend([]testRecord{{"x"}, {"y"}, {"z"}})
		got := buf.Read()
		require.Equal(t, []testRecord{{"x"}, {"y"}, {"z"}, {"a"}, {"b"}}, got)
		// len is now 5 > maxCapacity(3)
		require.Equal(t, 5, len(got))
	})

	t.Run("PriorityPrepend causes Add to block until CopyAndReset drains", func(t *testing.T) {
		buf := buffer.NewMemoryBuffer[testRecord](2)
		buf.Add(testRecord{"a"})
		buf.Add(testRecord{"b"})                 // full
		buf.PriorityPrepend([]testRecord{{"p"}}) // len=3 > cap=2

		done := make(chan struct{})
		go func() {
			buf.Add(testRecord{"c"}) // should block
			close(done)
		}()

		select {
		case <-done:
			t.Fatal("Add should have blocked while len>=maxCapacity")
		case <-time.After(20 * time.Millisecond):
			// still blocked as expected
		}

		tmp := buf.CopyAndReset() // drains and Broadcasts
		require.GreaterOrEqual(t, len(tmp), 3)

		// Now Add should proceed
		select {
		case <-done:
			// unblocked as expected
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Add should have unblocked after drain")
		}
	})

}

func TestInternetLatency_Buffer_AccountsBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Add stores sample under key", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		s := testRecord{value: "test"}
		k := testPartitionKey{key: "test"}
		buf.Add(k, s)

		snap := buf.FlushWithoutReset()
		samples := snap[k]
		require.Len(t, samples, 1)
		require.Equal(t, s, samples[0])
	})

	t.Run("Recycle reuses memory buffer", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		k := testPartitionKey{key: "test"}
		buf.Add(k, testRecord{value: "test"})

		s := buf.FlushWithoutReset()[k]
		require.Len(t, s, 1)

		// Properly reset buffer before recycling
		s = buf.CopyAndReset(k)
		buf.Recycle(k, s)

		buf.Add(k, testRecord{value: "test"})
		require.Len(t, buf.FlushWithoutReset()[k], 1)
	})

	t.Run("Remove removes account key", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		k := testPartitionKey{key: "test"}
		buf.Add(k, testRecord{value: "test"})
		buf.Remove(k)
		require.False(t, buf.Has(k))
	})

	t.Run("Has returns true if account key exists", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		k := testPartitionKey{key: "test"}
		buf.Add(k, testRecord{value: "test"})
		require.True(t, buf.Has(k))
	})

	t.Run("Has returns false if account key does not exist", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		k := testPartitionKey{key: "test"}
		require.False(t, buf.Has(k))
	})

	t.Run("CopyAndReset returns nil if key not found", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		out := buf.CopyAndReset(testPartitionKey{key: "test"})
		require.Nil(t, out)
	})

	t.Run("Read returns nil if key not found", func(t *testing.T) {
		buf := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		out := buf.Read(testPartitionKey{key: "test"})
		require.Nil(t, out)
	})

	t.Run("PriorityPrepend creates buffer and preserves order", func(t *testing.T) {
		p := buffer.NewMemoryPartitionedBuffer[testPartitionKey, testRecord](128)
		k := testPartitionKey{"k"}
		p.PriorityPrepend(k, []testRecord{{"x"}}) // creates
		p.Add(k, testRecord{"y"})
		require.Equal(t, []testRecord{{"x"}, {"y"}}, p.Read(k))
	})
}

type testRecord struct {
	value string
}

type testPartitionKey struct {
	key string
}

func (k testPartitionKey) String() string {
	return k.key
}
