//go:build qa

package e2e

import (
	"context"
	"flag"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	serviceability "github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retransmitSubscribeTimeout bounds how long we wait for the oracle to converge
// the seat's onchain multicast subscription after the tunnel comes up. The
// tunnel-up wait already covers the subscribe path, so this is a safety buffer
// against the oracle's reconcile cadence.
const retransmitSubscribeTimeout = 90 * time.Second

var (
	enableRetransmitOnlyTests = flag.Bool("enable-retransmit-only-settlement-tests", false, "enable the retransmit-only multicast settlement test")
	retransmitOnlyDeviceFlag  = flag.String("retransmit-only-device", "", "device code or pubkey in a retransmit-only metro (overrides auto-discovery)")
	leaderGroupCodeFlag       = flag.String("leader-group-code", "", "multicast group code for the leader (full) shred feed, expected to be EXCLUDED from the seat")
	retransmitGroupCodeFlag   = flag.String("retransmit-group-code", "", "multicast group code for the retransmit shred feed, expected to be subscribed")
	retransmitPriceFlag       = flag.Uint64("retransmit-price", 10, "expected discounted retransmit-only seat price in whole USDC dollars")
)

// TestQA_RetransmitOnlySettlement demonstrates the retransmit-only shred
// subscription end to end: a client pays for a seat on a device in a
// retransmit-only metro, is charged the discounted price, and ends up
// subscribed to the retransmit multicast group only — not the leader group, so
// leader shreds are excluded. It mirrors TestQA_MulticastSettlement over the
// shared runShredSettlement flow, adding retransmit-only device selection, the
// discounted-price assertion, and the subscribed-groups assertion. The test is
// environment-agnostic: the group codes and the expected price are flags, so
// the same binary validates the testnet QA network and later mainnet.
func TestQA_RetransmitOnlySettlement(t *testing.T) {
	runShredSettlement(t, shredSettlementParams{
		enabled:    *enableRetransmitOnlyTests,
		skipReason: "Skipping: --enable-retransmit-only-settlement-tests flag not set",

		selectSubtestName: "select_retransmit_only_device",
		selectDevice:      selectRetransmitOnlyDevice,

		priceLogMsg: "Found discounted epoch price",
		assertPrice: func(t *testing.T, device *qa.Device, epochPrice uint64) {
			// The retransmit-only metro is priced at the discount, so the seat
			// price must equal the expected retransmit price (default 10 USDC).
			require.Equal(t, *retransmitPriceFlag, epochPrice,
				"retransmit-only device %s should be priced at the discounted retransmit price", device.Code)
		},

		extraSubtestName: "assert_subscribed_groups",
		extraAssertion:   assertSubscribedGroups,
	})
}

// selectRetransmitOnlyDevice picks the device to settle against for the
// retransmit-only test: the -retransmit-only-device pin when set, otherwise the
// closest reachable device in a retransmit-only metro (auto-discovery).
//
// Auto-discovery distinguishes two outcomes: when no metro is flagged
// retransmit-only the feature is simply not deployed here, so it skips; but when
// metros are flagged and yet none of their devices is reachable, it fails
// (naming the flagged metros) rather than skipping — otherwise the one feature
// this test exists to guard would go silently unexercised on the deployed
// network.
func selectRetransmitOnlyDevice(t *testing.T, ctx context.Context, log *slog.Logger, test *qa.Test, client *qa.Client) *qa.Device {
	var device *qa.Device
	if *retransmitOnlyDeviceFlag != "" {
		pinned, ok := test.DeviceByCodeOrPubkey(*retransmitOnlyDeviceFlag)
		require.True(t, ok, "pinned device %q not found", *retransmitOnlyDeviceFlag)
		retransmitOnly, err := client.RetransmitOnlyExchangeKeys(ctx)
		require.NoError(t, err, "failed to read retransmit-only metros")
		require.True(t, retransmitOnly[pinned.ExchangePubKey],
			"pinned device %s (metro %s) is not in a retransmit-only metro", pinned.Code, pinned.ExchangeCode)
		device = pinned
	} else {
		selected, retransmitOnly, err := client.ClosestRetransmitOnlyDevice(ctx)
		require.NoError(t, err, "failed to find a retransmit-only device")
		if len(retransmitOnly) == 0 {
			t.Skip("Skipping: no metro is flagged retransmit-only (feature not deployed/configured on this network)")
		}
		require.NotNil(t, selected,
			"retransmit-only metros %v are configured but no reachable device matched; the feature under test cannot be exercised",
			flaggedMetroCodes(test, retransmitOnly))
		device = selected
	}
	log.Info("Selected retransmit-only device", "code", device.Code, "pubkey", device.PubKey, "metro", device.ExchangeCode)

	// The group codes are required to assert leader-exclusion once a device
	// exists to test. Enabling the test without them is a misconfiguration.
	require.NotEmpty(t, *leaderGroupCodeFlag, "--leader-group-code is required")
	require.NotEmpty(t, *retransmitGroupCodeFlag, "--retransmit-group-code is required")
	return device
}

// flaggedMetroCodes resolves the retransmit-only exchange pubkeys to readable
// exchange codes via the devices map, so a failure message can name the metros
// that were flagged but had no reachable device. Falls back to the raw pubkey
// for a flagged metro with no device in the map.
func flaggedMetroCodes(test *qa.Test, exchangeKeys map[string]bool) []string {
	seen := make(map[string]bool)
	codes := make([]string, 0, len(exchangeKeys))
	for _, d := range test.Devices() {
		if !exchangeKeys[d.ExchangePubKey] || seen[d.ExchangePubKey] {
			continue
		}
		seen[d.ExchangePubKey] = true
		code := d.ExchangeCode
		if code == "" {
			code = d.ExchangePubKey
		}
		codes = append(codes, code)
	}
	for key := range exchangeKeys {
		if !seen[key] {
			codes = append(codes, key)
		}
	}
	return codes
}

// assertSubscribedGroups polls the seat's onchain multicast subscription until
// it reflects retransmit-only membership: the retransmit group present and the
// leader group absent (leader shreds excluded). On timeout it logs the seat's
// last-seen subscription state so on-call can tell an oracle bug (retransmit
// group never appeared) from a leader-exclusion bug (leader group leaked in)
// without re-deriving it from a rerun.
func assertSubscribedGroups(t *testing.T, ctx context.Context, log *slog.Logger, client *qa.Client, _ *qa.Device) {
	// Resolve the leader and retransmit multicast groups by code. Neither
	// onchain data nor the SDK labels a group as leader/retransmit, so the
	// operator supplies the codes per network.
	leaderGroup, err := client.GetMulticastGroup(ctx, *leaderGroupCodeFlag)
	require.NoError(t, err, "failed to resolve leader group %q", *leaderGroupCodeFlag)
	require.NotNil(t, leaderGroup, "leader group %q not found onchain", *leaderGroupCodeFlag)
	retransmitGroup, err := client.GetMulticastGroup(ctx, *retransmitGroupCodeFlag)
	require.NoError(t, err, "failed to resolve retransmit group %q", *retransmitGroupCodeFlag)
	require.NotNil(t, retransmitGroup, "retransmit group %q not found onchain", *retransmitGroupCodeFlag)

	subscribed := func(user *serviceability.User, group solana.PublicKey) bool {
		for _, sub := range user.Subscribers {
			if solana.PublicKeyFromBytes(sub[:]).Equals(group) {
				return true
			}
		}
		return false
	}

	// The oracle converges the seat's onchain subscription asynchronously, so
	// poll until it reflects retransmit-only membership. Capture the last-seen
	// state on every poll so the timeout branch can report it.
	var (
		lastSubscribed      []string
		lastRetransmitFound bool
		lastLeaderFound     bool
	)
	ok := assert.Eventually(t, func() bool {
		user, err := client.GetServiceabilityUser(ctx)
		if err != nil {
			log.Info("serviceability user poll error", "error", err)
			return false
		}
		lastRetransmitFound = subscribed(user, retransmitGroup.PK)
		lastLeaderFound = subscribed(user, leaderGroup.PK)
		subs := make([]string, 0, len(user.Subscribers))
		for _, sub := range user.Subscribers {
			subs = append(subs, solana.PublicKeyFromBytes(sub[:]).String())
		}
		lastSubscribed = subs
		return lastRetransmitFound && !lastLeaderFound
	}, retransmitSubscribeTimeout, 5*time.Second)
	if !ok {
		log.Warn("seat did not converge to retransmit-only subscription within timeout",
			"retransmit_group", retransmitGroup.PK,
			"retransmit_present", lastRetransmitFound,
			"leader_group", leaderGroup.PK,
			"leader_present", lastLeaderFound,
			"subscribed_groups", lastSubscribed,
		)
	}
	require.True(t, ok,
		"seat should subscribe to the retransmit group and NOT the leader group")
	log.Info("Subscribed to the retransmit group only",
		"retransmit_group", retransmitGroup.PK,
		"leader_group_excluded", leaderGroup.PK,
		"subscriber_count", len(lastSubscribed),
	)
}
