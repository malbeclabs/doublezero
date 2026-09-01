//go:build e2e

package e2e_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/random"
	"github.com/malbeclabs/doublezero/e2e/internal/solana"
	"github.com/stretchr/testify/require"
)

// RFC-27 IP ownership proofs, end to end: `connect` asks the verification service for a proof,
// attaches it to the user creation, and the program validates it through the Ed25519 precompile.
//
// Every e2e devnet runs a verifier (IPVerifierSpec.Disabled defaults to false), so the ordinary
// connect path in every other test already carries a real proof. What is left to cover here is
// the three outcomes that path can have, which no other test distinguishes:
//
//   - a proof the program accepts,
//   - no proof at all, which the program also accepts while require-ip-ownership-proof is clear,
//   - a proof signed by a key the program does not trust, which it must reject.

// A valid proof is obtained and attached, and the user is created.
func TestE2E_IPOwnershipProof_ValidProof(t *testing.T) {
	t.Parallel()

	dn, _, client, log := setupIPProofDevnet(t, devnet.IPVerifierSpec{}, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})

	out := connectIBRLForProofTest(t, log, dn, client)

	// The verifier signed the address the client is provisioning. `connect` prints this only
	// after checking that the address the service observed matches the one being bound, so it is
	// evidence of the whole round trip, not just of a reachable service.
	require.Contains(t, out, "IP ownership verified for "+client.CYOANetworkIP,
		"connect must obtain a proof for the address it is provisioning")
	require.Contains(t, out, "✅  User Provisioned")

	// No proof would also have been accepted here, so assert the run did not quietly fall back.
	require.NotContains(t, out, "Continuing without an IP ownership proof")

	require.NoError(t, client.WaitForTunnelUp(t.Context(), 90*time.Second),
		"a user created with a proof must still come up normally")
}

// No verifier to reach, no proof, and the create is accepted anyway: enforcement is gated on the
// require-ip-ownership-proof feature flag, which is clear in a local devnet. This is the path a
// deployed environment takes before its verifier exists, so it has to keep working.
func TestE2E_IPOwnershipProof_NoProof(t *testing.T) {
	t.Parallel()

	// The devnet still runs a verifier; this client is simply not pointed at it, which is what a
	// client with no configured verifier looks like.
	dn, _, client, log := setupIPProofDevnet(t, devnet.IPVerifierSpec{}, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
		NoIPVerifier:        true,
	})

	out := connectIBRLForProofTest(t, log, dn, client)

	require.NotContains(t, out, "IP ownership verified for",
		"a client with no verifier configured must not report a proof")
	require.Contains(t, out, "✅  User Provisioned",
		"a create without a proof must be accepted while require-ip-ownership-proof is clear")

	require.NoError(t, client.WaitForTunnelUp(t.Context(), 90*time.Second))
}

// A proof signed by a key the program does not trust must be rejected.
//
// The rotation is what makes the proof invalid. The verifier re-reads
// GlobalState.ip_verifier_authority_pk periodically and stops serving when it no longer names its
// own key, so the refresh is pinned long enough that it does not notice: it keeps signing with the
// key it started with while GlobalState names a different one. `connect` reads the verifier key
// from GlobalState — the proof does not carry it — and finds the signature does not verify
// against it.
//
// Where the refusal lands: the SDK checks the proof against the onchain verifier key before it
// builds the transaction, so this fails client-side rather than onchain. That is the intended
// design — the "refused before the transaction is paid for" pre-flight — but it does mean this
// test does not reach the program's own Ed25519 precompile check. Covering that would take a
// client that skips the pre-flight, which `connect` gives no way to do; the program-side check
// has unit coverage in the serviceability program.
func TestE2E_IPOwnershipProof_UntrustedSigner(t *testing.T) {
	t.Parallel()

	dn, _, client, log := setupIPProofDevnet(t, devnet.IPVerifierSpec{
		// Comfortably longer than the test: the service must not observe the rotation.
		AuthorityRefreshSecs: 3600,
	}, devnet.ClientSpec{
		CYOANetworkIPHostID: 100,
	})

	// Rotate the trust root out from under the running verifier.
	keypairJSON, err := solana.GenerateKeypairJSON()
	require.NoError(t, err)
	rotated, err := solana.PubkeyFromKeypairJSON(keypairJSON)
	require.NoError(t, err)
	require.NotEqual(t, dn.IPVerifier.Pubkey, rotated)

	log.Info("==> Rotating the verifier authority away from the running service",
		"from", dn.IPVerifier.Pubkey, "to", rotated)
	require.NoError(t, dn.SetIPVerifierAuthority(t.Context(), rotated))

	setAccessPass(t, dn, client)

	log.Info("==> Connecting with a proof signed by the pre-rotation key")
	out, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl 2>&1"})
	output := string(out)
	log.Info("==> Connect output", "output", output)

	// The verifier still answers — it has not re-read the authority — so a proof is obtained and
	// attached. It is the transaction that must fail.
	require.Contains(t, output, "IP ownership verified for "+client.CYOANetworkIP,
		"the service should still be issuing proofs; if it is not, the rotation was noticed and "+
			"this test is no longer covering an untrusted signature")
	require.Error(t, err, "a proof signed by a key GlobalState does not name must not create a user")
	require.Contains(t, output, "does not verify against the onchain verifier "+rotated,
		"the refusal must name the rotated key it checked against")
	require.NotContains(t, output, "✅  User Provisioned")

	// And nothing landed onchain: a rejected transaction must not leave a half-created user.
	users, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c", "doublezero user list"})
	require.NoError(t, err)
	require.NotContains(t, string(users), client.CYOANetworkIP,
		"no user may exist for a client whose creation was rejected")
}

