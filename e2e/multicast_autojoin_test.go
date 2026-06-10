//go:build e2e

package e2e_test

import (
	"net"
	"testing"
	"time"

	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// TestE2E_Multicast_AutoJoinFromAccessPass verifies that `doublezero connect
// multicast` invoked with NO group arguments auto-joins every group authorized
// in the caller's AccessPass: it publishes to the groups in mgroup_pub_allowlist
// and subscribes to the groups in mgroup_sub_allowlist.
//
// The test authorizes the client for publishing on one group and subscribing on
// a different group, then runs the bare connect command and asserts the
// resulting onchain Multicast user holds exactly those roles.
func TestE2E_Multicast_AutoJoinFromAccessPass(t *testing.T) {
	t.Parallel()

	// Single device + single client. Start() also sets a prepaid access pass for
	// the client at (client.CYOANetworkIP, client.Pubkey).
	dn, _, client := NewSingleDeviceSingleClientTestDevnet(t)

	const pubGroup = "autojoin-pub"
	const subGroup = "autojoin-sub"

	// Create two groups and authorize the client for publishing on one and
	// subscribing on the other (distinct roles, distinct groups). The allowlist
	// adds target the same access pass PDA created by Start().
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -e
		doublezero multicast group create --code ` + pubGroup + ` --max-bandwidth 10Gbps --owner me -w
		doublezero multicast group create --code ` + subGroup + ` --max-bandwidth 10Gbps --owner me -w
		doublezero multicast group allowlist publisher add --code ` + pubGroup + ` --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
		doublezero multicast group allowlist subscriber add --code ` + subGroup + ` --user-payer ` + client.Pubkey + ` --client-ip ` + client.CYOANetworkIP + `
	`})
	require.NoError(t, err)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	// Resolve the onchain pubkeys for the two groups by code.
	groupPubKeyByCode := func(t *testing.T, code string) [32]byte {
		t.Helper()
		data, err := serviceabilityClient.GetProgramData(t.Context())
		require.NoError(t, err)
		for _, g := range data.MulticastGroups {
			if g.Code == code {
				return g.PubKey
			}
		}
		t.Fatalf("multicast group %q not found in program data", code)
		return [32]byte{}
	}
	pubGroupPK := groupPubKeyByCode(t, pubGroup)
	subGroupPK := groupPubKeyByCode(t, subGroup)

	clientIPBytes := func(ip string) [4]uint8 {
		parsed := net.ParseIP(ip).To4()
		require.NotNil(t, parsed, "could not parse client IP: %s", ip)
		return [4]uint8{parsed[0], parsed[1], parsed[2], parsed[3]}
	}
	wantClientIP := clientIPBytes(client.CYOANetworkIP)

	containsPubKey := func(list [][32]uint8, want [32]byte) bool {
		for _, pk := range list {
			if pk == want {
				return true
			}
		}
		return false
	}

	// Run the bare connect command — no publisher/subscriber, no group codes.
	// The CLI must look up the access pass and auto-join the authorized groups.
	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast"})
	require.NoError(t, err, "bare `doublezero connect multicast` failed")

	err = client.WaitForTunnelUp(t.Context(), 90*time.Second)
	require.NoError(t, err)

	// The resulting Multicast user must publish to pubGroup and subscribe to
	// subGroup, and to nothing else.
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		for _, u := range data.Users {
			if u.UserType != serviceability.UserTypeMulticast || u.ClientIp != wantClientIP {
				continue
			}
			if u.Status != serviceability.UserStatusActivated {
				return false
			}
			return len(u.Publishers) == 1 && containsPubKey(u.Publishers, pubGroupPK) &&
				len(u.Subscribers) == 1 && containsPubKey(u.Subscribers, subGroupPK)
		}
		return false
	}, 90*time.Second, 2*time.Second,
		"multicast user did not auto-join the access-pass-authorized publish and subscribe groups")
}
