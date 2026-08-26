//go:build e2e

package e2e_test

import (
	"net"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// TestE2E_Connect_AllModesFromAccessPass verifies that a bare `doublezero connect`
// — no mode argument — provisions every mode the client's AccessPass authorizes in a
// single invocation: an IBRL tunnel plus a multicast tunnel joined to the groups the
// pass grants.
//
// The same two-tunnel end state is reached today only by running `connect ibrl` and
// `connect multicast` back to back (TestE2E_MultiTunnel_FallbackToSecondDevice). This
// asserts one command gets there, and that the two users land on different devices
// because the second leg sees the first leg's tunnel endpoint as taken.
//
// The reconciler is disabled first, so the run has to enable it itself rather than
// inheriting an already-managing daemon.
func TestE2E_Connect_AllModesFromAccessPass(t *testing.T) {
	t.Parallel()

	// Two devices, one client, and an mg01 allowlist granting the client both
	// publisher and subscriber on that group.
	dn, device1, device2, client := setupMultiTunnelDevnet(t)
	log := logger.With("test", t.Name())

	// A prepaid pass covering the current epoch. The IBRL leg is epoch-gated and the
	// multicast leg is not, so an expired pass here would silently test only half of
	// what this case is about.
	log.Info("==> Setting access pass for the client")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --epochs max --client-ip " +
			client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	// Turn the reconciler off so that enabling it is attributable to the connect.
	log.Info("==> Disabling the reconciler before connecting")
	_, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disable"})
	require.NoError(t, err)

	// The whole point: one command, no mode argument.
	log.Info("==> Running bare `doublezero connect`")
	out, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect 2>&1"})
	log.Info("==> Bare connect output", "output", string(out))
	require.NoError(t, err, "bare `doublezero connect` failed: %s", string(out))

	output := string(out)
	require.Contains(t, output, "Reconciler enabled",
		"the bare form must enable the reconciler itself")
	require.Contains(t, output, "IBRL: provisioned")
	require.Contains(t, output, "Multicast: provisioned")
	require.Contains(t, output, "✅  User Provisioned")

	// Both devices have to receive their agent config before BGP can come up; waiting
	// here turns a config-push failure into a clear message instead of a tunnel timeout.
	log.Info("==> Waiting for agent config on both devices")
	waitForAgentConfigWithClient(t, log, dn, device1, client)
	waitForAgentConfigWithClient(t, log, dn, device2, client)

	log.Info("==> Waiting for both tunnels to come up")
	require.NoError(t, client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second),
		"one bare connect must bring up both tunnels")

	tunnels, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnels, 2, "expected exactly two tunnels from a single bare connect")

	var ibrlTunnel, mcastTunnel *devnet.ClientStatusResponse
	for i := range tunnels {
		switch tunnels[i].UserType {
		case devnet.ClientUserTypeIBRL, devnet.ClientUserTypeIBRLWithAllocated:
			ibrlTunnel = &tunnels[i]
		case devnet.ClientUserTypeMulticast:
			mcastTunnel = &tunnels[i]
		}
	}
	require.NotNil(t, ibrlTunnel, "bare connect must produce an IBRL tunnel")
	require.NotNil(t, mcastTunnel, "bare connect must produce a Multicast tunnel")

	// The multicast leg runs second and must see the IBRL leg's endpoint as taken.
	log.Info("==> Tunnel destinations",
		"ibrl_dst", ibrlTunnel.TunnelDst.String(),
		"mcast_dst", mcastTunnel.TunnelDst.String())
	require.NotEqual(t, ibrlTunnel.TunnelDst.String(), mcastTunnel.TunnelDst.String(),
		"the two legs must not share a tunnel endpoint")

	// Onchain: two activated users for this client IP, one per type, and the multicast
	// user holding exactly the roles the access pass granted on mg01.
	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	parsed := net.ParseIP(client.CYOANetworkIP).To4()
	require.NotNil(t, parsed, "could not parse client IP: %s", client.CYOANetworkIP)
	wantClientIP := [4]uint8{parsed[0], parsed[1], parsed[2], parsed[3]}

	var mg01 [32]byte
	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		for _, g := range data.MulticastGroups {
			if g.Code == "mg01" {
				mg01 = g.PubKey
				return true
			}
		}
		return false
	}, 30*time.Second, 2*time.Second, "multicast group mg01 not found in program data")

	holds := func(list [][32]uint8, want [32]byte) bool {
		for _, pk := range list {
			if pk == want {
				return true
			}
		}
		return false
	}

	require.Eventually(t, func() bool {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return false
		}
		var ibrl, mcast *serviceability.User
		for i := range data.Users {
			u := &data.Users[i]
			if u.ClientIp != wantClientIP || u.Status != serviceability.UserStatusActivated {
				continue
			}
			switch u.UserType {
			case serviceability.UserTypeIBRL, serviceability.UserTypeIBRLWithAllocatedIP:
				ibrl = u
			case serviceability.UserTypeMulticast:
				mcast = u
			}
		}
		if ibrl == nil || mcast == nil {
			return false
		}
		// The pass allowlists the client as both publisher and subscriber on mg01,
		// so the auto-join must claim both roles and nothing beyond them.
		return len(mcast.Publishers) == 1 && holds(mcast.Publishers, mg01) &&
			len(mcast.Subscribers) == 1 && holds(mcast.Subscribers, mg01)
	}, 60*time.Second, 2*time.Second,
		"expected an activated IBRL user and an activated Multicast user publishing and subscribing to mg01")

	log.Info("--> Bare connect provisioned both modes from the access pass")
}

