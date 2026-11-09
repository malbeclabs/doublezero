//go:build e2e

package e2e_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/malbeclabs/doublezero/e2e/internal/docker"
	e2esolana "github.com/malbeclabs/doublezero/e2e/internal/solana"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

// TestE2E_PrivateMulticastGroup connects five hosts that share one DZ ID, and so one EdgeSeat
// access pass, to a feed and to a private multicast group (malbeclabs/infra#3706).
//
// The pass holds the feed `shreds-e2e` for 4 users, so its multicast user cap is 4. The group
// `private-e2e` is private and admits at most 2 publishers and 3 subscribers. A host that joins
// only the private group does not count against the cap of 4 until it joins the feed. Hosts A and
// B join the private group and the feed as subscribers in opposite orders, and hosts C and D do
// the same as publishers, so each pair must end in the same state. Host E stays on the private
// group only. The hosts disconnect in the opposite order from the joins, and every count must go
// back to 0 before the admin closes the pass.
func TestE2E_PrivateMulticastGroup(t *testing.T) {
	t.Parallel()

	dn, device, _ := NewSingleDeviceSingleClientTestDevnet(t)
	log := logger.With("test", t.Name())
	ctx := t.Context()

	keypairJSON, err := e2esolana.GenerateKeypairJSON()
	require.NoError(t, err)
	keypairPath := filepath.Join(t.TempDir(), "dz-id.json")
	require.NoError(t, os.WriteFile(keypairPath, keypairJSON, 0o600))

	hosts := map[string]*devnet.Client{}
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		client, err := dn.AddClient(ctx, devnet.ClientSpec{
			CYOANetworkIPHostID: uint32(101 + i),
			KeypairPath:         keypairPath,
		})
		require.NoError(t, err)
		hosts[name] = client
	}
	hostA, hostB, hostC, hostD, hostE := hosts["a"], hosts["b"], hosts["c"], hosts["d"], hosts["e"]
	dzID := hostA.Pubkey

	manager := func(t *testing.T, command string) {
		t.Helper()
		output, err := dn.Manager.Exec(ctx, []string{"bash", "-c", command})
		require.NoError(t, err, "%s: %s", command, output)
	}
	runOnHost := func(host *devnet.Client, command string) ([]byte, error) {
		output, err := host.Exec(ctx, []string{"bash", "-c", command + " 2>&1"}, docker.NoPrintOnError())
		log.Info("==> host command", "host", host.CYOANetworkIP, "command", command, "output", string(output))
		return output, err
	}
	connect := func(t *testing.T, host *devnet.Client, args string) {
		t.Helper()
		output, err := runOnHost(host, "doublezero connect multicast --device "+device.Spec.Code+" "+args)
		require.NoError(t, err, "connect multicast %s: %s", args, output)
	}
	connectRefused := func(t *testing.T, host *devnet.Client, args string) {
		t.Helper()
		output, err := runOnHost(host, "doublezero connect multicast --device "+device.Spec.Code+" "+args)
		require.Error(t, err, "connect multicast %s must be refused: %s", args, output)
	}

	serviceabilityClient, err := dn.Ledger.GetServiceabilityClient()
	require.NoError(t, err)
	programData := func(t *testing.T) *serviceability.ProgramData {
		t.Helper()
		data, err := serviceabilityClient.GetProgramData(ctx)
		require.NoError(t, err)
		return data
	}
	groupPubkey := func(t *testing.T, code string) [32]byte {
		t.Helper()
		for _, group := range programData(t).MulticastGroups {
			if group.Code == code {
				return group.PubKey
			}
		}
		t.Fatalf("multicast group %s not found", code)
		return [32]byte{}
	}
	hostIP := func(host *devnet.Client) [4]uint8 {
		ip := net.ParseIP(host.CYOANetworkIP).To4()
		return [4]uint8{ip[0], ip[1], ip[2], ip[3]}
	}

	// waitUntil polls in the test goroutine, because readState fails the test through require.
	waitUntil := func(t *testing.T, condition func() bool, message func() string) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatal(message())
			}
			time.Sleep(2 * time.Second)
		}
	}

	type state struct {
		passPubkey   [32]byte
		passUsers    uint16
		passCap      uint16
		feedSeatUser uint8
		groups       map[string][2]uint32
		users        map[[4]uint8]serviceability.User
	}
	readState := func(t *testing.T) state {
		t.Helper()
		data := programData(t)
		payer := solana.MustPublicKeyFromBase58(dzID)
		read := state{groups: map[string][2]uint32{}, users: map[[4]uint8]serviceability.User{}}
		for _, pass := range data.AccessPasses {
			if pass.UserPayer == payer && pass.ClientIp == [4]uint8{} {
				read.passPubkey = pass.PubKey
				read.passUsers, read.passCap = pass.MulticastUserCount, pass.MaxMulticastUsers
				for _, seat := range pass.FeedSeats {
					read.feedSeatUser = seat.CurrentUsers
				}
			}
		}
		for _, group := range data.MulticastGroups {
			read.groups[group.Code] = [2]uint32{group.PublisherCount, group.SubscriberCount}
		}
		for _, user := range data.Users {
			if user.UserType == serviceability.UserTypeMulticast {
				read.users[user.ClientIp] = user
			}
		}
		return read
	}
	// requireCounts waits until the pass, the feed seat and the private group hold the given
	// counts, because a disconnect returns before the ledger shows the deleted user.
	requireCounts := func(t *testing.T, passUsers uint16, feedSeatUsers uint8, privatePublishers, privateSubscribers uint32) {
		t.Helper()
		var last state
		waitUntil(t, func() bool {
			last = readState(t)
			return last.passUsers == passUsers &&
				last.feedSeatUser == feedSeatUsers &&
				last.groups["private-e2e"] == [2]uint32{privatePublishers, privateSubscribers}
		}, func() string {
			return fmt.Sprintf("want pass users %d, feed seat users %d, private group %d/%d, got %d, %d, %v",
				passUsers, feedSeatUsers, privatePublishers, privateSubscribers,
				last.passUsers, last.feedSeatUser, last.groups["private-e2e"])
		})
	}

	if !t.Run("set_up_the_pass_the_feed_and_the_groups", func(t *testing.T) {
		for _, code := range []string{"feed-e2e-a", "feed-e2e-b", "private-e2e", "comped-e2e"} {
			manager(t, "doublezero multicast group create --code "+code+" --max-bandwidth 10Gbps --owner me -w")
		}
		manager(t, "doublezero feed create --code shreds-e2e --name 'Shreds E2E' --exchange "+device.Spec.Exchange+
			" --group feed-e2e-a --group feed-e2e-b")
		manager(t, "doublezero access-pass set --accesspass-type edge-seat --epochs max --allow-multiple-ip"+
			" --user-payer "+dzID+" --max-unicast-users 0 --max-multicast-users 0")
		manager(t, "doublezero access-pass add-feed --user-payer "+dzID+" --feed 'Shreds E2E' --max-users 4"+
			" --terminates-at "+time.Now().UTC().AddDate(0, 1, 0).Format("2006-01-02")+" <<< y")

		manager(t, "doublezero multicast group update --pubkey private-e2e --private true --max-publishers 2 --max-subscribers 3")
		manager(t, "doublezero multicast group allowlist subscriber add --code private-e2e --user-payer "+dzID+" --client-ip 0.0.0.0")
		manager(t, "doublezero multicast group allowlist publisher add --code private-e2e --user-payer "+dzID+" --client-ip 0.0.0.0")
		manager(t, "doublezero multicast group allowlist subscriber add --code comped-e2e --user-payer "+dzID+" --client-ip 0.0.0.0")

		read := readState(t)
		require.Equal(t, uint16(4), read.passCap, "add-feed must set the pass-wide cap to the feed's 4 users")
		requireCounts(t, 0, 0, 0, 0)
	}) {
		t.FailNow()
	}

	privateGroup := groupPubkey(t, "private-e2e")
	feedGroupA := groupPubkey(t, "feed-e2e-a")
	feedGroupB := groupPubkey(t, "feed-e2e-b")
	compedGroup := groupPubkey(t, "comped-e2e")

	if !t.Run("host_a_subscribes_to_the_private_group_then_joins_the_feed", func(t *testing.T) {
		connect(t, hostA, "--subscribe private-e2e")
		requireCounts(t, 0, 0, 0, 1)
		require.False(t, readState(t).users[hostIP(hostA)].HoldsPassSeat)

		connect(t, hostA, "--subscribe-feed shreds-e2e")
		requireCounts(t, 1, 1, 0, 1)
		require.True(t, readState(t).users[hostIP(hostA)].HoldsPassSeat)
	}) {
		t.FailNow()
	}

	if !t.Run("host_b_joins_the_feed_then_subscribes_to_the_private_group", func(t *testing.T) {
		connect(t, hostB, "--subscribe-feed shreds-e2e")
		requireCounts(t, 2, 2, 0, 1)

		connect(t, hostB, "--subscribe private-e2e")
		requireCounts(t, 2, 2, 0, 2)
	}) {
		t.FailNow()
	}

	if !t.Run("hosts_a_and_b_end_in_the_same_state", func(t *testing.T) {
		read := readState(t)
		for _, host := range []*devnet.Client{hostA, hostB} {
			user := read.users[hostIP(host)]
			require.ElementsMatch(t, [][32]byte{privateGroup, feedGroupA, feedGroupB}, user.Subscribers)
			require.Empty(t, user.Publishers)
			require.Len(t, user.FeedPks, 1)
			require.True(t, user.HoldsPassSeat)
		}
	}) {
		t.FailNow()
	}

	if !t.Run("host_e_subscribes_to_the_private_group_only", func(t *testing.T) {
		connect(t, hostE, "--subscribe private-e2e")
		requireCounts(t, 2, 2, 0, 3)
		user := readState(t).users[hostIP(hostE)]
		require.Empty(t, user.FeedPks)
		require.False(t, user.HoldsPassSeat)
	}) {
		t.FailNow()
	}

	if !t.Run("host_c_publishes_to_the_private_group_then_joins_the_feed", func(t *testing.T) {
		connect(t, hostC, "--publish private-e2e")
		requireCounts(t, 2, 2, 1, 3)

		connect(t, hostC, "--subscribe-feed shreds-e2e")
		requireCounts(t, 3, 3, 1, 3)
	}) {
		t.FailNow()
	}

	if !t.Run("host_d_joins_the_feed_then_publishes_to_the_private_group", func(t *testing.T) {
		connect(t, hostD, "--subscribe-feed shreds-e2e")
		requireCounts(t, 4, 4, 1, 3)

		connect(t, hostD, "--publish private-e2e")
		requireCounts(t, 4, 4, 2, 3)
	}) {
		t.FailNow()
	}

	if !t.Run("hosts_c_and_d_end_in_the_same_state", func(t *testing.T) {
		read := readState(t)
		for _, host := range []*devnet.Client{hostC, hostD} {
			user := read.users[hostIP(host)]
			require.ElementsMatch(t, [][32]byte{privateGroup}, user.Publishers)
			require.ElementsMatch(t, [][32]byte{feedGroupA, feedGroupB}, user.Subscribers)
			require.Len(t, user.FeedPks, 1)
			require.True(t, user.HoldsPassSeat)
		}
	}) {
		t.FailNow()
	}

	if !t.Run("the_private_group_refuses_a_role_past_its_caps", func(t *testing.T) {
		// The private group already has 2 publishers and 3 subscribers.
		connectRefused(t, hostA, "--publish private-e2e")
		connectRefused(t, hostC, "--subscribe private-e2e")
		requireCounts(t, 4, 4, 2, 3)
	}) {
		t.FailNow()
	}

	if !t.Run("host_e_cannot_join_the_feed_while_its_seats_are_full", func(t *testing.T) {
		connectRefused(t, hostE, "--subscribe-feed shreds-e2e")
		requireCounts(t, 4, 4, 2, 3)
		require.Empty(t, readState(t).users[hostIP(hostE)].FeedPks)
	}) {
		t.FailNow()
	}

	if !t.Run("host_b_subscribes_to_a_group_that_is_not_private", func(t *testing.T) {
		connect(t, hostB, "--subscribe comped-e2e")
		requireCounts(t, 4, 4, 2, 3)
		read := readState(t)
		require.Equal(t, [2]uint32{0, 1}, read.groups["comped-e2e"])
		require.Contains(t, read.users[hostIP(hostB)].Subscribers, compedGroup)
	}) {
		t.FailNow()
	}

	// The hosts disconnect in the opposite order from the joins. Host A joined the private group
	// first and goes before host B. Host D joined the feed first and goes before host C.
	disconnect := func(t *testing.T, host *devnet.Client) {
		t.Helper()
		output, err := runOnHost(host, "doublezero disconnect multicast")
		require.NoError(t, err, "disconnect multicast: %s", output)
		waitUntil(t, func() bool {
			_, exists := readState(t).users[hostIP(host)]
			return !exists
		}, func() string { return "host " + host.CYOANetworkIP + " must leave no user behind" })
	}
	for _, step := range []struct {
		name                                  string
		host                                  *devnet.Client
		passUsers                             uint16
		feedSeatUsers                         uint8
		privatePublishers, privateSubscribers uint32
	}{
		{"disconnect_host_a", hostA, 3, 3, 2, 2},
		{"disconnect_host_d", hostD, 2, 2, 1, 2},
		{"disconnect_host_e_leaves_the_pass_wide_count_alone", hostE, 2, 2, 1, 1},
		{"disconnect_host_b", hostB, 1, 1, 1, 0},
		{"disconnect_host_c", hostC, 0, 0, 0, 0},
	} {
		if !t.Run(step.name, func(t *testing.T) {
			disconnect(t, step.host)
			requireCounts(t, step.passUsers, step.feedSeatUsers, step.privatePublishers, step.privateSubscribers)
		}) {
			t.FailNow()
		}
	}

	t.Run("the_admin_closes_the_pass", func(t *testing.T) {
		require.Equal(t, [2]uint32{0, 0}, readState(t).groups["comped-e2e"])
		passPubkey := solana.PublicKey(readState(t).passPubkey)
		manager(t, "doublezero access-pass close --pubkey "+passPubkey.String())
		waitUntil(t, func() bool {
			return readState(t).passPubkey == [32]byte{}
		}, func() string { return "the access pass " + passPubkey.String() + " must be closed" })
	})
}
