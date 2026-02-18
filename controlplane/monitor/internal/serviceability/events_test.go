package serviceability

import (
	"crypto/sha256"
	"testing"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
)

func newTestDevice(id string, status serviceability.DeviceStatus) serviceability.Device {
	pubKey := sha256.Sum256([]byte("device-" + id))
	return serviceability.Device{
		Code:   "device-" + id,
		PubKey: pubKey,
		Status: status,
		Interfaces: []serviceability.Interface{
			{
				Name:  "interface-" + id,
				IpNet: [5]uint8{10, 0, 0, 1, 31},
			},
		},
	}
}

func newTestLink(id string, status serviceability.LinkStatus) serviceability.Link {
	pubKey := sha256.Sum256([]byte("link-" + id))
	return serviceability.Link{
		Code:      "link-" + id,
		PubKey:    pubKey,
		Status:    status,
		TunnelNet: [5]uint8{10, 0, 0, 0, 31},
	}
}

func TestCompare(t *testing.T) {
	deviceTests := []struct {
		name             string
		before           []serviceability.Device
		after            []serviceability.Device
		expectedAdded    int
		expectedRemoved  int
		expectedModified int
	}{
		{
			name:   "no changes",
			before: []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated)},
			after:  []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated)},
		},
		{
			name:          "item added",
			before:        []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated)},
			after:         []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated), newTestDevice("2", serviceability.DeviceStatusPending)},
			expectedAdded: 1,
		},
		{
			name:            "item removed",
			before:          []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated), newTestDevice("2", serviceability.DeviceStatusPending)},
			after:           []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated)},
			expectedRemoved: 1,
		},
		{
			name:             "item modified",
			before:           []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusPending)},
			after:            []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated)},
			expectedModified: 1,
		},
		{
			name:             "multiple event types",
			before:           []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusActivated), newTestDevice("2", serviceability.DeviceStatusPending)}, // 2 is removed
			after:            []serviceability.Device{newTestDevice("1", serviceability.DeviceStatusDeleting), newTestDevice("3", serviceability.DeviceStatusPending)},  // 1 is modified, 3 is added
			expectedAdded:    1,
			expectedRemoved:  1,
			expectedModified: 1,
		},
	}

	t.Run("Devices", func(t *testing.T) {
		for _, tc := range deviceTests {
			t.Run(tc.name, func(t *testing.T) {
				events := CompareDevice(tc.before, tc.after)

				var added, removed, modified int
				for _, event := range events {
					switch event.Type() {
					case EventTypeAdded:
						added++
					case EventTypeRemoved:
						removed++
					case EventTypeModified:
						modified++
						assert.NotEmpty(t, event.Diff(), "Modified event should have a non-empty diff")
					}
				}

				assert.Equal(t, tc.expectedAdded, added, "Incorrect number of added events")
				assert.Equal(t, tc.expectedRemoved, removed, "Incorrect number of removed events")
				assert.Equal(t, tc.expectedModified, modified, "Incorrect number of modified events")
				assert.Equal(t, tc.expectedAdded+tc.expectedRemoved+tc.expectedModified, len(events), "Incorrect total number of events")
			})
		}
	})

	linkTests := []struct {
		name             string
		before           []serviceability.Link
		after            []serviceability.Link
		expectedAdded    int
		expectedRemoved  int
		expectedModified int
	}{
		{
			name:   "no changes",
			before: []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated)},
			after:  []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated)},
		},
		{
			name:          "item added",
			before:        []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated)},
			after:         []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated), newTestLink("2", serviceability.LinkStatusPending)},
			expectedAdded: 1,
		},
		{
			name:            "item removed",
			before:          []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated), newTestLink("2", serviceability.LinkStatusPending)},
			after:           []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated)},
			expectedRemoved: 1,
		},
		{
			name:             "item modified",
			before:           []serviceability.Link{newTestLink("1", serviceability.LinkStatusPending)},
			after:            []serviceability.Link{newTestLink("1", serviceability.LinkStatusActivated)},
			expectedModified: 1,
		},
	}

	t.Run("Links", func(t *testing.T) {
		for _, tc := range linkTests {
			t.Run(tc.name, func(t *testing.T) {
				events := CompareLink(tc.before, tc.after)

				var added, removed, modified int
				for _, event := range events {
					switch event.Type() {
					case EventTypeAdded:
						added++
					case EventTypeRemoved:
						removed++
					case EventTypeModified:
						modified++
						assert.NotEmpty(t, event.Diff(), "Modified event should have a non-empty diff")
					}
				}

				assert.Equal(t, tc.expectedAdded, added, "Incorrect number of added events")
				assert.Equal(t, tc.expectedRemoved, removed, "Incorrect number of removed events")
				assert.Equal(t, tc.expectedModified, modified, "Incorrect number of modified events")
				assert.Equal(t, tc.expectedAdded+tc.expectedRemoved+tc.expectedModified, len(events), "Incorrect total number of events")
			})
		}
	})
}