// TestE2E_Connect_SameMetroThenDisconnectPerMode covers the shape a feed customer actually
// sees: two devices in one metro, a bare `doublezero connect` whose second leg has to fall
// back to the second device because the first one's only tunnel endpoint went to the IBRL
// leg, and then a teardown one mode at a time.
//
// Same metro is the whole point, and it is what TestE2E_MultiTunnel_FallbackToSecondDevice
// does not cover: there the two devices sit in different metros, so latency alone can
// separate them. A purchased feed only admits devices in its own metro, so a feed customer's
// fallback device is always a same-metro one, and endpoint exclusion is the only thing that
// can push the second leg off device 1. The pass here grants a multicast group rather than a
// feed because feed seats are written by the SetAccessPassFeeds instruction, which has no CLI
// surface for the devnet to drive; the bare form's feed path is covered by unit tests instead.
func TestE2E_Connect_SameMetroThenDisconnectPerMode(t *testing.T) {
	t.Parallel()

	// Both devices in ewr/xewr — the New York metro.
	dn, device1, device2, client := setupMultiTunnelDevnetWithSecondDevice(t, secondDeviceSpec{
		Code:     "ny5-dz02",
		Location: "ewr",
		Exchange: "xewr",
	})
	log := logger.With("test", t.Name())

	log.Info("==> Setting access pass for the client")
	_, err := dn.Manager.Exec(t.Context(), []string{"bash", "-c",
		"doublezero access-pass set --accesspass-type prepaid --epochs max --client-ip " +
			client.CYOANetworkIP + " --user-payer " + client.Pubkey})
	require.NoError(t, err)

	log.Info("==> Running bare `doublezero connect`")
	out, err := client.Exec(t.Context(), []string{"bash", "-c", "doublezero connect 2>&1"})
	log.Info("==> Bare connect output", "output", string(out))
	require.NoError(t, err, "bare `doublezero connect` failed: %s", string(out))
	require.Contains(t, string(out), "IBRL: provisioned")
	require.Contains(t, string(out), "Multicast: provisioned")

	log.Info("==> Waiting for agent config on both devices")
	waitForAgentConfigWithClient(t, log, dn, device1, client)
	waitForAgentConfigWithClient(t, log, dn, device2, client)

	log.Info("==> Waiting for both tunnels to come up")
	require.NoError(t, client.WaitForNTunnelsUp(t.Context(), 2, 90*time.Second),
		"one bare connect must bring up both tunnels")

	tunnels, err := client.GetTunnelStatus(t.Context())
	require.NoError(t, err)
	require.Len(t, tunnels, 2, "expected exactly two tunnels from a single bare connect")

	var ibrlDst, mcastDst string
	for _, tunnel := range tunnels {
		switch tunnel.UserType {
		case devnet.ClientUserTypeIBRL, devnet.ClientUserTypeIBRLWithAllocated:
			ibrlDst = tunnel.TunnelDst.String()
		case devnet.ClientUserTypeMulticast:
			mcastDst = tunnel.TunnelDst.String()
		}
	}
	require.NotEmpty(t, ibrlDst, "bare connect must produce an IBRL tunnel")
	require.NotEmpty(t, mcastDst, "bare connect must produce a Multicast tunnel")

	// Both devices serve the same metro, so nothing but the endpoint exclusion can have
	// separated the legs.
	deviceIPs := []string{device1.CYOANetworkIP, device2.CYOANetworkIP}
	require.Contains(t, deviceIPs, ibrlDst, "IBRL landed on neither device")
	require.Contains(t, deviceIPs, mcastDst, "Multicast landed on neither device")
	require.NotEqual(t, ibrlDst, mcastDst,
		"both legs landed on the same device: the multicast leg did not see the IBRL leg's endpoint as taken")
	log.Info("--> Legs landed on different devices in the same metro",
		"ibrl_dst", ibrlDst, "mcast_dst", mcastDst)

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)

	parsed := net.ParseIP(client.CYOANetworkIP).To4()
	require.NotNil(t, parsed, "could not parse client IP: %s", client.CYOANetworkIP)
	wantClientIP := [4]uint8{parsed[0], parsed[1], parsed[2], parsed[3]}

	// Activated, non-deleting users for this client IP, by type.
	liveUserTypes := func() (map[serviceability.UserUserType]bool, error) {
		data, err := serviceabilityClient.GetProgramData(t.Context())
		if err != nil {
			return nil, err
		}
		live := map[serviceability.UserUserType]bool{}
		for i := range data.Users {
			u := &data.Users[i]
			if u.ClientIp == wantClientIP && u.Status == serviceability.UserStatusActivated {
				live[u.UserType] = true
			}
		}
		return live, nil
	}

	// === Disconnect IBRL only ===
	log.Info("==> Disconnecting IBRL only")
	out, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect ibrl 2>&1"})
	log.Info("==> disconnect ibrl output", "output", string(out))
	require.NoError(t, err, "`doublezero disconnect ibrl` failed: %s", string(out))

	require.Eventually(t, func() bool {
		tunnels, err := client.GetTunnelStatus(t.Context())
		if err != nil || len(tunnels) != 1 {
			return false
		}
		// The survivor must be the multicast tunnel, still on the device it started on:
		// disconnecting one mode must not move or disturb the other.
		return tunnels[0].UserType == devnet.ClientUserTypeMulticast &&
			tunnels[0].TunnelDst.String() == mcastDst
	}, 90*time.Second, 2*time.Second,
		"`doublezero disconnect ibrl` must drop only the IBRL tunnel and leave multicast on %s", mcastDst)

	require.Eventually(t, func() bool {
		live, err := liveUserTypes()
		if err != nil {
			return false
		}
		return !live[serviceability.UserTypeIBRL] &&
			!live[serviceability.UserTypeIBRLWithAllocatedIP] &&
			live[serviceability.UserTypeMulticast]
	}, 90*time.Second, 2*time.Second,
		"onchain, only the IBRL user should be gone after `doublezero disconnect ibrl`")
	log.Info("--> IBRL disconnected; multicast untouched")

	// === Disconnect Multicast ===
	log.Info("==> Disconnecting Multicast")
	out, err = client.Exec(t.Context(), []string{"bash", "-c", "doublezero disconnect multicast 2>&1"})
	log.Info("==> disconnect multicast output", "output", string(out))
	require.NoError(t, err, "`doublezero disconnect multicast` failed: %s", string(out))

	require.Eventually(t, func() bool {
		live, err := liveUserTypes()
		if err != nil {
			return false
		}
		return len(live) == 0
	}, 90*time.Second, 2*time.Second,
		"no activated user should remain for this client after disconnecting both modes")

	require.Eventually(t, func() bool {
		tunnels, err := client.GetTunnelStatus(t.Context())
		if err != nil {
			return false
		}
		for _, tunnel := range tunnels {
			if tunnel.DoubleZeroStatus.SessionStatus == devnet.ClientSessionStatusUp {
				return false
			}
		}
		return true
	}, 90*time.Second, 2*time.Second, "no tunnel should still be up after disconnecting both modes")

	log.Info("--> Both modes torn down one at a time")
}
