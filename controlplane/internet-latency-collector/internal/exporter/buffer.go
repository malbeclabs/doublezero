package exporter

import (
	"fmt"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
)

type PartitionKey struct {
	DataProvider     DataProviderName
	SourceLocationPK solana.PublicKey
	TargetLocationPK solana.PublicKey
	Epoch            uint64
}

type Sample struct {
	Timestamp time.Time
	RTT       time.Duration
}

func (k PartitionKey) String() string {
	return fmt.Sprintf("%s-%s-%s-%d", k.DataProvider, k.SourceLocationPK.String(), k.TargetLocationPK.String(), k.Epoch)
}

type PartitionedBuffer struct {
	mu                      sync.RWMutex
	partitions              map[PartitionKey]*PartitionBuffer
	partitionBufferCapacity int
}

func NewPartitionedBuffer(partitionBufferCapacity int) *PartitionedBuffer {
	return &PartitionedBuffer{
		partitions:              make(map[PartitionKey]*PartitionBuffer),
		partitionBufferCapacity: partitionBufferCapacity,
	}
}

func (b *PartitionedBuffer) Add(key PartitionKey, record Sample) uint64 {
	b.mu.RLock()
	pb, ok := b.partitions[key]
	b.mu.RUnlock()

	if !ok {
		b.mu.Lock()
		if pb, ok = b.partitions[key]; !ok {
			pb = NewPartitionBuffer(b.partitionBufferCapacity)
			b.partitions[key] = pb
		}
		b.mu.Unlock()
	}

	pb.Add(record)

	return uint64(pb.Len())
}

func (b *PartitionedBuffer) FlushWithoutReset() map[PartitionKey][]Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make(map[PartitionKey][]Sample)
	for key, buffer := range b.partitions {
		copied[key] = buffer.FlushWithoutReset()
	}
	return copied
}

func (b *PartitionedBuffer) Recycle(partitionKey PartitionKey, records []Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if pb, ok := b.partitions[partitionKey]; ok {
		pb.Recycle(records)
	}
}

func (b *PartitionedBuffer) Remove(key PartitionKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.partitions, key)
}

func (b *PartitionedBuffer) Has(key PartitionKey) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.partitions[key]
	return ok
}

func (b *PartitionedBuffer) CopyAndReset(key PartitionKey) []Sample {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.partitions[key]; ok {
		return b.partitions[key].CopyAndReset()
	}

	return nil
}

func (b *PartitionedBuffer) Read(key PartitionKey) []Sample {
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
type PartitionBuffer struct {
	mu          sync.Mutex
	pool        sync.Pool
	records     []Sample
	maxCapacity int
	cond        *sync.Cond
}

func NewPartitionBuffer(capacity int) *PartitionBuffer {
	pb := &PartitionBuffer{
		records: make([]Sample, 0, capacity),
		pool: sync.Pool{
			New: func() any {
				return make([]Sample, 0, capacity)
			},
		},
		maxCapacity: capacity,
	}
	pb.cond = sync.NewCond(&pb.mu)
	return pb
}

func (b *PartitionBuffer) Add(record Sample) {
	b.mu.Lock()
	for len(b.records) >= b.maxCapacity {
		b.cond.Wait()
	}
	b.records = append(b.records, record)
	b.mu.Unlock()
}

func (b *PartitionBuffer) TryAdd(record Sample) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.records) >= b.maxCapacity {
		return false
	}
	b.records = append(b.records, record)
	return true
}

func (b *PartitionBuffer) CopyAndReset() []Sample {
	tmp := b.pool.Get().([]Sample)
	tmp = tmp[:0] // reuse capacity

	b.mu.Lock()
	tmp = append(tmp, b.records...)
	b.records = b.records[:0]
	b.cond.Broadcast() // wake up any blocked Add calls
	b.mu.Unlock()

	return tmp
}

func (b *PartitionBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.records)
}

func (b *PartitionBuffer) FlushWithoutReset() []Sample {
	b.mu.Lock()
	defer b.mu.Unlock()
	tmp := make([]Sample, len(b.records))
	copy(tmp, b.records)
	return tmp
}

func (b *PartitionBuffer) Recycle(buf []Sample) {
	// Reset the slice length before returning it to the pool to ensure that future users see an
	// empty slice, even though the underlying capacity is preserved for reuse.
	b.pool.Put(buf[:0])
}

func (b *PartitionBuffer) Read() []Sample {
	b.mu.Lock()
	defer b.mu.Unlock()

	copied := make([]Sample, len(b.records))
	copy(copied, b.records)
	return copied
}
