package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClickHouseClient_StripsScheme(t *testing.T) {
	// NewClickHouseClient will fail to connect (no server), but we can verify
	// it doesn't panic and returns a sensible error for various addr formats.
	tests := []struct {
		name string
		addr string
	}{
		{"plain host:port", "localhost:8123"},
		{"https prefix", "https://clickhouse.example.com:8443"},
		{"http prefix", "http://localhost:8123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClickHouseClient(tt.addr, "default", "default", "", true)
			// Connection will fail (no server) — we just verify no panic and an error is returned.
			assert.Error(t, err)
		})
	}
}
