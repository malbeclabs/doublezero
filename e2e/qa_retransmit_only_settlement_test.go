//go:build qa

package e2e

import (
	"context"
	"flag"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/e2e/internal/qa"
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
	retransmitGroupCodesFlag  = flag.String("retransmit-group-codes", "", "comma-separated multicast group codes a seat in a retransmit-only metro must subscribe to, and nothing else")
	retransmitPriceFlag       = flag.Uint64("retransmit-price", 10, "expected discounted retransmit-only seat price in whole USDC dollars")
)

// TestQA_RetransmitOnlySettlement demonstrates the retransmit-only shred
// subscription end to end: a client pays for a seat on a device in a
// retransmit-only metro, is charged the discounted price, and ends up
// subscribed to the retransmit groups only, so leader shreds are excluded. It
// mirrors TestQA_MulticastSettlement over the shared runShredSettlement flow,
// adding retransmit-only device selection, the discounted-price assertion, and
// the subscribed-groups assertion. The test is environment-agnostic: the group
// codes and the expected price are flags, so the same binary validates the
// testnet QA network and later mainnet.
func TestQA_RetransmitOnlySettlement(t *testing.T) {
	runShredSettlement(t, shredSettlementParams{
		enabled:    *enableRetransmitOnlyTests,
		skipReason: "Skipping: --enable-retransmit-only-settlement-tests flag not set",

		selectSubtestName: "select_retransmit_only_device",
		selectDevice:      selectRetransmitOnlyDevice,

		priceLogMsg: "Found discounted epoch price",
		assertPrice: func(t *testing.T, device *qa.Device, price uint64) {
			// The retransmit-only metro is priced at the discount, so the seat
			// price must equal the expected retransmit price (default 10 USDC).
			// This is the price the program charges, read from chain, not the CLI
			// quote — the quote is checked against chain separately.
			require.Equal(t, *retransmitPriceFlag, price,
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
	require.NotEmpty(t, *retransmitGroupCodesFlag, "--retransmit-group-codes is required")
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
// it holds the retransmit groups and nothing else, so the leader group, the
// root-node group and any non-retransmit per-metro group all fall away. On
// timeout it logs the seat's last-seen subscription state so on-call can tell an
// oracle bug (a retransmit group never appeared) from a leader-exclusion bug (a
// group leaked in) without re-deriving it from a rerun.
func assertSubscribedGroups(t *testing.T, ctx context.Context, log *slog.Logger, client *qa.Client, _ *qa.Device) {
	// Neither onchain data nor the SDK labels a group as leader or retransmit,
	// so the operator supplies the codes per network.
	required := make(map[solana.PublicKey]string)
	for _, code := range strings.Split(*retransmitGroupCodesFlag, ",") {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		group, err := client.GetMulticastGroup(ctx, code)
		require.NoError(t, err, "failed to resolve multicast group %q", code)
		require.NotNil(t, group, "multicast group %q not found onchain", code)
		required[group.PK] = code
	}
	require.NotEmpty(t, required, "no multicast group resolved from --retransmit-group-codes %q", *retransmitGroupCodesFlag)

	// The oracle converges the seat's onchain subscription asynchronously, so
	// poll until it reflects retransmit-only membership. Capture the last-seen
	// state on every poll so the timeout branch can report it.
	var (
		lastSubscribed []string
		lastMissing    []string
		lastExtra      []string
	)
	ok := assert.Eventually(t, func() bool {
		user, err := client.GetServiceabilityUser(ctx)
		if err != nil {
			log.Info("serviceability user poll error", "error", err)
			return false
		}
		subscribed := make(map[solana.PublicKey]bool, len(user.Subscribers))
		subs := make([]string, 0, len(user.Subscribers))
		for _, sub := range user.Subscribers {
			group := solana.PublicKeyFromBytes(sub[:])
			subscribed[group] = true
			subs = append(subs, group.String())
		}
		var missing, extra []string
		for group, code := range required {
			if !subscribed[group] {
				missing = append(missing, code)
			}
		}
		for group := range subscribed {
			if _, want := required[group]; !want {
				extra = append(extra, group.String())
			}
		}
		lastSubscribed, lastMissing, lastExtra = subs, missing, extra
		return len(missing) == 0 && len(extra) == 0
	}, retransmitSubscribeTimeout, 5*time.Second)
	if !ok {
		log.Warn("seat did not converge to retransmit-only subscription within timeout",
			"missing_groups", lastMissing,
			"extra_groups", lastExtra,
			"subscribed_groups", lastSubscribed,
		)
	}
	require.True(t, ok,
		"the seat should subscribe to %s and to nothing else; it is missing %v and wrongly subscribes to %v",
		*retransmitGroupCodesFlag, lastMissing, lastExtra)
	log.Info("Subscribed to the retransmit groups only",
		"retransmit_groups", *retransmitGroupCodesFlag,
		"subscriber_count", len(lastSubscribed),
	)
}
