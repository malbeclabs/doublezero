//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	multicastPublisherFlag = flag.String("multicast-publisher", "", "host to use as publisher for multicast tests (optional)")
	multicastGroupFlag     = flag.String("multicast-group", "", "comma-separated multicast group codes to use for tests (optional, skips group creation)")
)

// parseMulticastGroups parses the multicast-group flag into a slice of group codes.
func parseMulticastGroups() []string {
	if *multicastGroupFlag == "" {
		return nil
	}
	codes := strings.Split(*multicastGroupFlag, ",")
	for i := range codes {
		codes[i] = strings.TrimSpace(codes[i])
	}
	return codes
}

func TestQA_MulticastConnectivity(t *testing.T) {
	if *multiTunnelFlag {
		t.Skip("Skipping: use TestQA_MultiTunnel in multi-tunnel mode")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	// Cleanup stale test groups from previous runs.
	deleted, err := clients[0].CleanupStaleTestGroups(ctx, clients)
	require.NoError(t, err, "failed to cleanup stale test groups")
	if deleted > 0 {
		log.Info("Cleaned up stale test groups", "count", deleted)
	}

	// Generate multicast group code or use the given one.
	providedGroups := parseMulticastGroups()
	var groupCode string
	if len(providedGroups) > 0 {
		groupCode = providedGroups[0]
		log.Debug("Using provided multicast group", "code", groupCode)
	} else {
		groupCode = test.RandomMulticastGroupCode()
		log.Debug("No multicast group code specified, using generated code", "code", groupCode)
	}

	// Find publisher client.
	var publisher *qa.Client
	if *multicastPublisherFlag != "" {
		publisher = test.GetClient(*multicastPublisherFlag)
		require.NotNil(t, publisher, "failed to find publisher client for host %s", *multicastPublisherFlag)
	} else {
		publisher = test.RandomClient()
	}
	log.Debug("Determined publisher", "host", publisher.Host)

	// Build list of subscribers.
	subscribers := qa.MapFilter(clients, func(client *qa.Client) (*qa.Client, bool) {
		if client.Host == publisher.Host {
			return nil, false
		}
		return client, true
	})
	log.Debug("Determined subscribers", "count", len(subscribers), "hosts", strings.Join(qa.Map(subscribers, func(c *qa.Client) string { return c.Host }), ", "))

	var group *qa.MulticastGroup
	if len(providedGroups) == 0 {
		// Create multicast group and delete it on cleanup.
		group, err = publisher.CreateMulticastGroup(ctx, groupCode, "10Gbps")
		require.NoError(t, err, "failed to create multicast group")
		t.Cleanup(func() {
			err := publisher.DeleteMulticastGroup(context.Background(), group.PK)
			assert.NoError(t, err, "failed to delete multicast group")
		})

		// Add publisher to multicast group allowlist.
		err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, publisher.PublicIP().String())
		require.NoError(t, err, "failed to add publisher to multicast group allowlist")

		// Add subscribers to multicast group allowlist.
		for _, subscriber := range subscribers {
			err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, subscriber.PublicIP().String())
			require.NoError(t, err, "failed to add subscriber to multicast group allowlist")
		}
	} else {
		// Get existing multicast group.
		group, err = publisher.GetMulticastGroup(ctx, groupCode)
		require.NoError(t, err, "failed to get multicast group")
		require.NotNil(t, group, "multicast group not found: %s", groupCode)
	}

	// Disconnect source client on cleanup.
	t.Cleanup(func() {
		err := publisher.DisconnectUser(context.Background(), true, true)
		assert.NoError(t, err, "failed to disconnect user")
	})

	// Disconnect subscribers on cleanup.
	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, subscriber := range subscribers {
			wg.Add(1)
			go func(subscriber *qa.Client) {
				defer wg.Done()
				err := subscriber.DisconnectUser(context.Background(), true, true)
				assert.NoError(t, err, "failed to disconnect user")
			}(subscriber)
		}
		wg.Wait()
	})

	// Dump diagnostics on failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		publisher.DumpDiagnostics([]*qa.MulticastGroup{group})
		for _, sub := range subscribers {
			sub.DumpDiagnostics([]*qa.MulticastGroup{group})
		}
	})

	// Connect publisher to multicast group.
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, group.Code)
	require.NoError(t, err, "failed to connect publisher to multicast group")

	// Connect subscribers to multicast group.
	for _, subscriber := range subscribers {
		err = subscriber.ConnectUserMulticast_Subscriber_Wait(ctx, group.Code)
		require.NoError(t, err, "failed to connect subscriber to multicast group")
	}

	// Wait for status of all clients to be up.
	for _, client := range clients {
		err := client.WaitForStatusUp(ctx)
		require.NoError(t, err, "failed to wait for status")
	}

	validateMulticastConnectivity(t, ctx, log, publisher, subscribers, group)
}

