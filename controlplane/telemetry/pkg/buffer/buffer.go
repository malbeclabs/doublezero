package buffer

import (
	"sync"
)

type PartitionKey interface {
	comparable
}

type Record any

type PartitionedBuffer[K PartitionKey, R Record] struct {
	mu                      sync.RWMutex
	partitions              map[K]*PartitionBuffer[R]
	partitionBufferCapacity int
}

func NewPartitionedBuffer[K PartitionKey, R Record](partitionBufferCapacity int) *PartitionedBuffer[K, R] {
	return &PartitionedBuffer[K, R]{
		partitions:              make(map[K]*PartitionBuffer[R]),
		partitionBufferCapacity: partitionBufferCapacity,
	}
}

func (b *PartitionedBuffer[K, R]) Add(key K, record R) uint64 {
	b.mu.RLock()
	pb, ok := b.partitions[key]
	b.mu.RUnlock()

	if !ok {
		b.mu.Lock()
		if pb, ok = b.partitions[key]; !ok {
			pb = NewPartitionBuffer[R](b.partitionBufferCapacity)
			b.partitions[key] = pb
		}
		b.mu.Unlock()
	}

	pb.Add(record)

	return uint64(pb.Len())
}

func (b *PartitionedBuffer[K, R]) FlushWithoutReset() map[K][]R {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make(map[K][]R)
	for key, buffer := range b.partitions {
		copied[key] = buffer.FlushWithoutReset()
	}
	return copied
}

func (b *PartitionedBuffer[K, R]) Recycle(partitionKey K, records []R) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if pb, ok := b.partitions[partitionKey]; ok {
		pb.Recycle(records)
	}
}

func (b *PartitionedBuffer[K, R]) Remove(key K) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.partitions, key)
}

func (b *PartitionedBuffer[K, R]) Has(key K) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.partitions[key]
	return ok
}

func (b *PartitionedBuffer[K, R]) CopyAndReset(key K) []R {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.partitions[key]; ok {
		return b.partitions[key].CopyAndReset()
	}

	return nil
}

func (b *PartitionedBuffer[K, R]) Read(key K) []R {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.partitions[key]; ok {
		return b.partitions[key].Read()
	}

	return nil
}

// PartitionBuffer provides a thread-safe buffer for storing internet latency samples.
// It supports concurrent appends and atomic flushing, as well as a maximum capacity
// with backpressure to avoid having too many records in the buffer at once.
type PartitionBuffer[R Record] struct {
	mu          sync.Mutex
	pool        sync.Pool
	records     []R
	maxCapacity int
	cond        *sync.Cond
}

func NewPartitionBuffer[R Record](capacity int) *PartitionBuffer[R] {
	pb := &PartitionBuffer[R]{
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

func (b *PartitionBuffer[R]) Add(record R) {
	b.mu.Lock()
	for len(b.records) >= b.maxCapacity {
		b.cond.Wait()
	}
	b.records = append(b.records, record)
	b.mu.Unlock()
}

func (b *PartitionBuffer[R]) TryAdd(record R) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) >= b.maxCapacity {
		return false
	}
	b.records = append(b.records, record)
	return true
}

func (b *PartitionBuffer[R]) CopyAndReset() []R {
	tmp := b.pool.Get().([]R)
	tmp = tmp[:0] // reuse capacity

	b.mu.Lock()
	tmp = append(tmp, b.records...)
	b.records = b.records[:0]
	b.cond.Broadcast() // wake up any blocked Add calls
	b.mu.Unlock()

	return tmp
}

func (b *PartitionBuffer[R]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

func (b *PartitionBuffer[R]) FlushWithoutReset() []R {
	b.mu.Lock()
	defer b.mu.Unlock()
	tmp := make([]R, len(b.records))
	copy(tmp, b.records)
	return tmp
}

func (b *PartitionBuffer[R]) Recycle(buf []R) {
	// Reset the slice length before returning it to the pool to ensure that future users see an
	// empty slice, even though the underlying capacity is preserved for reuse.
	b.pool.Put(buf[:0])
}

func (b *PartitionBuffer[R]) Read() []R {
	b.mu.Lock()
	defer b.mu.Unlock()

	copied := make([]R, len(b.records))
	copy(copied, b.records)
	return copied
}
