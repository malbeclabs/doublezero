package telemetry

import (
	"sync"
)

// SampleBuffer provides a thread-safe buffer for storing telemetry samples
// collected during probing. It supports concurrent appends and atomic flushing.
type SampleBuffer struct {
	mu      sync.RWMutex
	pool    sync.Pool
	samples []Sample
}

func NewSampleBuffer(capacity int) *SampleBuffer {
	return &SampleBuffer{
		samples: make([]Sample, 0, capacity),
		pool: sync.Pool{
			New: func() any {
				return make([]Sample, 0, capacity)
			},
		},
	}
}

func (b *SampleBuffer) Add(sample Sample) {
	b.mu.Lock()
	b.samples = append(b.samples, sample)
	b.mu.Unlock()
}

func (b *SampleBuffer) CopyAndReset() []Sample {
	tmp := b.pool.Get().([]Sample)
	tmp = tmp[:0] // reuse capacity

	b.mu.Lock()
	tmp = append(tmp, b.samples...)
	b.samples = b.samples[:0]
	b.mu.Unlock()

	return tmp
}

func (b *SampleBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.samples)
}

func (b *SampleBuffer) FlushWithoutReset() []Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()
	tmp := make([]Sample, len(b.samples))
	copy(tmp, b.samples)
	return tmp
}

func (b *SampleBuffer) Recycle(buf []Sample) {
	b.pool.Put(buf)
}

func (b *SampleBuffer) Read() []Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make([]Sample, len(b.samples))
	copy(copied, b.samples)
	return copied
}
