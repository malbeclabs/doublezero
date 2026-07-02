//go:build e2e

package e2e_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/arista"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/stretchr/testify/require"
)

// TestE2E_MulticastPublisherRegisterLifecycle verifies the client-originated PIM
// Register beacon (RFC-22) keeps the device originating a published source into
// MSDP across publisher/subscriber role transitions.
//
// The case that matters is the dual-role tunnel: when the same client both
// publishes and subscribes to a group, the subscriber-side PIM neighbor
// suppresses `pim ipv4 border-router` source injection, so the Register is the
// only thing that still sets the "may notify MSDP" (N) flag on the (S, G). The
// device records a source learned from a Register with the "learned via a
// register" (C) flag, so a dual-role (S, G) carrying both C and N is direct
// evidence the beacon is working.
//
// The test also guards the RegisterSender lifecycle. The sender is a
// daemon-lifetime singleton reused across connect/disconnect: a disconnect tears
// it down (Close) and a reconnect re-creates it (Start). If the restart path
// regresses, the beacon goes silent and the (S, G) loses its C and N flags. The
// final phase reconnects as dual-role and requires the flags to come back.
func TestE2E_MulticastPublisherRegisterLifecycle(t *testing.T) {
	t.Parallel()

	dn, device, _, client := setupCoexistenceTestDevnet(t)
	log := newTestLoggerForTest(t)

	// mg01 maps to this group address in the devnet.
	const group = "233.84.178.0"

	// Phase 1: publisher only. The published source shows up on the device.
	if !t.Run("publisher_only", func(t *testing.T) {
		_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast publisher mg01 2>&1"})
		require.NoError(t, err, "connect as multicast publisher")

		require.NoError(t, client.WaitForTunnelUp(t.Context(), 90*time.Second))
		waitForAgentConfigWithClient(t, log, dn, device, client)

		// Reuses the coexistence helper: polls until the (S, G) source appears.
		verifyMulticastPublisherMrouteState(t, log, device, client)
	}) {
		t.Fail()
		return
	}

	// Phase 2: add the subscriber role in place, making the tunnel dual-role. The
	// PIM neighbor now suppresses border-router injection, so origination survives
	// only because the client's Register drives it. Require C (learned via a
	// register) and N (may notify MSDP) on the (S, G).
	if !t.Run("add_subscriber_dual_role", func(t *testing.T) {
		_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero multicast subscribe mg01 2>&1"})
		require.NoError(t, err, "add subscriber role to the connected publisher")

		// Border-router injection is now suppressed by this neighbor.
		verifyMulticastSubscriberPIMAdjacency(t, log, device)

		source := multicastAllocatedIP(t, client)
		requireMrouteSourceFlags(t, log, device, group, source, "CN")
	}) {
		t.Fail()
		return
	}

	// Phase 3: disconnect and reconnect as dual-role. This exercises the
	// RegisterSender teardown (Close) and re-create (Start). The beacon must
	// resume, so the (S, G) regains its C and N flags.
	if !t.Run("reconnect_dual_role_register_resumes", func(t *testing.T) {
		_, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast 2>&1"})
		require.NoError(t, err, "disconnect multicast")
		verifyTunnelRemoved(t, client, "doublezero1")

		_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect multicast --publish mg01 --subscribe mg01 2>&1"})
		require.NoError(t, err, "reconnect as dual-role publisher and subscriber")

		require.NoError(t, client.WaitForTunnelUp(t.Context(), 90*time.Second))
		waitForAgentConfigWithClient(t, log, dn, device, client)
		verifyMulticastSubscriberPIMAdjacency(t, log, device)

		source := multicastAllocatedIP(t, client)
		requireMrouteSourceFlags(t, log, device, group, source, "CN")
	}) {
		t.Fail()
	}
}

// multicastAllocatedIP returns the DoubleZero IP allocated to the client's
// multicast tunnel, which is the source address of the published (S, G).
func multicastAllocatedIP(t *testing.T, client *devnet.Client) string {
	t.Helper()
	status, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	for _, ts := range status {
		if ts.UserType == devnet.ClientUserTypeMulticast {
			require.NotNil(t, ts.DoubleZeroIP, "multicast tunnel has no DoubleZero IP")
			return ts.DoubleZeroIP.String()
		}
	}
	t.Fatal("no multicast tunnel found in client status")
	return ""
}

// requireMrouteSourceFlags polls the device mroute until the (source, group)
// entry exists and its route flags contain every letter in mustContain. The
// beacon has a startup jitter of up to its interval (default 60s) before the
// first Register, so the C flag can take that long to appear after a
// (re)connect; the poll window accounts for it.
func requireMrouteSourceFlags(t *testing.T, log *slog.Logger, device *devnet.Device, group, source, mustContain string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		mroutes, err := devnet.DeviceExecAristaCliJSON[*arista.ShowIPMroute](t.Context(), device, arista.ShowIPMrouteCmd())
		if err != nil {
			log.Debug("error fetching mroutes", "error", err)
			return false
		}
		g, ok := mroutes.Groups[group]
		if !ok {
			log.Debug("group not yet in mroutes", "group", group)
			return false
		}
		src, ok := g.GroupSources[source]
		if !ok {
			log.Debug("source not yet in group", "group", group, "source", source)
			return false
		}
		for _, f := range mustContain {
			if !strings.ContainsRune(src.RouteFlags, f) {
				log.Debug("mroute flags missing required flag", "group", group, "source", source, "flags", src.RouteFlags, "required", mustContain)
				return false
			}
		}
		log.Info("mroute source flags verified", "group", group, "source", source, "flags", src.RouteFlags, "required", mustContain)
		return true
	}, 120*time.Second, 2*time.Second, "(%s, %s) mroute flags never contained all of %q", source, group, mustContain)
}
