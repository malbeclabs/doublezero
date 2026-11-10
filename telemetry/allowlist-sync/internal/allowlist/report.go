package allowlist

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Call is the newest GetConfig the controller recorded for one device pubkey
// from one caller address.
type Call struct {
	DevicePubkey string
	CallerIP     netip.Addr
	LastSeen     time.Time
}

// CallerReader reads the addresses devices call the controller from (spec
// section 2, step 1). cut reports a read that kept only the newest calls.
type CallerReader interface {
	RecentCalls(ctx context.Context, since time.Time) (calls []Call, cut bool, err error)
}

// OffchainCaller is a calling address the ledger does not hold for the device
// whose pubkey it called with: an open exception, a device that moved, or a
// caller that is not the device.
type OffchainCaller struct {
	Pubkey   string
	Code     string
	CallerIP netip.Addr
	LastSeen time.Time
	// Listed is true when the address is on the device's own entry, which
	// today means it is the device's inclusion. An unlisted one needs a person.
	Listed bool
}

// Report is the section 2 report for one pass.
type Report struct {
	Offchain []OffchainCaller
	// LastListedCall is, per live device that is not excluded, the newest call
	// from one of its own listed addresses. A device with no such call in the
	// window has the zero time.
	LastListedCall map[string]time.Time
	// OffchainDropped counts offchain callers left out because their device
	// already had MaxOffchainCallersPerDevice newer ones.
	OffchainDropped int
}

// MaxOffchainCallersPerDevice caps the offchain callers reported per device
// pubkey, newest first. Any caller may send a device's pubkey from any address,
// so without it each source address would add a metric series.
const MaxOffchainCallersPerDevice = 8

// BuildReport compares the calls against the list. It covers the listed
// devices, and pubkeys that are not on the ledger at all, whose every caller is
// offchain and keeps an empty code. Excluded devices, and devices on the ledger
// that are not live, are left out.
func BuildReport(b Built, calls []Call) Report {
	r := Report{LastListedCall: make(map[string]time.Time, len(b.List.Devices))}
	for pk := range b.List.Devices {
		r.LastListedCall[pk] = time.Time{}
	}
	for _, c := range calls {
		dev, listed := b.List.Devices[c.DevicePubkey]
		_, onLedger := b.Codes[c.DevicePubkey]
		if b.Excluded[c.DevicePubkey] || (!listed && onLedger) {
			continue
		}
		if listed && slices.Contains(dev.Addrs(), c.CallerIP) {
			if c.LastSeen.After(r.LastListedCall[c.DevicePubkey]) {
				r.LastListedCall[c.DevicePubkey] = c.LastSeen
			}
		}
		if listed && slices.Contains(dev.Onchain, c.CallerIP) {
			continue
		}
		r.Offchain = append(r.Offchain, OffchainCaller{
			Pubkey:   c.DevicePubkey,
			Code:     b.Codes[c.DevicePubkey],
			CallerIP: c.CallerIP,
			LastSeen: c.LastSeen,
			Listed:   listed && slices.Contains(dev.Included, c.CallerIP),
		})
	}
	slices.SortFunc(r.Offchain, func(a, b OffchainCaller) int {
		if a.Pubkey != b.Pubkey {
			return strings.Compare(a.Pubkey, b.Pubkey)
		}
		if c := b.LastSeen.Compare(a.LastSeen); c != 0 {
			return c
		}
		return a.CallerIP.Compare(b.CallerIP)
	})
	kept := r.Offchain[:0]
	perDevice := make(map[string]int)
	for _, c := range r.Offchain {
		if perDevice[c.Pubkey] >= MaxOffchainCallersPerDevice {
			r.OffchainDropped++
			continue
		}
		perDevice[c.Pubkey]++
		kept = append(kept, c)
	}
	r.Offchain = kept
	return r
}
