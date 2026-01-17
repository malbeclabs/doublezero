package isis

import (
	"context"
	"time"
)

// Dump represents raw IS-IS data fetched from a source.
type Dump struct {
	FetchedAt time.Time
	RawJSON   []byte
	FileName  string
}

// Source provides access to IS-IS routing database dumps.
// Implementations exist for S3 (current) and ClickHouse (future).
type Source interface {
	// FetchLatest retrieves the most recent IS-IS database dump.
	FetchLatest(ctx context.Context) (*Dump, error)

	// Close releases any resources held by the source.
	Close() error
}
