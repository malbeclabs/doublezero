package api

import (
	"net"
	"reflect"
	"sort"
	"testing"
)

func TestProvisionRequest_InfraEqual(t *testing.T) {
	base := &ProvisionRequest{
		UserType:  UserTypeMulticast,
		TunnelSrc: net.IPv4(1, 1, 1, 1),
		TunnelDst: net.IPv4(2, 2, 2, 2),
		TunnelNet: &net.IPNet{
			IP:   net.IPv4(169, 254, 0, 0),
			Mask: net.CIDRMask(31, 32),
		},
		DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
		DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(24, 32)}},
		BgpLocalAsn:        65000,
		BgpRemoteAsn:       65001,
		MulticastPubGroups: []net.IP{net.IPv4(239, 0, 0, 1)},
		MulticastSubGroups: []net.IP{net.IPv4(239, 0, 0, 2)},
	}

	t.Run("true when only groups differ", func(t *testing.T) {
		other := *base
		other.MulticastPubGroups = []net.IP{net.IPv4(239, 0, 0, 1), net.IPv4(239, 0, 0, 3)}
		other.MulticastSubGroups = nil
		if !base.InfraEqual(&other) {
			t.Fatal("expected InfraEqual to return true when only groups differ")
		}
	})

	t.Run("true when identical", func(t *testing.T) {
		other := *base
		if !base.InfraEqual(&other) {
			t.Fatal("expected InfraEqual to return true for identical requests")
		}
	})

	t.Run("false when TunnelDst differs", func(t *testing.T) {
		other := *base
		other.TunnelDst = net.IPv4(3, 3, 3, 3)
		if base.InfraEqual(&other) {
			t.Fatal("expected InfraEqual to return false when TunnelDst differs")
		}
	})

	t.Run("false when BgpLocalAsn differs", func(t *testing.T) {
		other := *base
		other.BgpLocalAsn = 65999
		if base.InfraEqual(&other) {
			t.Fatal("expected InfraEqual to return false when BgpLocalAsn differs")
		}
	})

	t.Run("false when UserType differs", func(t *testing.T) {
		other := *base
		other.UserType = UserTypeIBRL
		if base.InfraEqual(&other) {
			t.Fatal("expected InfraEqual to return false when UserType differs")
		}
	})

	t.Run("nil handling", func(t *testing.T) {
		var nilPR *ProvisionRequest
		if nilPR.InfraEqual(base) {
			t.Fatal("expected false when receiver is nil")
		}
		if base.InfraEqual(nil) {
			t.Fatal("expected false when arg is nil")
		}
		if !nilPR.InfraEqual(nil) {
			t.Fatal("expected true when both are nil")
		}
	})
}

// TestProvisionRequest_InfraEqual_CoversAllFields uses reflection to ensure
// every field in ProvisionRequest is explicitly handled by InfraEqual — either
// as an infra field (changing it makes InfraEqual return false) or as a
// group field (changing it still returns true). If someone adds a new field to
// ProvisionRequest without updating this test, it will fail.
func TestProvisionRequest_InfraEqual_CoversAllFields(t *testing.T) {
	// Fields that InfraEqual intentionally ignores (group fields).
	groupFields := map[string]bool{
		"MulticastPubGroups": true,
		"MulticastSubGroups": true,
	}

	// All remaining fields must be infra fields — changing them must make
	// InfraEqual return false.
	typ := reflect.TypeOf(ProvisionRequest{})
	for i := range typ.NumField() {
		field := typ.Field(i)
		if groupFields[field.Name] {
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			a := testFullProvisionRequest()
			b := testFullProvisionRequest()

			// Mutate field on b to a different value.
			va := reflect.ValueOf(&a).Elem().Field(i)
			vb := reflect.ValueOf(&b).Elem().Field(i)
			mutateField(t, va, vb)

			if a.InfraEqual(&b) {
				t.Fatalf("InfraEqual returned true after mutating infra field %s — field is not compared", field.Name)
			}
		})
	}

	// Verify group fields are actually ignored.
	for name := range groupFields {
		t.Run(name+"_ignored", func(t *testing.T) {
			a := testFullProvisionRequest()
			b := testFullProvisionRequest()

			idx := fieldIndex(typ, name)
			if idx < 0 {
				t.Fatalf("group field %s not found on ProvisionRequest", name)
			}
			vb := reflect.ValueOf(&b).Elem().Field(idx)
			// Set to a different slice.
			vb.Set(reflect.ValueOf([]net.IP{net.IPv4(240, 0, 0, 99)}))

			if !a.InfraEqual(&b) {
				t.Fatalf("InfraEqual returned false when only group field %s changed", name)
			}
		})
	}

	// Guard: ensure every struct field is accounted for.
	allFields := make(map[string]bool)
	for i := range typ.NumField() {
		allFields[typ.Field(i).Name] = true
	}
	// infraFields = allFields - groupFields; verify no field is missing.
	for name := range groupFields {
		if !allFields[name] {
			t.Fatalf("groupFields references non-existent field %s", name)
		}
	}
}

