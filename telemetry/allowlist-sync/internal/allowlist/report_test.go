package allowlist

import (
	"net/netip"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReport(t *testing.T) {
	t0 := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	devices := []serviceability.Device{
		dev(1, "onchain", serviceability.DeviceStatusActivated, "45.1.1.1", "45.1.1.9/31"),
		dev(2, "mgmt-vrf", serviceability.DeviceStatusActivated, "45.2.2.2"),
		dev(3, "silent", serviceability.DeviceStatusDrained, "45.3.3.3"),
		dev(4, "excluded", serviceability.DeviceStatusActivated, "45.4.4.4"),
		dev(5, "deleting", serviceability.DeviceStatusDeleting, "45.5.5.5"),
	}
	exc := Exceptions{
		Inclusions: []Inclusion{{Pubkey: pk(2), Code: "mgmt-vrf", Address: netip.MustParseAddr("216.155.20.62"), Evidence: "x"}},
		Exclusions: []Exclusion{{Pubkey: pk(4), Code: "excluded", Reason: "x"}},
	}
	b := mustBuild(t, devices, exc)

	calls := []Call{
		{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("45.1.1.9"), LastSeen: t0},
		{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("45.1.1.1"), LastSeen: t0.Add(-time.Hour)},
		{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("8.8.8.8"), LastSeen: t0.Add(time.Minute)}, // not the device
		{DevicePubkey: pk(2), CallerIP: netip.MustParseAddr("216.155.20.62"), LastSeen: t0},            // open exception
		{DevicePubkey: pk(4), CallerIP: netip.MustParseAddr("9.9.9.9"), LastSeen: t0},                  // excluded
		{DevicePubkey: pk(5), CallerIP: netip.MustParseAddr("9.9.9.5"), LastSeen: t0},                  // not live
		{DevicePubkey: pk(9), CallerIP: netip.MustParseAddr("9.9.9.6"), LastSeen: t0},                  // not on the ledger
	}
	r := BuildReport(b, calls)

	assert.Equal(t, []OffchainCaller{
		{Pubkey: pk(1), Code: "onchain", CallerIP: netip.MustParseAddr("8.8.8.8"), LastSeen: t0.Add(time.Minute), Listed: false},
		{Pubkey: pk(2), Code: "mgmt-vrf", CallerIP: netip.MustParseAddr("216.155.20.62"), LastSeen: t0, Listed: true},
		{Pubkey: pk(9), Code: "", CallerIP: netip.MustParseAddr("9.9.9.6"), LastSeen: t0, Listed: false},
	}, r.Offchain)
	assert.Equal(t, map[string]time.Time{
		pk(1): t0, // the newest call from its own addresses, not the 8.8.8.8 one
		pk(2): t0,
		pk(3): {}, // live, never called
	}, r.LastListedCall)
}

// A device calling from another device's address is not calling from its own.
func TestBuildReport_AnotherDevicesAddressIsNotListed(t *testing.T) {
	t0 := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	b := mustBuild(t, []serviceability.Device{
		dev(1, "a", serviceability.DeviceStatusActivated, "45.1.1.1"),
		dev(2, "b", serviceability.DeviceStatusActivated, "45.2.2.2"),
	}, Exceptions{})
	r := BuildReport(b, []Call{{DevicePubkey: pk(1), CallerIP: netip.MustParseAddr("45.2.2.2"), LastSeen: t0}})
	assert.Equal(t, []OffchainCaller{{Pubkey: pk(1), Code: "a", CallerIP: netip.MustParseAddr("45.2.2.2"), LastSeen: t0}}, r.Offchain)
	assert.Equal(t, map[string]time.Time{pk(1): {}, pk(2): {}}, r.LastListedCall)
}

// Any caller may send a device's pubkey from any address, so the report keeps
// the newest MaxOffchainCallersPerDevice per device and counts the rest.
func TestBuildReport_CapsOffchainCallersPerDevice(t *testing.T) {
	b := Build([]serviceability.Device{dev(1, "dz1", serviceability.DeviceStatusActivated, "45.0.0.1")}, Exceptions{})
	var calls []Call
	for i := 0; i < MaxOffchainCallersPerDevice+2; i++ {
		calls = append(calls, Call{DevicePubkey: pk(1), CallerIP: netip.AddrFrom4([4]byte{198, 18, 0, byte(i + 1)}), LastSeen: t0.Add(time.Duration(i) * time.Minute)})
	}
	r := BuildReport(b, calls)
	require.Len(t, r.Offchain, MaxOffchainCallersPerDevice)
	assert.Equal(t, 2, r.OffchainDropped)
	assert.Equal(t, netip.AddrFrom4([4]byte{198, 18, 0, byte(MaxOffchainCallersPerDevice + 2)}), r.Offchain[0].CallerIP, "newest first")
	m := NewMetrics(prometheus.NewRegistry())
	m.setReport(b, r)
	assert.Equal(t, float64(2), testutil.ToFloat64(m.OffchainCallersDropped))
	assert.Equal(t, MaxOffchainCallersPerDevice, testutil.CollectAndCount(m.OffchainCaller))
	for _, c := range r.Offchain {
		assert.NotEqual(t, netip.MustParseAddr("198.18.0.1"), c.CallerIP, "the oldest are dropped")
		assert.NotEqual(t, netip.MustParseAddr("198.18.0.2"), c.CallerIP, "the oldest are dropped")
	}
}
