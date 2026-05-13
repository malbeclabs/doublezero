package qa

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestForValidDevices builds a minimal *Test from a slice of devices.
// ValidDevices only depends on t.log and t.devices.
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

// codesOf returns the sorted codes returned by ValidDevices, for easy
// comparison against expected sets.
func codesOf(devices []*Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Code)
	}
	return out
}

func TestValidDevices_Unicast(t *testing.T) {
	t.Parallel()

	const minCapacity = 2

	tests := []struct {
		name              string
		devices           []*Device
		skipCapacityCheck bool
		want              []string
	}{
		{
			name: "per-type max set with free slots is included",
			devices: []*Device{
				{Code: "alpha", MaxUsers: 96, UsersCount: 4, MaxUnicastUsers: 48, UnicastUsersCount: 4},
			},
			want: []string{"alpha"},
		},
		{
			name: "per-type max set and saturated is excluded (preserves #3563 fix)",
			devices: []*Device{
				{Code: "nyc002-dz002", MaxUsers: 96, UsersCount: 29, MaxUnicastUsers: 29, UnicastUsersCount: 29},
			},
			want: []string{},
		},
		{
			name: "per-type max set with fewer than minCapacity free slots is excluded",
			devices: []*Device{
				{Code: "bravo", MaxUsers: 96, UsersCount: 47, MaxUnicastUsers: 48, UnicastUsersCount: 47},
			},
			want: []string{},
		},
		{
			name: "per-type max zero with users counted is included (regression fix)",
			devices: []*Device{
				{Code: "frankfurt-edge", MaxUsers: 96, UsersCount: 12, MaxUnicastUsers: 0, UnicastUsersCount: 12},
			},
			want: []string{"frankfurt-edge"},
		},
		{
			name: "per-type max zero with aggregate cap saturated is excluded",
			devices: []*Device{
				{Code: "full", MaxUsers: 5, UsersCount: 4, MaxUnicastUsers: 0, UnicastUsersCount: 4},
			},
			want: []string{},
		},
		{
			name: "device with test in code is excluded",
			devices: []*Device{
				{Code: "lab-test-1", MaxUsers: 96, UsersCount: 0, MaxUnicastUsers: 48, UnicastUsersCount: 0},
			},
			want: []string{},
		},
		{
			name: "skipCapacityCheck includes saturated and zero-max devices",
			devices: []*Device{
				{Code: "alpha", MaxUsers: 96, UsersCount: 4, MaxUnicastUsers: 48, UnicastUsersCount: 4},
				{Code: "nyc002-dz002", MaxUsers: 96, UsersCount: 29, MaxUnicastUsers: 29, UnicastUsersCount: 29},
				{Code: "frankfurt-edge", MaxUsers: 96, UsersCount: 12, MaxUnicastUsers: 0, UnicastUsersCount: 12},
				{Code: "full", MaxUsers: 5, UsersCount: 4, MaxUnicastUsers: 0, UnicastUsersCount: 4},
			},
			skipCapacityCheck: true,
			want:              []string{"alpha", "frankfurt-edge", "full", "nyc002-dz002"},
		},
		{
			name: "mainnet-beta-like mix returns only those with free per-type or unset cap",
			devices: []*Device{
				{Code: "allnodes-fra1", MaxUsers: 96, UsersCount: 29, MaxUnicastUsers: 48, UnicastUsersCount: 29},
				{Code: "fra-velia", MaxUsers: 96, UsersCount: 4, MaxUnicastUsers: 48, UnicastUsersCount: 4},
				{Code: "frankry", MaxUsers: 128, UsersCount: 68, MaxUnicastUsers: 96, UnicastUsersCount: 68},
				{Code: "nyc002-dz002", MaxUsers: 96, UsersCount: 29, MaxUnicastUsers: 29, UnicastUsersCount: 29},
				{Code: "amsterdam-edge", MaxUsers: 96, UsersCount: 7, MaxUnicastUsers: 0, UnicastUsersCount: 7},
				{Code: "tokyo-edge", MaxUsers: 96, UsersCount: 2, MaxUnicastUsers: 0, UnicastUsersCount: 2},
			},
			want: []string{"allnodes-fra1", "amsterdam-edge", "fra-velia", "frankry", "tokyo-edge"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tt := newTestForValidDevices(tc.devices)
			got := codesOf(tt.ValidDevices(DeviceUserTypeUnicast, minCapacity, tc.skipCapacityCheck))
			require.Equal(t, tc.want, got)
		})
	}
}

func TestValidDevices_MulticastPublisher(t *testing.T) {
	t.Parallel()

	const minCapacity = 1

	tests := []struct {
		name    string
		devices []*Device
		want    []string
	}{
		{
			name: "per-type max set with free slots is included",
			devices: []*Device{
				{Code: "pub-ok", MaxUsers: 96, UsersCount: 0, MaxMulticastPublishers: 4, MulticastPublishersCount: 1},
			},
			want: []string{"pub-ok"},
		},
		{
			name: "per-type max set and saturated is excluded",
			devices: []*Device{
				{Code: "pub-full", MaxUsers: 96, UsersCount: 0, MaxMulticastPublishers: 1, MulticastPublishersCount: 1},
			},
			want: []string{},
		},
		{
			name: "per-type max zero with publishers counted is included (regression fix)",
			devices: []*Device{
				{Code: "pub-uncapped", MaxUsers: 96, UsersCount: 3, MaxMulticastPublishers: 0, MulticastPublishersCount: 3},
			},
			want: []string{"pub-uncapped"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tt := newTestForValidDevices(tc.devices)
			got := codesOf(tt.ValidDevices(DeviceUserTypeMulticastPublisher, minCapacity, false))
			require.Equal(t, tc.want, got)
		})
	}
}

func TestValidDevices_MulticastSubscriber(t *testing.T) {
	t.Parallel()

	const minCapacity = 1

	tests := []struct {
		name    string
		devices []*Device
		want    []string
	}{
		{
			name: "per-type max set with free slots is included",
			devices: []*Device{
				{Code: "sub-ok", MaxUsers: 96, UsersCount: 0, MaxMulticastSubscribers: 8, MulticastSubscribersCount: 2},
			},
			want: []string{"sub-ok"},
		},
		{
			name: "per-type max set and saturated is excluded",
			devices: []*Device{
				{Code: "sub-full", MaxUsers: 96, UsersCount: 0, MaxMulticastSubscribers: 2, MulticastSubscribersCount: 2},
			},
			want: []string{},
		},
		{
			name: "per-type max zero with subscribers counted is included (regression fix)",
			devices: []*Device{
				{Code: "sub-uncapped", MaxUsers: 96, UsersCount: 5, MaxMulticastSubscribers: 0, MulticastSubscribersCount: 5},
			},
			want: []string{"sub-uncapped"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tt := newTestForValidDevices(tc.devices)
			got := codesOf(tt.ValidDevices(DeviceUserTypeMulticastSubscriber, minCapacity, false))
			require.Equal(t, tc.want, got)
		})
	}
}
