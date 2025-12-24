package enricher

import (
	"context"
	"encoding/json"
	"io"
	"os"
)

// StdoutWriter implements FlowWriter for writing FlowSamples as JSON to an io.Writer.
type StdoutWriter struct {
	writer  io.Writer
	encoder *json.Encoder
}

type StdoutWriterOption func(*StdoutWriter)

// WithWriter sets a custom writer (defaults to os.Stdout).
func WithWriter(w io.Writer) StdoutWriterOption {
	return func(s *StdoutWriter) {
		s.writer = w
	}
}

func NewStdoutWriter(opts ...StdoutWriterOption) *StdoutWriter {
	s := &StdoutWriter{
		writer: os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.encoder = json.NewEncoder(s.writer)
	return s
}

// BatchInsert writes each FlowSample as a JSON line to the configured writer.
func (s *StdoutWriter) BatchInsert(ctx context.Context, samples []FlowSample) error {
	for _, sample := range samples {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.encoder.Encode(sample); err != nil {
			return err
		}
	}
	return nil
}
