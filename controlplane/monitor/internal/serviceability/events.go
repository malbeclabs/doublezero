package serviceability

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
)

type ServiceabilityEventer interface {
	Diff() string
	Id() string
	PubKey() string
	Type() EventType
	EntityType() EntityType
}

type EventType uint8

const (
	EventTypeUnknown EventType = iota
	EventTypeAdded
	EventTypeRemoved
	EventTypeModified
)

func (e EventType) String() string {
	switch e {
	case EventTypeAdded:
		return "added"
	case EventTypeRemoved:
		return "removed"
	case EventTypeModified:
		return "modified"
	default:
		return "unknown"
	}
}

type EntityType uint8

const (
	EntityTypeUnknown EntityType = iota
	EntityTypeDevice
	EntityTypeLink
	EntityTypeUser
)

func (e EntityType) String() string {
	switch e {
	case EntityTypeDevice:
		return "device"
	case EntityTypeLink:
		return "link"
	case EntityTypeUser:
		return "user"
	default:
		return "unknown"
	}
}

type ServiceabilityDeviceEvent struct {
	eventType EventType
	Device    serviceability.Device
	diff      string
}

func (e ServiceabilityDeviceEvent) Diff() string {
	return e.diff
}

func (e ServiceabilityDeviceEvent) EntityType() EntityType {
	return EntityTypeDevice
}

func (e ServiceabilityDeviceEvent) Id() string {
	return e.Device.Code
}

func (e ServiceabilityDeviceEvent) PubKey() string {
	return base58.Encode(e.Device.PubKey[:])
}

func (e ServiceabilityDeviceEvent) Type() EventType {
	return e.eventType
}

type ServiceabilityLinkEvent struct {
	eventType EventType
	Link      serviceability.Link
	diff      string
}

func (e ServiceabilityLinkEvent) Diff() string {
	return e.diff
}

func (e ServiceabilityLinkEvent) EntityType() EntityType {
	return EntityTypeLink
}

func (e ServiceabilityLinkEvent) Id() string {
	return e.Link.Code
}

func (e ServiceabilityLinkEvent) PubKey() string {
	return base58.Encode(e.Link.PubKey[:])
}

func (e ServiceabilityLinkEvent) Type() EventType {
	return e.eventType
}

type ServiceabilityUserEvent struct {
	eventType EventType
	User      serviceability.User
	diff      string
}

func (e ServiceabilityUserEvent) Diff() string {
	return e.diff
}

func (e ServiceabilityUserEvent) EntityType() EntityType {
	return EntityTypeUser
}

func (e ServiceabilityUserEvent) Id() string {
	return e.PubKey()
}

func (e ServiceabilityUserEvent) PubKey() string {
	return base58.Encode(e.User.PubKey[:])
}

func (e ServiceabilityUserEvent) Type() EventType {
	return e.eventType
}

// Compare is a generic that is able to compare two slices of common serviceability types (device, links, etc).
func Compare[T any, E ServiceabilityEventer](
	a, b []T,
	getPubKey func(T) string,
	newEvent func(eventType EventType, entity T, diff string) E,
	diff func(T, T) string,
) []E {
	var events []E
	oldMap := make(map[string]T)
	for _, item := range a {
		oldMap[getPubKey(item)] = item
	}

	newMap := make(map[string]T)
	for _, item := range b {
		newMap[getPubKey(item)] = item
	}

	// check for modified and added items.
	for key, newItem := range newMap {
		if oldItem, ok := oldMap[key]; ok {
			if !reflect.DeepEqual(oldItem, newItem) {
				d := diff(oldItem, newItem)
				events = append(events, newEvent(EventTypeModified, newItem, d))
			}
		} else {
			var zero T
			d := diff(zero, newItem)
			events = append(events, newEvent(EventTypeAdded, newItem, d))
		}
	}

	// check for removed items.
	for key, oldItem := range oldMap {
		if _, ok := newMap[key]; !ok {
			var zero T
			d := diff(oldItem, zero)
			events = append(events, newEvent(EventTypeRemoved, oldItem, d))
		}
	}
	return events
}

func CompareDevice(a, b []serviceability.Device) []ServiceabilityDeviceEvent {
	diffFunc := func(a, b serviceability.Device) string {
		return diffEntities(a, b, func(d serviceability.Device) string { return d.Code })
	}
	return Compare(a, b,
		func(d serviceability.Device) string { return base58.Encode(d.PubKey[:]) },
		func(et EventType, d serviceability.Device, df string) ServiceabilityDeviceEvent {
			return ServiceabilityDeviceEvent{eventType: et, Device: d, diff: df}
		},
		diffFunc,
	)
}

func CompareLink(a, b []serviceability.Link) []ServiceabilityLinkEvent {
	diffFunc := func(a, b serviceability.Link) string {
		return diffEntities(a, b, func(l serviceability.Link) string { return l.Code })
	}
	return Compare(a, b,
		func(l serviceability.Link) string { return base58.Encode(l.PubKey[:]) },
		func(et EventType, l serviceability.Link, df string) ServiceabilityLinkEvent {
			return ServiceabilityLinkEvent{eventType: et, Link: l, diff: df}
		},
		diffFunc,
	)
}

func CompareUser(a, b []serviceability.User) []ServiceabilityUserEvent {
	diffFunc := func(a, b serviceability.User) string {
		return diffEntities(a, b, func(u serviceability.User) string { return base58.Encode(u.PubKey[:]) })
	}
	return Compare(a, b,
		func(u serviceability.User) string { return base58.Encode(u.PubKey[:]) },
		func(et EventType, u serviceability.User, df string) ServiceabilityUserEvent {
			return ServiceabilityUserEvent{eventType: et, User: u, diff: df}
		},
		diffFunc,
	)
}

func diffEntities[T any](a, b T, getCode func(T) string) string {
	diff := ""
	if !reflect.DeepEqual(a, b) {
		oldJSON, _ := json.MarshalIndent(a, "", "  ")
		newJSON, _ := json.MarshalIndent(b, "", "  ")

		// Use the code from whichever entity has one for the diff header.
		code := getCode(a)
		if code == "" {
			code = getCode(b)
		}

		edits := myers.ComputeEdits(span.URIFromPath("old/"+code), string(oldJSON), string(newJSON))
		diff = fmt.Sprint(gotextdiff.ToUnified("old/"+code, "new/"+code, string(oldJSON)+"\n", edits))
	}
	return diff
}
