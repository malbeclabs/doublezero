package serviceability

import (
	"bytes"
	"sort"

	"github.com/gagliardetto/solana-go"
)

// ReconcilePlan describes the delta needed to drive the set of users owned by a
// given key toward a desired count.
type ReconcilePlan struct {
	// ToCreate is the number of users to add. Always >= 0.
	ToCreate int
	// ToDelete lists user PDAs to remove, in the order they should be deleted.
	// Sorted by ClientIp ascending, then by PubKey ascending as a tiebreaker, so
	// repeated calls against the same input produce identical plans.
	ToDelete []solana.PublicKey
}

// PlanReconcile decides what to create or delete so that the number of users
// owned by ownerFilter equals target. Users with a different Owner are ignored
// (neither counted nor deleted), which lets the stress orchestrator share a
// program with other tenants without disturbing them.
//
// The function is pure — no I/O — so it is safe to call repeatedly while the
// orchestrator polls live state. Returns a zero plan when target is negative.
func PlanReconcile(current []User, target int, ownerFilter solana.PublicKey) ReconcilePlan {
	if target < 0 {
		return ReconcilePlan{}
	}

	var owned []User
	for _, u := range current {
		if bytes.Equal(u.Owner[:], ownerFilter[:]) {
			owned = append(owned, u)
		}
	}

	switch {
	case len(owned) < target:
		return ReconcilePlan{ToCreate: target - len(owned)}
	case len(owned) > target:
		sort.Slice(owned, func(i, j int) bool {
			if c := bytes.Compare(owned[i].ClientIp[:], owned[j].ClientIp[:]); c != 0 {
				return c < 0
			}
			return bytes.Compare(owned[i].PubKey[:], owned[j].PubKey[:]) < 0
		})
		victims := owned[target:]
		out := make([]solana.PublicKey, len(victims))
		for i, u := range victims {
			out[i] = solana.PublicKeyFromBytes(u.PubKey[:])
		}
		return ReconcilePlan{ToDelete: out}
	default:
		return ReconcilePlan{}
	}
}