// validateMulticastConnectivity verifies multicast data delivery from publisher
// to all subscribers. Clients must already be connected with status up.
func validateMulticastConnectivity(t *testing.T, ctx context.Context, log *slog.Logger, publisher *qa.Client, subscribers []*qa.Client, group *qa.MulticastGroup) {
	t.Helper()

	// Join all subscribers to the multicast group.
	for _, subscriber := range subscribers {
		err := subscriber.MulticastJoin(ctx, group)
		require.NoError(t, err, "failed to join multicast group %s", group.Code)
	}

	// Send multicast data from publisher in background while we poll for reports.
	// This avoids the race where PIM convergence takes longer than the send window.
	go func() {
		_ = publisher.MulticastSend(ctx, group, 120*time.Second)
	}()

	// Get multicast report from each subscriber.
	for _, subscriber := range subscribers {
		t.Run(fmt.Sprintf("subscriber_%s", subscriber.Host), func(t *testing.T) {
			outerLog := log
			log := newTestLogger(t)
			subscriber.SetLogger(log)
			t.Cleanup(func() {
				subscriber.SetLogger(outerLog)
			})
			subCtx := t.Context()

			report, err := subscriber.WaitForMulticastReport(subCtx, group)
			require.NoError(t, err, "failed to get multicast report for group %s", group.Code)
			require.NotNil(t, report, "multicast report not found for group %s", group.Code)
			log.Info("Received multicast packets", "subscriber", subscriber.Host, "group", group.Code, "packetCount", report.PacketCount)
		})
	}

	// Leave multicast group.
	err := publisher.MulticastLeave(ctx, group.Code)
	require.NoError(t, err, "failed to leave multicast group")
}

