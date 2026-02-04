package gnmi

import (
	"strings"
	"time"

	gpb "github.com/openconfig/gnmi/proto/gnmi"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// Record is a marker interface for table-specific records.
// Each record type maps to a specific ClickHouse table.
type Record interface {
	// TableName returns the destination table name for this record.
	TableName() string
}

// Metadata contains common fields extracted from gNMI notifications.
type Metadata struct {
	DevicePubkey string
	Timestamp    time.Time
}

// PathMatcher is a function that determines if a gNMI path should be processed.
type PathMatcher func(path *gpb.Path) bool

// ExtractFunc extracts records from an unmarshaled ygot Device.
type ExtractFunc func(device *oc.Device, meta Metadata) []Record

// ExtractorDef defines a single extractor with its path matching and extraction logic.
type ExtractorDef struct {
	Name    string
	Match   PathMatcher
	Extract ExtractFunc
}

// PathContains returns a PathMatcher that matches if the path contains all specified element names.
// All element names must be present for the path to match (logical AND).
//
// Element names are matched against path element names exactly. Leading and trailing
// slashes in the pattern are stripped for convenience, so both "system" and "/system/"
// are equivalent and will match a path element named "system".
//
// Examples:
//
//	PathContains("isis", "adjacencies")
//	  - Matches: /network-instances/network-instance/protocols/protocol/isis/levels/level/adjacencies
//	  - No match: /network-instances/network-instance/protocols/protocol/bgp/neighbors
//
//	PathContains("system", "state")
//	  - Matches: /system/state
//	  - No match: /system/memory
func PathContains(elems ...string) PathMatcher {
	// Pre-process: strip slashes to get clean element names
	cleanElems := make([]string, len(elems))
	for i, elem := range elems {
		cleanElems[i] = strings.Trim(elem, "/")
	}

	return func(path *gpb.Path) bool {
		if path == nil {
			return false
		}

		pathElems := path.GetElem()

		// For each required element, check if any path element matches exactly
		for _, required := range cleanElems {
			found := false
			for _, pathElem := range pathElems {
				if pathElem.GetName() == required {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
}

// PathContainsAny returns a PathMatcher that matches if the path contains any of the specified element names.
// At least one element name must be present for the path to match (logical OR).
//
// Element names are matched against path element names exactly. Leading and trailing
// slashes in the pattern are stripped for convenience, so both "system" and "/system/"
// are equivalent and will match a path element named "system".
//
// Examples:
//
//	PathContainsAny("isis", "bgp")
//	  - Matches: /network-instances/network-instance/protocols/protocol/isis/...
//	  - Matches: /network-instances/network-instance/protocols/protocol/bgp/...
//	  - No match: /system/state
func PathContainsAny(elems ...string) PathMatcher {
	// Pre-process: strip slashes to get clean element names
	cleanElems := make([]string, len(elems))
	for i, elem := range elems {
		cleanElems[i] = strings.Trim(elem, "/")
	}

	return func(path *gpb.Path) bool {
		if path == nil {
			return false
		}

		for _, pathElem := range path.GetElem() {
			name := pathElem.GetName()
			for _, required := range cleanElems {
				if name == required {
					return true
				}
			}
		}
		return false
	}
}
