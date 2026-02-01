package serviceability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestEnumStrings(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	fixturePath := filepath.Join(filepath.Dir(filename), "..", "testdata", "enum_strings.json")

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading enum_strings.json: %v", err)
	}

	var fixture map[string]map[string]string
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parsing enum_strings.json: %v", err)
	}

	// Map enum type names to a function that converts int -> String() output.
	// AccessPassTypeTag is skipped because it has no String() method in Go.
	stringers := map[string]func(int) string{
		"LocationStatus":       func(v int) string { return LocationStatus(v).String() },
		"ExchangeStatus":       func(v int) string { return ExchangeStatus(v).String() },
		"DeviceDeviceType":     func(v int) string { return DeviceDeviceType(v).String() },
		"DeviceStatus":         func(v int) string { return DeviceStatus(v).String() },
		"DeviceHealth":         func(v int) string { return DeviceHealth(v).String() },
		"DeviceDesiredStatus":  func(v int) string { return DeviceDesiredStatus(v).String() },
		"InterfaceStatus":      func(v int) string { return InterfaceStatus(v).String() },
		"InterfaceType":        func(v int) string { return InterfaceType(v).String() },
		"LoopbackType":         func(v int) string { return LoopbackType(v).String() },
		"LinkLinkType":         func(v int) string { return LinkLinkType(v).String() },
		"LinkStatus":           func(v int) string { return LinkStatus(v).String() },
		"LinkHealth":           func(v int) string { return LinkHealth(v).String() },
		"LinkDesiredStatus":    func(v int) string { return LinkDesiredStatus(v).String() },
		"ContributorStatus":    func(v int) string { return ContributorStatus(v).String() },
		"UserUserType":         func(v int) string { return UserUserType(v).String() },
		"CyoaType":             func(v int) string { return CyoaType(v).String() },
		"UserStatus":           func(v int) string { return UserStatus(v).String() },
		"MulticastGroupStatus": func(v int) string { return MulticastGroupStatus(v).String() },
		"AccessPassStatus":     func(v int) string { return AccessPassStatus(v).String() },
	}

	for enumName, cases := range fixture {
		// AccessPassTypeTag has no String() method in Go; skip it.
		if enumName == "AccessPassTypeTag" {
			continue
		}

		stringer, ok := stringers[enumName]
		if !ok {
			t.Errorf("no stringer registered for enum type %s", enumName)
			continue
		}

		for valStr, expected := range cases {
			val, err := strconv.Atoi(valStr)
			if err != nil {
				t.Fatalf("%s: invalid key %q: %v", enumName, valStr, err)
			}

			t.Run(fmt.Sprintf("%s/%d", enumName, val), func(t *testing.T) {
				got := stringer(val)
				if got != expected {
					t.Errorf("%s(%d).String() = %q, want %q", enumName, val, got, expected)
				}
			})
		}
	}
}
