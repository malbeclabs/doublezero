package allowlist

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuild_StatusDecidesLiveness(t *testing.T) {
	statuses := []struct {
		status serviceability.DeviceStatus
		live   bool
	}{
		{serviceability.DeviceStatusActivated, true},
		{serviceability.DeviceStatusDeviceProvisioning, true},
		{serviceability.DeviceStatusLinkProvisioning, true},
		{serviceability.DeviceStatusDrained, true},
		{serviceability.DeviceStatusDeleting, false},
		{serviceability.DeviceStatusPendingDeprecated, false},
		{serviceability.DeviceStatusRejectedDeprecated, false},
		{serviceability.DeviceStatus(99), false},
	}
	for _, tc := range statuses {
		t.Run(tc.status.String(), func(t *testing.T) {
			b := mustBuild(t, []serviceability.Device{dev(1, "dz1", tc.status, "8.8.8.8")}, Exceptions{})
			_, listed := b.List.Devices[pk(1)]
			assert.Equal(t, tc.live, listed)
			assert.Equal(t, map[string]string{pk(1): "dz1"}, b.Codes)
		})
	}
}

func TestBuild_EveryAddressSource(t *testing.T) {
	d := dev(1, "dz1", serviceability.DeviceStatusActivated, "45.1.1.1",
		"45.1.1.9/31",    // DIA interface: only the address is taken
		"45.2.2.2/32",    // loopback
		"45.1.1.1/29",    // duplicate of public_ip
		"10.0.0.1/31",    // private: skipped
		"169.254.0.1/31", // link-local: skipped
		"0.0.0.0/0",      // unset: skipped
		"100.64.0.1/31",  // CGNAT: skipped
	)
	b := mustBuild(t, []serviceability.Device{d}, Exceptions{})
	assert.Equal(t, Device{Code: "dz1", Onchain: addrs("45.1.1.1", "45.1.1.9", "45.2.2.2")}, b.List.Devices[pk(1)])
}

func TestBuild_PrivatePublicIPIsSkipped(t *testing.T) {
	d := dev(1, "dz1", serviceability.DeviceStatusActivated, "172.16.0.1", "45.2.2.2/32")
	b := mustBuild(t, []serviceability.Device{d}, Exceptions{})
	assert.Equal(t, addrs("45.2.2.2"), b.List.Devices[pk(1)].Addrs())
}

func TestBuild_AgentIPIsListed(t *testing.T) {
	set := dev(1, "dz1", serviceability.DeviceStatusActivated, "45.1.1.1")
	set.AgentIp = [4]uint8{37, 61, 209, 41}
	unset := dev(2, "dz2", serviceability.DeviceStatusActivated, "45.1.1.2")
	b := mustBuild(t, []serviceability.Device{set, unset}, Exceptions{})
	assert.Equal(t, addrs("37.61.209.41", "45.1.1.1"), b.List.Devices[pk(1)].Addrs())
	assert.Equal(t, addrs("45.1.1.2"), b.List.Devices[pk(2)].Addrs(), "0.0.0.0 is unset and never listed")
}

func TestBuild_InclusionAddsAddress(t *testing.T) {
	devices := []serviceability.Device{dev(1, "dz-fr5-sw01", serviceability.DeviceStatusActivated, "89.222.118.225")}
	exc := Exceptions{Inclusions: []Inclusion{{Pubkey: pk(1), Code: "dz-fr5-sw01", Address: netip.MustParseAddr("216.155.20.62"), Evidence: "capture"}}}
	b := mustBuild(t, devices, exc)
	assert.Equal(t, Device{Code: "dz-fr5-sw01", Onchain: addrs("89.222.118.225"), Included: addrs("216.155.20.62")}, b.List.Devices[pk(1)])
	assert.Equal(t, addrs("89.222.118.225", "216.155.20.62"), b.List.Devices[pk(1)].Addrs())
	assert.Empty(t, b.Review)
}

func TestBuild_UnusedInclusions(t *testing.T) {
	devices := []serviceability.Device{
		dev(1, "onchain", serviceability.DeviceStatusActivated, "45.1.1.1"),
		dev(2, "deleting", serviceability.DeviceStatusDeleting, "45.2.2.2"),
	}
	exc := Exceptions{Inclusions: []Inclusion{
		{Pubkey: pk(1), Code: "onchain", Address: netip.MustParseAddr("45.1.1.1"), Evidence: "x"},
		{Pubkey: pk(2), Code: "deleting", Address: netip.MustParseAddr("45.9.9.2"), Evidence: "x"},
		{Pubkey: pk(3), Code: "gone", Address: netip.MustParseAddr("45.9.9.3"), Evidence: "x"},
	}}
	b := mustBuild(t, devices, exc)
	assert.Equal(t, []ExceptionReview{
		{Kind: "inclusion", Pubkey: pk(1), Code: "onchain", Address: "45.1.1.1", Reason: "address_onchain"},
		{Kind: "inclusion", Pubkey: pk(2), Code: "deleting", Address: "45.9.9.2", Reason: "device_not_live"},
		{Kind: "inclusion", Pubkey: pk(3), Code: "gone", Address: "45.9.9.3", Reason: "device_not_on_ledger"},
	}, b.Review)
	assert.Equal(t, addrs("45.1.1.1"), b.List.Addrs(), "unused inclusions add nothing")
}