// testFullProvisionRequest returns a ProvisionRequest with all fields
// populated to non-zero values.
func testFullProvisionRequest() ProvisionRequest {
	return ProvisionRequest{
		UserType:           UserTypeMulticast,
		TunnelSrc:          net.IPv4(1, 1, 1, 1),
		TunnelDst:          net.IPv4(2, 2, 2, 2),
		TunnelNet:          &net.IPNet{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(31, 32)},
		DoubleZeroIP:       net.IPv4(10, 0, 0, 1),
		DoubleZeroPrefixes: []*net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(24, 32)}},
		BgpLocalAsn:        65000,
		BgpRemoteAsn:       65001,
		MulticastPubGroups: []net.IP{net.IPv4(239, 0, 0, 1)},
		MulticastSubGroups: []net.IP{net.IPv4(239, 0, 0, 2)},
	}
}

// mutateField changes vb to a value different from va for common field types
// found in ProvisionRequest.
func mutateField(t *testing.T, va, vb reflect.Value) {
	t.Helper()
	typ := va.Type()
	switch {
	// net.IP is []byte — match by type name before generic slice handling.
	case typ == reflect.TypeOf(net.IP{}):
		vb.Set(reflect.ValueOf(net.IPv4(250, 250, 250, 250)))
	case typ.Kind() == reflect.Uint32:
		vb.SetUint(va.Uint() + 1)
	case typ.Kind() == reflect.Slice && typ.Elem() == reflect.TypeOf(net.IP{}):
		vb.Set(reflect.ValueOf([]net.IP{net.IPv4(250, 250, 250, 250)}))
	case typ.Kind() == reflect.Slice && typ.Elem() == reflect.TypeOf((*net.IPNet)(nil)):
		vb.Set(reflect.ValueOf([]*net.IPNet{{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)}}))
	case typ == reflect.TypeOf((*net.IPNet)(nil)):
		vb.Set(reflect.ValueOf(&net.IPNet{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)}))
	case typ.ConvertibleTo(reflect.TypeOf(int(0))):
		// Covers UserType (type UserType int).
		vb.Set(reflect.ValueOf(UserTypeIBRL).Convert(typ))
	default:
		t.Fatalf("unsupported field type %v for mutation — update mutateField", typ)
	}
}

func fieldIndex(typ reflect.Type, name string) int {
	for i := range typ.NumField() {
		if typ.Field(i).Name == name {
			return i
		}
	}
	return -1
}

func TestIPSetDiff(t *testing.T) {
	tests := []struct {
		name        string
		oldIPs      []net.IP
		newIPs      []net.IP
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:        "add new IP",
			oldIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			newIPs:      []net.IP{net.IPv4(239, 0, 0, 1), net.IPv4(239, 0, 0, 2)},
			wantAdded:   []string{"239.0.0.2"},
			wantRemoved: nil,
		},
		{
			name:        "remove IP",
			oldIPs:      []net.IP{net.IPv4(239, 0, 0, 1), net.IPv4(239, 0, 0, 2)},
			newIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			wantAdded:   nil,
			wantRemoved: []string{"239.0.0.2"},
		},
		{
			name:        "add and remove",
			oldIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			newIPs:      []net.IP{net.IPv4(239, 0, 0, 2)},
			wantAdded:   []string{"239.0.0.2"},
			wantRemoved: []string{"239.0.0.1"},
		},
		{
			name:        "no change",
			oldIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			newIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "both empty",
			oldIPs:      nil,
			newIPs:      nil,
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "from empty to some",
			oldIPs:      nil,
			newIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			wantAdded:   []string{"239.0.0.1"},
			wantRemoved: nil,
		},
		{
			name:        "from some to empty",
			oldIPs:      []net.IP{net.IPv4(239, 0, 0, 1)},
			newIPs:      nil,
			wantAdded:   nil,
			wantRemoved: []string{"239.0.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := IPSetDiff(tt.oldIPs, tt.newIPs)
			gotAdded := ipStrings(added)
			gotRemoved := ipStrings(removed)
			sort.Strings(gotAdded)
			sort.Strings(gotRemoved)
			sort.Strings(tt.wantAdded)
			sort.Strings(tt.wantRemoved)

			if !strSlicesEqual(gotAdded, tt.wantAdded) {
				t.Errorf("added: got %v, want %v", gotAdded, tt.wantAdded)
			}
			if !strSlicesEqual(gotRemoved, tt.wantRemoved) {
				t.Errorf("removed: got %v, want %v", gotRemoved, tt.wantRemoved)
			}
		})
	}
}

func ipStrings(ips []net.IP) []string {
	if len(ips) == 0 {
		return nil
	}
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}

func strSlicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
