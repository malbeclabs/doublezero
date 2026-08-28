//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/stretchr/testify/require"
)

// RFC-27 enforcement: the `require-ip-ownership-proof` feature flag set.
//
// The sibling file covers the flag-clear outcomes, where a missing proof is tolerated. Here the
// flag is on, which changes exactly one thing in the program: whether a *missing* proof is an
// error (`ip_proof.rs`, the `None` arm). A supplied proof is validated in full either way.
//
// What is worth testing at this level is narrow, and deliberately so. The serviceability program's
// own `user_ip_proof_test.rs` already covers every rejection condition against a program-test
// runtime, and the Rust SDK pre-flights version, payer, client_ip, user_type and the signature
// before it builds a transaction — so those rejections can never reach the chain through the real
// CLI. Of the program's proof errors only `IpOwnershipProofRequired` (105) and
// `IpProofEpochOutOfWindow` (110) are reachable end to end, and 110 needs a ledger epoch that a
// devnet never advances past 0. So 105 is the one onchain rejection these tests can assert, and
// the rest of the value here is in the integration: a real verifier, a real ledger, a real tunnel.
//
// Everything that can share a devnet does. A devnet is by far the expensive part of an e2e test —
// a ledger, a manager, a controller and a cEOS device — while an extra client is one small
// container, and whether a client has a verifier to reach is fixed at container start. So the
// outcomes below are subtests over one devnet with three clients, and only the client-IP mismatch,
// which needs a client whose daemon is pointed at an address it does not own, takes a second.

// The enforced outcomes that can share a devnet: a proof accepted, a proof missing, and the
// sentinel exemption. Subtests run in order against distinct clients, so each one's onchain state
// is its own and a failure earlier does not invalidate what follows.
func TestE2E_IPOwnershipProof_Enforced(t *testing.T) {
	t.Parallel()

	// Client with a verifier, for the ordinary path: an access pass naming its address.
	dn, device, specific, log := setupIPProofDevnet(t, devnet.IPVerifierSpec{}, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})

	log.Info("==> Enabling require-ip-ownership-proof")
	require.NoError(t, dn.SetIPOwnershipProofFeatureFlag(t.Context(), true))

	// Client with a verifier, for the wildcard pass.
	wildcard, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 101,
	})
	require.NoError(t, err)

	// Client with no verifier to reach, so `connect` attaches no proof at all.
	unverified, err := dn.AddClient(t.Context(), devnet.ClientSpec{
		CYOANetworkIPHostID: 102,
		NoIPVerifier:        true,
	})
	require.NoError(t, err)

	// A client picks its device from its own latency measurements; connecting before they exist
	// fails on endpoint selection rather than on anything these tests are about.
	for _, c := range []*devnet.Client{wildcard, unverified} {
		require.NoError(t, c.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second))
		log.Info("--> Client added", "clientIP", c.CYOANetworkIP, "pubkey", c.Pubkey)
	}

	// The working path still works with enforcement on: proof obtained, attached, accepted,
	// tunnel up.
	t.Run("specific_pass_with_a_proof", func(t *testing.T) {
		out := connectIBRLForProofTest(t, log, dn, specific)

		require.Contains(t, out, "IP ownership verified for "+specific.CYOANetworkIP)
		require.Contains(t, out, "✅  User Provisioned")

		// The whole point of asserting BGP rather than stopping at account creation: enforcement
		// must not disturb anything downstream of the proof.
		require.NoError(t, specific.WaitForTunnelUp(t.Context(), 90*time.Second),
			"a user created under enforcement must still reach BGP")
	})

	// The case RFC-27 exists for.
	//
	// A wildcard access pass — stored at the 0.0.0.0 PDA, which is the shape the shred-oracle
	// issues — authorizes its payer for *any* routable address. Without a proof the program would
	// let that payer squat the User PDA of an address they do not control and point device tunnel
	// provisioning at a third party. For a specific-IP pass the issuing authority already chose
	// the IP, so the proof is redundant there; here it is the only thing binding client_ip.
	//
	// The wildcard pass had no e2e coverage of any kind before this: all 72 access-pass call sites
	// in e2e name a --client-ip.
	t.Run("wildcard_pass_with_a_proof", func(t *testing.T) {
		setWildcardAccessPass(t, dn, wildcard)

		log.Info("==> Connecting the verified client on a wildcard pass")
		out, err := wildcard.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl 2>&1"})
		output := string(out)
		log.Info("==> Connect output", "output", output)
		require.NoError(t, err, "connect failed: %s", output)

		require.Contains(t, output, "IP ownership verified for "+wildcard.CYOANetworkIP)
		require.Contains(t, output, "✅  User Provisioned")

		// The pass named no address, so the proof is what bound this one.
		users, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user list"})
		require.NoError(t, err)
		require.Contains(t, string(users), wildcard.CYOANetworkIP,
			"the user must be bound to the address the verifier observed")
	})

	// The enforcement moment: with the flag on, a client that cannot reach a verifier is refused,
	// and a wildcard pass does not rescue it.
	//
	// This is the one rejection in this file that is genuinely the program's. The CLI attaches no
	// proof, so nothing is caught client-side, and `create_user` fails with
	// `DoubleZeroError::IpOwnershipProofRequired` — custom program error 105 (0x69).
	t.Run("wildcard_pass_without_a_proof", func(t *testing.T) {
		setWildcardAccessPass(t, dn, unverified)

		log.Info("==> Connecting the unverified client on a wildcard pass")
		out, err := unverified.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl 2>&1"})
		output := string(out)
		log.Info("==> Connect output", "output", output)

		require.Error(t, err, "a wildcard pass must not admit an unproven address under enforcement")
		// Assert the specific refusal, not merely that connect failed: a test that passes because
		// connect broke for an unrelated reason would be worse than no test at all.
		require.Contains(t, output, "An IP ownership proof is required to create a user",
			"the refusal must be the program's IpOwnershipProofRequired, not an incidental failure")
		require.NotContains(t, output, "✅  User Provisioned")

		requireNoUserForIP(t, dn, unverified.CYOANetworkIP)
	})

	// The sentinel exemption, which is what keeps enforcement from breaking the shred-oracle.
	//
	// The oracle provisions multicast publishers owned by validators, for addresses the
	// verification service never sees a request from, so there is no proof it could obtain. The
	// program waives the *requirement* for a creation paid for by
	// `globalstate.sentinel_authority_pk`.
	//
	// In this devnet the manager is that authority (`smartcontract_init.go` runs
	// `authority set --sentinel-authority me`), and `doublezero user create` never attaches a
	// proof at all, so a manager-side create is the exemption in action. The contrast with
	// wildcard_pass_without_a_proof is the transaction payer, which is what `is_sentinel` compares.
	t.Run("sentinel_authority_is_exempt", func(t *testing.T) {
		// An address the manager owns a pass for. It has no container behind it; this subtest is
		// about whether the creation is admitted, not about tunnels.
		const sentinelUserIP = "9.0.0.9"

		_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero access-pass set --accesspass-type prepaid --epochs max --client-ip " +
				sentinelUserIP + " --user-payer me"})
		require.NoError(t, err)

		log.Info("==> Creating a user as the sentinel authority, with no proof")
		out, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
			"doublezero user create --device " + device.Spec.Code + " --client-ip " + sentinelUserIP + " 2>&1"})
		log.Info("==> User create output", "output", string(out))
		require.NoError(t, err, "the sentinel authority must be exempt from the proof requirement: %s", string(out))

		users, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user list"})
		require.NoError(t, err)
		require.Contains(t, string(users), sentinelUserIP)
	})
}

