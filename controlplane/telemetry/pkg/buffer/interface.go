package buffer

type PartitionKey interface {
	comparable
}

type Record any

type PartitionedBuffer[K PartitionKey, R Record] interface {
	Add(key K, record R) uint64
	FlushWithoutReset() map[K][]R
	Recycle(partitionKey K, records []R)
	Remove(key K)
	Has(key K) bool
	CopyAndReset(key K) []R
	Read(key K) []R
}
