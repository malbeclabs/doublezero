package edge

import (
	"fmt"
	"strings"
)

// OutputSink writes decoded records to a destination.
type OutputSink interface {
	// Write outputs one or more records.
	Write(records []Record) error

	// Close releases any resources held by the sink.
	Close() error
}

// SinkConfig describes the desired output format and destination.
type SinkConfig struct {
	// Format is the output encoding: "json" or "csv".
	Format string

	// Path is the output destination. A file path for file output,
	// or "unix:///path/to/sock" for a Unix domain socket.
	Path string
}

// NewSink creates an OutputSink from the given configuration.
//
// Path formats:
//   - "/path/to/file"          → file output
//   - "unix:///path/to/sock"   → Unix domain socket (broadcast to all connected clients)
func NewSink(cfg SinkConfig) (OutputSink, error) {
	isSocket := strings.HasPrefix(cfg.Path, "unix://")

	switch cfg.Format {
	case "json":
		if isSocket {
			return NewSocketSink("json", strings.TrimPrefix(cfg.Path, "unix://"))
		}
		return NewJSONFileSink(cfg.Path)
	case "csv":
		if isSocket {
			return NewSocketSink("csv", strings.TrimPrefix(cfg.Path, "unix://"))
		}
		return NewCSVFileSink(cfg.Path)
	default:
		return nil, fmt.Errorf("unknown output format: %q", cfg.Format)
	}
}