// A proof for an address other than the one being provisioned is refused.
//
// The daemon is told to provision an address this container does not own, so the CLI cannot bind
// its proof request to it (`probe_source_binding`) and the request leaves from the real CYOA
// address. The verifier signs what it observes, and `connect` refuses rather than binding an
// address it has no proof for.
//
// This needs its own devnet: DaemonClientIP is fixed when the client container starts, so the
// disagreement cannot be arranged on a client any other case can use.
//
// This asserts the client-side guard on purpose. The equivalent onchain error
// (`IpProofClientIpMismatch`, 108) is unreachable through the SDK, which pre-flights the same
// comparison before building a transaction; the program-level case is covered by
// `test_proof_for_a_different_client_ip_is_rejected`.
func TestE2E_IPOwnershipProof_EnforcedClientIpMismatchRefused(t *testing.T) {
	t.Parallel()

	// Routable, in the CYOA range, and not an address this container holds.
	const unownedIP = "9.0.0.7"

	dn, _, client, log := setupIPProofDevnet(t, devnet.IPVerifierSpec{}, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
		DaemonClientIP:      unownedIP,
	})

	log.Info("==> Enabling require-ip-ownership-proof")
	require.NoError(t, dn.SetIPOwnershipProofFeatureFlag(t.Context(), true))

	// The pass has to cover the address the daemon reports, or connect stops on the pass instead.
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --epochs max --client-ip " +
			unownedIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	log.Info("==> Connecting with a daemon client IP this host does not own",
		"provisioning", unownedIP, "observable", client.CYOANetworkIP)
	out, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl 2>&1"})
	output := string(out)
	log.Info("==> Connect output", "output", output)

	require.Error(t, err, "connect must refuse a proof for an address other than the one it binds")
	require.Contains(t, output, "The verification service observed this host at",
		"the refusal must be the address disagreement, not an incidental failure")
	require.NotContains(t, output, "✅  User Provisioned")

	requireNoUserForIP(t, dn, unownedIP)
}

// setWildcardAccessPass grants the client a prepaid pass with no --client-ip, which lands at the
// UNSPECIFIED (0.0.0.0) PDA and admits any routable address its payer can prove.
func setWildcardAccessPass(t *testing.T, dn *devnet.Devnet, client *devnet.Client) {
	t.Helper()
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --epochs max --user-payer " + client.Pubkey})
	require.NoError(t, err)
}

// requireNoUserForIP asserts a rejected creation left nothing onchain.
func requireNoUserForIP(t *testing.T, dn *devnet.Devnet, clientIP string) {
	t.Helper()
	users, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user list"})
	require.NoError(t, err)
	require.NotContains(t, string(users), clientIP,
		"no user may exist for a client whose creation was rejected")
}
