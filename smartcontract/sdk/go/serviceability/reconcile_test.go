package serviceability_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeUser is a tiny helper to build a User suitable for PlanReconcile testing:
// only Owner, ClientIp, and PubKey actually influence the planner.
func makeUser(owner solana.PublicKey, pubkey solana.PublicKey, clientIP [4]byte) serviceability.User {
	return serviceability.User{
		Owner:    owner,
		ClientIp: clientIP,
		PubKey:   pubkey,
	}
}

func TestPlanReconcile(t *testing.T) {
	t.Parallel()

	orchestrator := solana.NewWallet().PublicKey()
	stranger := solana.NewWallet().PublicKey()

	// Stable pubkeys so we can assert exact ordering.
	u1 := solana.NewWallet().PublicKey()
	u2 := solana.NewWallet().PublicKey()
	u3 := solana.NewWallet().PublicKey()
	u4 := solana.NewWallet().PublicKey()
	u5 := solana.NewWallet().PublicKey()

	ip := func(a, b, c, d byte) [4]byte { return [4]byte{a, b, c, d} }

	tests := []struct {
		name          string
		current       []serviceability.User
		target        int
		owner         solana.PublicKey
		wantCreate    int
		wantDeleteIPs [][4]byte // ClientIp order we expect to see in ToDelete
	}{
		{
			name:       "zero to N",
			current:    nil,
			target:     4,
			owner:      orchestrator,
			wantCreate: 4,
		},
		{
			name: "N to zero deletes in ip-ascending order",
			current: []serviceability.User{
				makeUser(orchestrator, u1, ip(10, 0, 0, 3)),
				makeUser(orchestrator, u2, ip(10, 0, 0, 1)),
				makeUser(orchestrator, u3, ip(10, 0, 0, 4)),
				makeUser(orchestrator, u4, ip(10, 0, 0, 2)),
			},
			target:        0,
			owner:         orchestrator,
			wantCreate:    0,
			wantDeleteIPs: [][4]byte{ip(10, 0, 0, 1), ip(10, 0, 0, 2), ip(10, 0, 0, 3), ip(10, 0, 0, 4)},
		},
		{
			name: "partial trim deletes only the overflow",
			current: []serviceability.User{
				makeUser(orchestrator, u1, ip(10, 0, 0, 5)),
				makeUser(orchestrator, u2, ip(10, 0, 0, 4)),
				makeUser(orchestrator, u3, ip(10, 0, 0, 3)),
				makeUser(orchestrator, u4, ip(10, 0, 0, 2)),
				makeUser(orchestrator, u5, ip(10, 0, 0, 1)),
			},
			target:        3,
			owner:         orchestrator,
			wantCreate:    0,
			wantDeleteIPs: [][4]byte{ip(10, 0, 0, 4), ip(10, 0, 0, 5)},
		},
		{
			name: "partial grow asks for the missing count",
			current: []serviceability.User{
				makeUser(orchestrator, u1, ip(10, 0, 0, 1)),
				makeUser(orchestrator, u2, ip(10, 0, 0, 2)),
			},
			target:     5,
			owner:      orchestrator,
			wantCreate: 3,
		},
		{
			name: "only foreign users present grows by full target",
			current: []serviceability.User{
				makeUser(stranger, u1, ip(10, 0, 0, 1)),
				makeUser(stranger, u2, ip(10, 0, 0, 2)),
				makeUser(stranger, u3, ip(10, 0, 0, 3)),
			},
			target:     2,
			owner:      orchestrator,
			wantCreate: 2,
		},
		{
			name: "mixed ownership only counts and deletes owned",
			current: []serviceability.User{
				makeUser(stranger, u1, ip(10, 0, 0, 9)),
				makeUser(orchestrator, u2, ip(10, 0, 0, 2)),
				makeUser(stranger, u3, ip(10, 0, 0, 8)),
				makeUser(orchestrator, u4, ip(10, 0, 0, 1)),
			},
			target:        1,
			owner:         orchestrator,
			wantCreate:    0,
			wantDeleteIPs: [][4]byte{ip(10, 0, 0, 2)},
		},
		{
			name: "already at target produces zero plan",
			current: []serviceability.User{
				makeUser(orchestrator, u1, ip(10, 0, 0, 1)),
				makeUser(orchestrator, u2, ip(10, 0, 0, 2)),
			},
			target:     2,
			owner:      orchestrator,
			wantCreate: 0,
		},
		{
			name: "negative target produces zero plan",
			current: []serviceability.User{
				makeUser(orchestrator, u1, ip(10, 0, 0, 1)),
			},
			target:     -1,
			owner:      orchestrator,
			wantCreate: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := serviceability.PlanReconcile(tc.current, tc.target, tc.owner)
			assert.Equal(t, tc.wantCreate, plan.ToCreate, "ToCreate")
			require.Len(t, plan.ToDelete, len(tc.wantDeleteIPs), "ToDelete length")

			// Resolve expected pubkeys via ClientIp lookup against the current set.
			ipToPubkey := map[[4]byte]solana.PublicKey{}
			for _, u := range tc.current {
				ipToPubkey[u.ClientIp] = solana.PublicKeyFromBytes(u.PubKey[:])
			}
			for i, ipKey := range tc.wantDeleteIPs {
				assert.Equal(t, ipToPubkey[ipKey], plan.ToDelete[i], "ToDelete[%d] (clientIp=%v)", i, ipKey)
			}
		})
	}
}

func TestPlanReconcile_TieBreaksByPubkey(t *testing.T) {
	t.Parallel()

	orchestrator := solana.NewWallet().PublicKey()
	sharedIP := [4]byte{10, 0, 0, 1}

	// Two users with the same ClientIp (artificial — onchain the IP is part of
	// the PDA seed so collisions can't happen, but the tiebreak must still be
	// deterministic).
	pkA := solana.PublicKeyFromBytes([]byte{0xAA, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	pkB := solana.PublicKeyFromBytes([]byte{0xBB, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	plan := serviceability.PlanReconcile([]serviceability.User{
		makeUser(orchestrator, pkB, sharedIP),
		makeUser(orchestrator, pkA, sharedIP),
	}, 0, orchestrator)

	require.Len(t, plan.ToDelete, 2)
	// pkA (0xAA…) sorts before pkB (0xBB…).
	assert.Equal(t, pkA, plan.ToDelete[0])
	assert.Equal(t, pkB, plan.ToDelete[1])
}
