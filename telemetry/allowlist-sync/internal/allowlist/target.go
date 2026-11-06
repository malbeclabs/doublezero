package allowlist

import (
	"context"
	"net/netip"
	"slices"
)

// Target is one place the list is applied: gnmic's map, the Cloudflare IP
// list, and later the controller security group and flow-in's source ranges
// (spec section 7), each as one more Target.
type Target interface {
	Name() string
	// Plan reads what the target holds now and compares it with the list. It
	// writes nothing.
	Plan(ctx context.Context, list List) (Plan, error)
	// Apply makes the target hold the list. It is called only with a Plan this
	// target returned for that list in the same pass.
	Apply(ctx context.Context, plan Plan) error
}

// Plan is what a pass would change in one target.
type Plan struct {
	Target  string
	Changed bool

	// PrevDevices and RemovedDevices count devices, for a target that records
	// them. TracksDevices is false for one that holds only addresses.
	TracksDevices  bool
	PrevDevices    int
	RemovedDevices []string

	PrevAddrs    int
	AddedAddrs   []netip.Addr
	RemovedAddrs []netip.Addr
	// RemovedOther holds entries that are not single IPv4 addresses, which
	// the sync never writes, so a write removes them.
	RemovedOther []string

	list List
}

// Removed counts the entries a write would remove.
func (p Plan) Removed() int { return len(p.RemovedAddrs) + len(p.RemovedOther) }

func diffAddrs(prev, next []netip.Addr) (added, removed []netip.Addr) {
	for _, a := range next {
		if !slices.Contains(prev, a) {
			added = append(added, a)
		}
	}
	for _, a := range prev {
		if !slices.Contains(next, a) {
			removed = append(removed, a)
		}
	}
	return added, removed
}
