package telemetry

import (
	"fmt"
	"sync"

	"github.com/gagliardetto/solana-go"
)

const (
	defaultAccountBufferCapacity = 1024
)

type AccountKey struct {
	OriginDevicePK solana.PublicKey
	TargetDevicePK solana.PublicKey
	LinkPK         solana.PublicKey
	Epoch          uint64
}

func (k AccountKey) String() string {
	return fmt.Sprintf("%s-%s-%s-%d", k.OriginDevicePK.String(), k.TargetDevicePK.String(), k.LinkPK.String(), k.Epoch)
}

type AccountsBuffer struct {
	mu       sync.RWMutex
	accounts map[AccountKey]*AccountBuffer
}

func NewAccountsBuffer() *AccountsBuffer {
	return &AccountsBuffer{
		accounts: make(map[AccountKey]*AccountBuffer),
	}
}

func (b *AccountsBuffer) Add(key AccountKey, sample Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.accounts[key]; !ok {
		b.accounts[key] = NewAccountBuffer(defaultAccountBufferCapacity)
	}

	b.accounts[key].Add(sample)
}

func (b *AccountsBuffer) FlushWithoutReset() map[AccountKey][]Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make(map[AccountKey][]Sample)
	for key, buffer := range b.accounts {
		copied[key] = buffer.FlushWithoutReset()
	}
	return copied
}

func (b *AccountsBuffer) Recycle(accountKey AccountKey, samples []Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.accounts[accountKey].Recycle(samples)
}

func (b *AccountsBuffer) CopyAndReset(key AccountKey) []Sample {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.accounts[key].CopyAndReset()
}

func (b *AccountsBuffer) Read(key AccountKey) []Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.accounts[key].Read()
}

// AccountBuffer provides a thread-safe buffer for storing telemetry samples
// collected during probing. It supports concurrent appends and atomic flushing.
type AccountBuffer struct {
	mu      sync.RWMutex
	pool    sync.Pool
	samples []Sample
}

func NewAccountBuffer(capacity int) *AccountBuffer {
	return &AccountBuffer{
		samples: make([]Sample, 0, capacity),
		pool: sync.Pool{
			New: func() any {
				return make([]Sample, 0, capacity)
			},
		},
	}
}

func (b *AccountBuffer) Add(sample Sample) {
	b.mu.Lock()
	b.samples = append(b.samples, sample)
	b.mu.Unlock()
}

func (b *AccountBuffer) CopyAndReset() []Sample {
	tmp := b.pool.Get().([]Sample)
	tmp = tmp[:0] // reuse capacity

	b.mu.Lock()
	tmp = append(tmp, b.samples...)
	b.samples = b.samples[:0]
	b.mu.Unlock()

	return tmp
}

func (b *AccountBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.samples)
}

func (b *AccountBuffer) FlushWithoutReset() []Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()
	tmp := make([]Sample, len(b.samples))
	copy(tmp, b.samples)
	return tmp
}

func (b *AccountBuffer) Recycle(buf []Sample) {
	b.pool.Put(buf)
}

func (b *AccountBuffer) Read() []Sample {
	b.mu.RLock()
	defer b.mu.RUnlock()

	copied := make([]Sample, len(b.samples))
	copy(copied, b.samples)
	return copied
}
