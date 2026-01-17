package isis

import (
	"context"
	"time"
)

// MockSource is a Source implementation for testing.
type MockSource struct {
	Dump     *Dump
	FetchErr error
	Closed   bool
}

// NewMockSource creates a new MockSource with the given dump data.
func NewMockSource(rawJSON []byte, fileName string) *MockSource {
	return &MockSource{
		Dump: &Dump{
			FetchedAt: time.Now(),
			RawJSON:   rawJSON,
			FileName:  fileName,
		},
	}
}

// FetchLatest returns the configured dump or error.
func (m *MockSource) FetchLatest(ctx context.Context) (*Dump, error) {
	if m.FetchErr != nil {
		return nil, m.FetchErr
	}
	return m.Dump, nil
}

// Close marks the source as closed.
func (m *MockSource) Close() error {
	m.Closed = true
	return nil
}