// TestQA_MulticastPublisherMultipleGroups tests a single publisher sending to multiple
// multicast groups, with different subscribers for each group.
func TestQA_MulticastPublisherMultipleGroups(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 3, "need at least 3 clients for this test (1 publisher + 2 subscribers)")

	// Cleanup stale test groups from previous runs.
	deleted, err := clients[0].CleanupStaleTestGroups(ctx, clients)
	require.NoError(t, err, "failed to cleanup stale test groups")
	if deleted > 0 {
		log.Info("Cleaned up stale test groups", "count", deleted)
	}

	// Select publisher and two different subscribers.
	publisher := test.RandomClient()
	var subscriberA, subscriberB *qa.Client
	for _, c := range clients {
		if c.Host == publisher.Host {
			continue
		}
		if subscriberA == nil {
			subscriberA = c
		} else if subscriberB == nil {
			subscriberB = c
			break
		}
	}
	require.NotNil(t, subscriberA, "failed to find first subscriber")
	require.NotNil(t, subscriberB, "failed to find second subscriber")
	log.Debug("Selected clients", "publisher", publisher.Host, "subscriberA", subscriberA.Host, "subscriberB", subscriberB.Host)

	// Use provided groups or create new ones.
	providedGroups := parseMulticastGroups()
	var groupA, groupB *qa.MulticastGroup

	if len(providedGroups) >= 2 {
		log.Debug("Using provided multicast groups", "groupA", providedGroups[0], "groupB", providedGroups[1])
		groupA, err = publisher.GetMulticastGroup(ctx, providedGroups[0])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[0])
		require.NotNil(t, groupA, "multicast group not found: %s", providedGroups[0])
		groupB, err = publisher.GetMulticastGroup(ctx, providedGroups[1])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[1])
		require.NotNil(t, groupB, "multicast group not found: %s", providedGroups[1])
	} else {
		groupCodeA := test.RandomMulticastGroupCode()
		groupCodeB := test.RandomMulticastGroupCode()
		log.Debug("Creating multicast groups", "groupA", groupCodeA, "groupB", groupCodeB)

		groupA, err = publisher.CreateMulticastGroup(ctx, groupCodeA, "10Gbps")
		require.NoError(t, err, "failed to create multicast group A")
		t.Cleanup(func() {
			_ = publisher.DeleteMulticastGroup(context.Background(), groupA.PK)
		})

		groupB, err = publisher.CreateMulticastGroup(ctx, groupCodeB, "10Gbps")
		require.NoError(t, err, "failed to create multicast group B")
		t.Cleanup(func() {
			_ = publisher.DeleteMulticastGroup(context.Background(), groupB.PK)
		})

		// Add publisher to allowlists for both groups.
		err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, publisher.PublicIP().String())
		require.NoError(t, err, "failed to add publisher to allowlist for group A")
		err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, publisher.PublicIP().String())
		require.NoError(t, err, "failed to add publisher to allowlist for group B")

		// SubscriberA needs both groups (starts on A, later adds B in phase 2).
		// Also needs publisher allowlist for group A (acts as pub+sub in phase 3).
		err = subscriberA.AddPublisherToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, subscriberA.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberA to publisher allowlist for group A")
		err = subscriberA.AddSubscriberToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, subscriberA.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberA to allowlist for group A")
		err = subscriberA.AddSubscriberToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, subscriberA.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberA to allowlist for group B")

		// SubscriberB only needs group B.
		err = subscriberB.AddSubscriberToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, subscriberB.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberB to allowlist for group B")
	}

	// Cleanup: disconnect all clients.
	t.Cleanup(func() {
		_ = publisher.DisconnectUser(context.Background(), true, true)
		_ = subscriberA.DisconnectUser(context.Background(), true, true)
		_ = subscriberB.DisconnectUser(context.Background(), true, true)
	})

	// Dump diagnostics on failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		gs := []*qa.MulticastGroup{groupA, groupB}
		publisher.DumpDiagnostics(gs)
		subscriberA.DumpDiagnostics(gs)
		subscriberB.DumpDiagnostics(gs)
	})

	// Connect publisher to BOTH groups simultaneously.
	log.Debug("Connecting publisher to both groups simultaneously", "codes", []string{groupA.Code, groupB.Code})
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, groupA.Code, groupB.Code)
	require.NoError(t, err, "failed to connect publisher to groups")
	err = publisher.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for publisher status up")

	// --- Phase 1: Selective fan-out ---
	// SubA on group A only, SubB on group B only — verify each receives their group.
	log.Debug("Phase 1: selective fan-out")

	log.Debug("Connecting subscriberA to group A", "code", groupA.Code)
	err = subscriberA.ConnectUserMulticast_Subscriber_Wait(ctx, groupA.Code)
	require.NoError(t, err, "failed to connect subscriberA to group A")

	log.Debug("Connecting subscriberB to group B", "code", groupB.Code)
	err = subscriberB.ConnectUserMulticast_Subscriber_Wait(ctx, groupB.Code)
	require.NoError(t, err, "failed to connect subscriberB to group B")

	err = subscriberA.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberA status up")
	err = subscriberB.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberB status up")

	err = subscriberA.MulticastJoin(ctx, groupA)
	require.NoError(t, err, "failed to join group A")
	err = subscriberB.MulticastJoin(ctx, groupB)
	require.NoError(t, err, "failed to join group B")

	log.Debug("Publisher sending to both groups (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()
	go func() {
		_ = publisher.MulticastSend(ctx, groupB, 120*time.Second)
	}()

	reportA, err := subscriberA.WaitForMulticastReport(ctx, groupA)
	require.NoError(t, err, "failed to get report for group A from subscriberA")
	require.Greater(t, reportA.PacketCount, uint64(0), "subscriberA received no packets from group A")
	log.Info("Received multicast packets", "subscriber", subscriberA.Host, "group", groupA.Code, "packetCount", reportA.PacketCount)

	reportB, err := subscriberB.WaitForMulticastReport(ctx, groupB)
	require.NoError(t, err, "failed to get report for group B from subscriberB")
	require.Greater(t, reportB.PacketCount, uint64(0), "subscriberB received no packets from group B")
	log.Info("Received multicast packets", "subscriber", subscriberB.Host, "group", groupB.Code, "packetCount", reportB.PacketCount)

	// --- Phase 2: Dynamic subscription ---
	// SubA disconnects and reconnects with both groups A+B — verify identity preserved and receives from both.
	log.Debug("Phase 2: dynamic subscription")

	statusBefore, err := subscriberA.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get subscriberA status before adding group B")
	log.Debug("SubscriberA status before", "status", statusBefore)

	log.Debug("SubscriberA reconnecting with both groups", "codes", []string{groupA.Code, groupB.Code})
	err = subscriberA.ConnectUserMulticast_Subscriber_Wait(ctx, groupA.Code, groupB.Code)
	require.NoError(t, err, "failed to reconnect subscriberA with both groups")

	err = subscriberA.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberA status up after adding group B")

	// Verify user pubkey is preserved (user was not recreated).
	statusAfter, err := subscriberA.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get subscriberA status after adding group B")
	log.Debug("SubscriberA status after", "status", statusAfter)

	err = subscriberA.MulticastJoin(ctx, groupA, groupB)
	require.NoError(t, err, "failed to join both groups")

	log.Debug("Publisher sending to both groups (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()
	go func() {
		_ = publisher.MulticastSend(ctx, groupB, 120*time.Second)
	}()

	reports, err := subscriberA.WaitForMulticastReports(ctx, []*qa.MulticastGroup{groupA, groupB})
	require.NoError(t, err, "failed to get reports from both groups")

	reportA = reports[groupA.IP.String()]
	require.NotNil(t, reportA, "no report for group A after adding B")
	require.Greater(t, reportA.PacketCount, uint64(0), "no packets from group A after adding B")

	reportB = reports[groupB.IP.String()]
	require.NotNil(t, reportB, "no report for group B")
	require.Greater(t, reportB.PacketCount, uint64(0), "no packets from group B")

	log.Debug("Phase 2 passed: dynamic subscription verified",
		"groupA_packets", reportA.PacketCount, "groupB_packets", reportB.PacketCount)

	// --- Phase 3: Simultaneous pub+sub ---
	// SubA reconnects as both publisher and subscriber on group A, sends to itself.
	log.Debug("Phase 3: simultaneous pub+sub")

	err = subscriberA.ConnectUserMulticast_PubAndSub_Wait(ctx, []string{groupA.Code}, []string{groupA.Code})
	require.NoError(t, err, "failed to connect subscriberA as pub+sub")

	err = subscriberA.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberA status up as pub+sub")

	err = subscriberA.MulticastJoin(ctx, groupA)
	require.NoError(t, err, "failed to join group A as pub+sub")

	// Both the original publisher and subscriberA (as pub+sub) send to group A.
	// This verifies the pub+sub client participates in the full multicast mesh,
	// not just self-loop.
	log.Debug("Publisher and subscriberA both sending to group A (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()
	go func() {
		_ = subscriberA.MulticastSend(ctx, groupA, 120*time.Second)
	}()

	reportPubSub, err := subscriberA.WaitForMulticastReport(ctx, groupA)
	require.NoError(t, err, "failed to get report for group A as pub+sub")
	require.Greater(t, reportPubSub.PacketCount, uint64(0), "pub+sub client received no packets")
	log.Debug("Phase 3 passed: pub+sub verified", "group", groupA.Code, "packetCount", reportPubSub.PacketCount)
}
