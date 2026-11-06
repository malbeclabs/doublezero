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

	// ExternalAdded and ExternalRemoved are items (a Cloudflare address, a
	// gnmic map pubkey) the target gained, changed or lost since the previous
	// pass of this process left it: a write the sync did not make.
	ExternalAdded   []string
	ExternalRemoved []string

	list List
	// cfAdd and cfDeleteIDs are the Cloudflare items to add and the ids to
	// delete.
	cfAdd       []ListItem
	cfDeleteIDs []string
	// cfAfter is what the list holds once the write succeeds, by address.
	cfAfter map[string]string
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

// lastSeen remembers what a target held when the previous pass of this
// process left it: what that pass wrote, or what it read when it wrote
// nothing. It is empty at start, so the first pass after a start compares
// with nothing and finds no outside edit, and empty again after a write that
// failed partway, which leaves the target in a state no pass recorded.
type lastSeen struct {
	items map[string]string
}

// observe records what a pass read and returns how it differs from what the
// previous pass left.
func (s *lastSeen) observe(now map[string]string) (added, removed []string) {
	added, removed = diffItems(s.items, now)
	s.items = now
	return added, removed
}

// writing forgets the last state before a write, in case it fails partway.
func (s *lastSeen) writing() { s.items = nil }

// wrote records what a write that succeeded left the target holding.
func (s *lastSeen) wrote(now map[string]string) { s.items = now }

// diffItems names the items that differ between what the list held after the
// previous pass and what it holds now: added holds items that are new or
// whose comment changed, removed holds items that are gone. Nothing differs
// when there was no previous pass to compare with.
func diffItems(prev, now map[string]string) (added, removed []string) {
	if prev == nil {
		return nil, nil
	}
	for k, c := range now {
		if pc, ok := prev[k]; !ok || pc != c {
			added = append(added, k)
		}
	}
	for k := range prev {
		if _, ok := now[k]; !ok {
			removed = append(removed, k)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}
