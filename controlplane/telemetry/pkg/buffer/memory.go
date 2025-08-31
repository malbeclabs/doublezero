package buffer

import (
	"sync"
)

type MemoryPartitionedBuffer[K PartitionKey, R Record] struct {
	mu                      sync.RWMutex
	partitions              map[K]*MemoryBuffer[R]
	partitionBufferCapacity int
}

func NewMemoryPartitionedBuffer[K PartitionKey, R Record](partitionBufferCapacity int) *MemoryPartitionedBuffer[K, R] {
	return &MemoryPartitionedBuffer[K, R]{
		partitions:              make(map[K]*MemoryBuffer[R]),
		partitionBufferCapacity: partitionBufferCapacity,
	}
}

func (b *MemoryPartitionedBuffer[K, R]) Capacity(key K) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.partitionBufferCapacity
}

func (b *MemoryPartitionedBuffer[K, R]) Len(key K) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if _, ok := b.partitions[key]; !ok {
		return 0
	}
	return b.partitions[key].Len()
}

func (b *MemoryPartitionedBuffer[K, R]) Add(key K, record R) uint64 {
	b.mu.RLock()
	pb, ok := b.partitions[key]
	b.mu.RUnlock()

	if !ok {
		b.mu.Lock()
		if pb, ok = b.partitions[key]; !ok {
			pb = NewMemoryBuffer[R](b.partitionBufferCapacity)
			b.partitions[key] = pb
		}
		b.mu.Unlock()
	}

	pb.Add(record)

	return uint64(pb.Len())
}

func (b *MemoryPartitionedBuffer[K, R]) FlushWithoutReset() map[K][]R {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make(map[K][]R)
	for key, buffer := range b.partitions {
		copied[key] = buffer.FlushWithoutReset()
	}
	return copied
}

func (b *MemoryPartitionedBuffer[K, R]) Recycle(partitionKey K, records []R) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if pb, ok := b.partitions[partitionKey]; ok {
		pb.Recycle(records)
	}
}

func (b *MemoryPartitionedBuffer[K, R]) Remove(key K) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.partitions, key)
}

func (b *MemoryPartitionedBuffer[K, R]) Has(key K) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.partitions[key]
	return ok
}

func (b *MemoryPartitionedBuffer[K, R]) CopyAndReset(key K) []R {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.partitions[key]; ok {
		return b.partitions[key].CopyAndReset()
	}

	return nil
}

func (b *MemoryPartitionedBuffer[K, R]) Read(key K) []R {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.partitions[key]; ok {
		return b.partitions[key].Read()
	}

	return nil
}

func (b *MemoryPartitionedBuffer[K, R]) PriorityPrepend(key K, records []R) {
	b.mu.RLock()
	pb, ok := b.partitions[key]
	b.mu.RUnlock()
	if !ok {
		b.mu.Lock()
		if pb, ok = b.partitions[key]; !ok {
			pb = NewMemoryBuffer[R](b.partitionBufferCapacity)
			b.partitions[key] = pb
		}
		b.mu.Unlock()
	}
	pb.PriorityPrepend(records)
}

// PartitionBuffer provides a thread-safe buffer for storing internet latency samples.
// It supports concurrent appends and atomic flushing, as well as a maximum capacity
// with backpressure to avoid having too many records in the buffer at once.
type MemoryBuffer[R Record] struct {
	mu          sync.Mutex
	pool        sync.Pool
	records     []R
	maxCapacity int
	cond        *sync.Cond
}

func NewMemoryBuffer[R Record](capacity int) *MemoryBuffer[R] {
	pb := &MemoryBuffer[R]{
		records: make([]R, 0, capacity),
		pool: sync.Pool{
			New: func() any {
				return make([]R, 0, capacity)
			},
		},
		maxCapacity: capacity,
	}
	pb.cond = sync.NewCond(&pb.mu)
	return pb
}

func (b *MemoryBuffer[R]) Add(record R) {
	b.mu.Lock()
	for len(b.records) >= b.maxCapacity {
		b.cond.Wait()
	}
	b.records = append(b.records, record)
	b.mu.Unlock()
}

func (b *MemoryBuffer[R]) TryAdd(record R) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) >= b.maxCapacity {
		return false
	}
	b.records = append(b.records, record)
	return true
}

func (b *MemoryBuffer[R]) CopyAndReset() []R {
	tmp := b.pool.Get().([]R)
	tmp = tmp[:0] // reuse capacity

	b.mu.Lock()
	tmp = append(tmp, b.records...)
	b.records = b.records[:0]
	b.cond.Broadcast() // wake up any blocked Add calls
	b.mu.Unlock()

	return tmp
}

func (b *MemoryBuffer[R]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

func (b *MemoryBuffer[R]) FlushWithoutReset() []R {
	b.mu.Lock()
	defer b.mu.Unlock()
	tmp := make([]R, len(b.records))
	copy(tmp, b.records)
	return tmp
}

func (b *MemoryBuffer[R]) Recycle(buf []R) {
	// Reset the slice length before returning it to the pool to ensure that future users see an
	// empty slice, even though the underlying capacity is preserved for reuse.
	b.pool.Put(buf[:0])
}

func (b *MemoryBuffer[R]) Read() []R {
	b.mu.Lock()
	defer b.mu.Unlock()

	copied := make([]R, len(b.records))
	copy(copied, b.records)
	return copied
}

func (b *MemoryBuffer[R]) PriorityPrepend(records []R) {
	b.mu.Lock()
	// Build new slice sized exactly to fit all records; ignores maxCapacity intentionally to
	// prevent blocking during retry or priority scenarios.
	newLen := len(records) + len(b.records)
	newBuf := make([]R, 0, newLen)
	newBuf = append(newBuf, records...)
	newBuf = append(newBuf, b.records...)
	b.records = newBuf
	// Broadcast to wake up any blocked producers for consistency with the Add method's wait condition.
	b.cond.Broadcast()
	b.mu.Unlock()
}