func TestBuild_ExclusionKeepsDeviceOff(t *testing.T) {
	devices := []serviceability.Device{
		dev(1, "keep", serviceability.DeviceStatusActivated, "45.1.1.1"),
		dev(2, "fra-test-tve1", serviceability.DeviceStatusActivated, "178.241.19.43", "178.241.19.44/32"),
		dev(3, "deleting", serviceability.DeviceStatusDeleting, "45.3.3.3"),
	}
	exc := Exceptions{Exclusions: []Exclusion{
		{Pubkey: pk(2), Code: "fra-test-tve1", Reason: "fake device"},
		{Pubkey: pk(3), Code: "deleting", Reason: "x"},
		{Pubkey: pk(4), Code: "gone", Reason: "x"},
	}}
	b := mustBuild(t, devices, exc)
	assert.Equal(t, []string{pk(1)}, keys(b.List.Devices))
	assert.Equal(t, addrs("45.1.1.1"), b.List.Addrs())
	assert.Equal(t, map[string]bool{pk(2): true, pk(3): true, pk(4): true}, b.Excluded)
	assert.Equal(t, []ExceptionReview{
		{Kind: "exclusion", Pubkey: pk(3), Code: "deleting", Reason: "device_not_live"},
		{Kind: "exclusion", Pubkey: pk(4), Code: "gone", Reason: "device_not_on_ledger"},
	}, b.Review)
}

// A contributor can rename its own device. The entry still applies by
// pubkey, and the mismatch is reported, so a rename cannot stop the sync.
func TestBuild_CodeMismatchStillApplies(t *testing.T) {
	devices := []serviceability.Device{
		dev(1, "renamed1", serviceability.DeviceStatusActivated, "45.1.1.1"),
		dev(2, "renamed2", serviceability.DeviceStatusActivated, "45.2.2.2"),
	}
	b := mustBuild(t, devices, Exceptions{
		Inclusions: []Inclusion{{Pubkey: pk(1), Code: "dz1", Address: netip.MustParseAddr("45.9.9.9"), Evidence: "x"}},
		Exclusions: []Exclusion{{Pubkey: pk(2), Code: "dz2", Reason: "x"}},
	})
	assert.Equal(t, addrs("45.1.1.1", "45.9.9.9"), b.List.Addrs())
	assert.Equal(t, []ExceptionReview{
		{Kind: "exclusion", Pubkey: pk(2), Code: "dz2", Reason: "code_mismatch"},
		{Kind: "inclusion", Pubkey: pk(1), Code: "dz1", Address: "45.9.9.9", Reason: "code_mismatch"},
	}, b.Review)
}

func TestCheckComplete(t *testing.T) {
	ok := &serviceability.ProgramData{GlobalState: &serviceability.GlobalState{}, Devices: []serviceability.Device{dev(1, "dz1", 1, "45.1.1.1")}}
	require.NoError(t, checkComplete(ok))

	bad := dev(2, "dz2", 1, "45.2.2.2")
	bad.DeserializeError = errors.New("trailing interfaces length mismatch")
	for name, pd := range map[string]*serviceability.ProgramData{
		"nil":               nil,
		"no global state":   {Devices: ok.Devices},
		"no devices":        {GlobalState: ok.GlobalState},
		"malformed account": {GlobalState: ok.GlobalState, Devices: append(ok.Devices, bad)},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, checkComplete(pd), ErrIncompleteLedger)
		})
	}
}

func TestList_GnmicMapAndDigest(t *testing.T) {
	l := List{Devices: map[string]Device{
		pk(1): {Code: "a", Onchain: addrs("45.1.1.2", "45.1.1.10"), Included: addrs("45.1.1.1")},
		pk(2): {Code: "b", Onchain: addrs("45.2.2.2")},
	}}
	assert.Equal(t, GnmicAddressMap{
		pk(1): {"45.1.1.1", "45.1.1.2", "45.1.1.10"},
		pk(2): {"45.2.2.2"},
	}, l.GnmicMap())

	same := List{Devices: map[string]Device{
		pk(2): {Code: "b", Onchain: addrs("45.2.2.2")},
		pk(1): {Code: "a", Onchain: addrs("45.1.1.1", "45.1.1.2", "45.1.1.10")},
	}}
	assert.Equal(t, l.Digest(), same.Digest(), "the digest covers addresses per device, not where they came from")
	other := List{Devices: map[string]Device{pk(1): l.Devices[pk(1)]}}
	assert.NotEqual(t, l.Digest(), other.Digest())
}

func TestIsGlobalIPv4(t *testing.T) {
	for s, want := range map[string]bool{
		"8.8.8.8": true, "137.239.195.54": true,
		"0.0.0.0": false, "0.1.2.3": false, "10.1.1.1": false, "172.16.1.1": false, "192.168.1.1": false,
		"127.0.0.1": false, "169.254.1.1": false, "100.64.0.1": false, "100.127.255.255": false,
		"100.128.0.1": true, "192.0.0.1": false, "192.0.2.1": false, "198.51.100.1": false,
		"203.0.113.1": false, "224.0.0.1": false, "240.0.0.1": false, "255.255.255.255": false,
		"2001:db8::1": false, "::ffff:8.8.8.8": false,
	} {
		assert.Equal(t, want, isGlobalIPv4(netip.MustParseAddr(s)), s)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
