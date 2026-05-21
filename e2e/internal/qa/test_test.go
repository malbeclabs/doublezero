package qa

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestForValidDevices(devices []*Device) *Test {
	deviceMap := make(map[string]*Device, len(devices))
	for _, d := range devices {
		deviceMap[d.Code] = d
	}
	return &Test{
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		devices: deviceMap,
	}
}

func codesOf(devices []*Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Code)
	}
	return out
}

func TestValidDevices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		devices []*Device
		want    []string
	}{
		{
			name: "normal device is included",
			devices: []*Device{
				{Code: "fra-velia", MaxUsers: 96, UsersCount: 4},
			},
			want: []string{"fra-velia"},
		},
		{
			name: "device with test in code is excluded",
			devices: []*Device{
				{Code: "lab-test-1", MaxUsers: 96, UsersCount: 0},
			},
			want: []string{},
		},
		{
			name: "device with TEST in code (case insensitive) is excluded",
			devices: []*Device{
				{Code: "NYC-TEST-DZ001", MaxUsers: 96, UsersCount: 0},
			},
			want: []string{},
		},
		{
			name: "mix of real and test devices returns only real ones sorted",
			devices: []*Device{
				{Code: "tokyo-edge"},
				{Code: "test-device-1"},
				{Code: "amsterdam-edge"},
				{Code: "fra-test-01"},
			},
			want: []string{"amsterdam-edge", "tokyo-edge"},
		},
		{
			name: "empty device list returns empty",
			devices: []*Device{},
			want:    []string{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tt := newTestForValidDevices(tc.devices)
			got := codesOf(tt.ValidDevices())
			require.Equal(t, tc.want, got)
		})
	}
}