// setAccessPass grants the client a prepaid pass, which every connect needs before it gets as far
// as the proof.
func setAccessPass(t *testing.T, dn *devnet.Devnet, client *devnet.Client) {
	t.Helper()
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --epochs max --client-ip " +
			client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)
}

func connectIBRLForProofTest(t *testing.T, log *slog.Logger, dn *devnet.Devnet, client *devnet.Client) string {
	t.Helper()

	setAccessPass(t, dn, client)

	log.Info("==> Connecting IBRL")
	out, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect ibrl 2>&1"})
	log.Info("==> Connect output", "output", string(out))
	require.NoError(t, err, "connect failed: %s", string(out))
	return string(out)
}

// setupIPProofDevnet builds the smallest devnet these tests need: one device, one client, and the
// verifier the spec asks for.
func setupIPProofDevnet(t *testing.T, verifier devnet.IPVerifierSpec, clientSpec devnet.ClientSpec) (*devnet.Devnet, *devnet.Device, *devnet.Client, *slog.Logger) {
	deployID := "dz-e2e-" + t.Name() + "-" + random.ShortID()
	log := logger.With("test", t.Name(), "deployID", deployID)

	currentDir, err := os.Getwd()
	require.NoError(t, err)
	serviceabilityProgramKeypairPath := filepath.Join(currentDir, "data", "serviceability-program-keypair.json")

	dn, err := devnet.New(devnet.DevnetSpec{
		DeployID:  deployID,
		DeployDir: t.TempDir(),

		CYOANetwork: devnet.CYOANetworkSpec{
			CIDRPrefix: subnetCIDRPrefix,
		},
		Manager: devnet.ManagerSpec{
			ServiceabilityProgramKeypairPath: serviceabilityProgramKeypairPath,
		},
		IPVerifier: verifier,
	}, log, dockerClient, subnetAllocator)
	require.NoError(t, err)

	log.Info("==> Starting devnet")
	require.NoError(t, dn.Start(t.Context(), nil))

	require.NotNil(t, dn.IPVerifier, "the devnet must run a verifier for these tests")
	log.Info("--> IP verifier running", "pubkey", dn.IPVerifier.Pubkey, "url", dn.IPVerifier.InternalURL)

	device, err := dn.AddDevice(t.Context(), devnet.DeviceSpec{
		Code:                         "ny5-dz01",
		Location:                     "ewr",
		Exchange:                     "xewr",
		CYOANetworkIPHostID:          8,
		CYOANetworkAllocatablePrefix: 29,
	})
	require.NoError(t, err)

	_, err = dn.Manager.Exec(t.Context(), []string{"bash", "-c", `
		set -euo pipefail
		doublezero device interface create ny5-dz01 "Ethernet2" --bandwidth 10G -w
		doublezero device interface create ny5-dz01 "Loopback255" --loopback-type vpnv4 --bandwidth 10G -w
		doublezero device interface create ny5-dz01 "Loopback256" --loopback-type ipv4 --bandwidth 10G -w
	`})
	require.NoError(t, err)

	client, err := dn.AddClient(t.Context(), clientSpec)
	require.NoError(t, err)
	log.Info("--> Client added", "clientIP", client.CYOANetworkIP, "pubkey", client.Pubkey)

	// The client picks a device from its own latency measurements; connecting before they exist
	// fails on endpoint selection rather than on anything this test is about.
	require.NoError(t, client.WaitForLatencyResults(t.Context(), device.ID, 75*time.Second))

	return dn, device, client, log
}
