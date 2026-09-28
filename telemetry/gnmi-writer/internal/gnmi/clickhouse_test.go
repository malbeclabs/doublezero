package gnmi

import (
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestIsRetryableClickhouseError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "table not found (code 60)",
			err:      &clickhouse.Exception{Code: 60, Message: "Table does not exist"},
			expected: false,
		},
		{
			name:     "wrapped table not found",
			err:      fmt.Errorf("write failed: %w", &clickhouse.Exception{Code: 60}),
			expected: false,
		},
		{
			name:     "other clickhouse error is retryable",
			err:      &clickhouse.Exception{Code: 999, Message: "Some other error"},
			expected: true,
		},
		{
			name:     "non-clickhouse error is retryable",
			err:      fmt.Errorf("network timeout"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableClickhouseError(tt.err); got != tt.expected {
				t.Errorf("IsRetryableClickhouseError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClickhouseRecordWriterOptionsConnMaxLifetime(t *testing.T) {
	cw := &ClickhouseRecordWriter{addr: "localhost:9440"}
	if got := cw.options().ConnMaxLifetime; got != 3*time.Minute {
		t.Errorf("ConnMaxLifetime = %v, want 3m", got)
	}
}
