package telemetry_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestDeriveEpoch(t *testing.T) {
	cases := []struct {
		name     string
		input    time.Time
		expected uint64
	}{
		{
			name:     "unix epoch start",
			input:    time.Unix(0, 0).UTC(),
			expected: 0,
		},
		{
			name:     "just before first epoch ends",
			input:    time.Unix(172799, 0).UTC(), // 2 days - 1 second
			expected: 0,
		},
		{
			name:     "exactly at second epoch start",
			input:    time.Unix(172800, 0).UTC(), // 2 days
			expected: 1,
		},
		{
			name:     "random modern time",
			input:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: uint64(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix() / (60 * 60 * 24 * 2)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := telemetry.DeriveEpoch(tc.input)
			require.Equal(t, tc.expected, got)
		})
	}
}
