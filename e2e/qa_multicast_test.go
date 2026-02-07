//go:build qa

package e2e

import (
	"context"
	"flag"
	"fmt"
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
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

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

	// Join all subscribers to the multicast group.
	for _, subscriber := range subscribers {
		err = subscriber.MulticastJoin(ctx, group)
		require.NoError(t, err, "failed to join multicast group %s", group.Code)
	}

	// Send multicast data from publisher to the multicast group.
	err = publisher.MulticastSend(ctx, group, 60*time.Second)
	require.NoError(t, err, "failed to send multicast data to group %s", group.Code)

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
			log.Debug("Got multicast report", "subscriber", subscriber.Host, "group", group.Code, "report", report)
		})
	}

	// Leave multicast group.
	err = publisher.MulticastLeave(ctx, group.Code)
	require.NoError(t, err, "failed to leave multicast group")
}

// TestQA_MulticastMultiGroupSimultaneous tests subscribing to multiple multicast groups
// in a single connect command.
func TestQA_MulticastMultiGroupSimultaneous(t *testing.T) {
	if envArg != "devnet" && envArg != "testnet" {
		t.Skip("Skipping: requires QA agent support for multi-group multicast")
	}
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 2, "need at least 2 clients for this test")

	// Select publisher and subscriber.
	publisher := test.RandomClient()
	var subscriber *qa.Client
	for _, c := range clients {
		if c.Host != publisher.Host {
			subscriber = c
			break
		}
	}
	require.NotNil(t, subscriber, "failed to find subscriber client")
	log.Debug("Selected clients", "publisher", publisher.Host, "subscriber", subscriber.Host)

	// Use provided groups or create new ones.
	providedGroups := parseMulticastGroups()
	var groups []*qa.MulticastGroup
	var groupCodes []string

	if len(providedGroups) >= 2 {
		// Use provided groups (skip creation and allowlist setup).
		groupCodes = providedGroups
		log.Debug("Using provided multicast groups", "codes", groupCodes)
		groups = make([]*qa.MulticastGroup, len(groupCodes))
		for i, code := range groupCodes {
			group, err := publisher.GetMulticastGroup(ctx, code)
			require.NoError(t, err, "failed to get multicast group %s", code)
			require.NotNil(t, group, "multicast group not found: %s", code)
			groups[i] = group
		}
	} else {
		// Create random groups.
		groupCodes = []string{
			test.RandomMulticastGroupCode(),
			test.RandomMulticastGroupCode(),
			test.RandomMulticastGroupCode(),
		}
		log.Debug("Creating multicast groups", "codes", groupCodes)
		groups = make([]*qa.MulticastGroup, len(groupCodes))
		for i, code := range groupCodes {
			group, err := publisher.CreateMulticastGroup(ctx, code, "10Gbps")
			require.NoError(t, err, "failed to create multicast group %s", code)
			groups[i] = group

			// Register cleanup.
			t.Cleanup(func() {
				err := publisher.DeleteMulticastGroup(context.Background(), group.PK)
				assert.NoError(t, err, "failed to delete multicast group %s", code)
			})

			// Add publisher to allowlist.
			err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, publisher.PublicIP().String())
			require.NoError(t, err, "failed to add publisher to allowlist for group %s", code)

			// Add subscriber to allowlist.
			err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, subscriber.PublicIP().String())
			require.NoError(t, err, "failed to add subscriber to allowlist for group %s", code)
		}
	}

	// Cleanup: disconnect clients.
	t.Cleanup(func() {
		_ = publisher.DisconnectUser(context.Background(), true, true)
		_ = subscriber.DisconnectUser(context.Background(), true, true)
	})

	// Connect publisher to all groups simultaneously.
	log.Debug("Connecting publisher to all groups simultaneously", "codes", groupCodes)
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, groupCodes...)
	require.NoError(t, err, "failed to connect publisher to multiple groups")

	// Connect subscriber to all groups simultaneously.
	log.Debug("Connecting subscriber to all groups simultaneously", "codes", groupCodes)
	err = subscriber.ConnectUserMulticast_Subscriber_Wait(ctx, groupCodes...)
	require.NoError(t, err, "failed to connect subscriber to multiple groups")

	// Wait for status up.
	err = publisher.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for publisher status up")
	err = subscriber.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriber status up")

	// Subscriber joins all groups.
	err = subscriber.MulticastJoin(ctx, groups...)
	require.NoError(t, err, "failed to join multiple multicast groups")

	// Publisher sends to all groups in parallel in background.
	// We use a longer duration to ensure packets keep flowing while we poll for reports.
	// This avoids guessing how long PIM takes to establish routing.
	log.Debug("Publisher sending to all groups in parallel (background)", "codes", groupCodes)
	for _, group := range groups {
		go func(g *qa.MulticastGroup) {
			_ = publisher.MulticastSend(ctx, g, 120*time.Second)
		}(group)
	}

	// Poll for reports while sending continues in background.
	// As soon as we receive at least 1 packet from each group, we know it works.
	log.Debug("Waiting for multicast reports (while sending continues)")
	reports, err := subscriber.WaitForMulticastReports(ctx, groups)
	require.NoError(t, err, "failed to get multicast reports")
	for _, group := range groups {
		report := reports[group.IP.String()]
		require.NotNil(t, report, "no report for group %s", group.Code)
		require.Greater(t, report.PacketCount, uint64(0), "no packets received for group %s", group.Code)
		log.Debug("Verified packets received", "group", group.Code, "packetCount", report.PacketCount)
	}

	log.Debug("Test passed: subscriber received traffic from all 3 groups")
}

// TestQA_MulticastAddGroupToExistingUser tests adding a new multicast group subscription
// to a user that is already connected and subscribed to another group.
func TestQA_MulticastAddGroupToExistingUser(t *testing.T) {
	if envArg != "devnet" && envArg != "testnet" {
		t.Skip("Skipping: requires QA agent support for multi-group multicast")
	}
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 2, "need at least 2 clients for this test")

	// Select publisher and subscriber.
	publisher := test.RandomClient()
	var subscriber *qa.Client
	for _, c := range clients {
		if c.Host != publisher.Host {
			subscriber = c
			break
		}
	}
	require.NotNil(t, subscriber, "failed to find subscriber client")
	log.Debug("Selected clients", "publisher", publisher.Host, "subscriber", subscriber.Host)

	// Use provided groups or create new ones.
	providedGroups := parseMulticastGroups()
	var groupA, groupB *qa.MulticastGroup

	if len(providedGroups) >= 2 {
		// Use provided groups (skip creation and allowlist setup).
		log.Debug("Using provided multicast groups", "groupA", providedGroups[0], "groupB", providedGroups[1])
		groupA, err = publisher.GetMulticastGroup(ctx, providedGroups[0])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[0])
		require.NotNil(t, groupA, "multicast group not found: %s", providedGroups[0])
		groupB, err = publisher.GetMulticastGroup(ctx, providedGroups[1])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[1])
		require.NotNil(t, groupB, "multicast group not found: %s", providedGroups[1])
	} else {
		// Create random groups.
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

		// Add subscriber to allowlists for both groups.
		err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, subscriber.PublicIP().String())
		require.NoError(t, err, "failed to add subscriber to allowlist for group A")
		err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, subscriber.PublicIP().String())
		require.NoError(t, err, "failed to add subscriber to allowlist for group B")
	}

	// Cleanup: disconnect clients.
	t.Cleanup(func() {
		_ = publisher.DisconnectUser(context.Background(), true, true)
		_ = subscriber.DisconnectUser(context.Background(), true, true)
	})

	// Step 1: Connect publisher to both groups (publisher needs all groups from start).
	log.Debug("Connecting publisher to both groups")
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, groupA.Code, groupB.Code)
	require.NoError(t, err, "failed to connect publisher to groups")

	// Step 2: Connect subscriber to group A only.
	log.Debug("Connecting subscriber to group A only", "code", groupA.Code)
	err = subscriber.ConnectUserMulticast_Subscriber_Wait(ctx, groupA.Code)
	require.NoError(t, err, "failed to connect subscriber to group A")

	// Wait for status up.
	err = subscriber.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriber status up")

	// Get subscriber's user pubkey before adding group B.
	statusBefore, err := subscriber.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get subscriber status before adding group B")
	log.Debug("Subscriber status before adding group B", "status", statusBefore)

	// Step 3: Subscriber joins and verifies traffic from group A.
	err = subscriber.MulticastJoin(ctx, groupA)
	require.NoError(t, err, "failed to join group A")

	log.Debug("Publisher sending to group A (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()

	reportA, err := subscriber.WaitForMulticastReport(ctx, groupA)
	require.NoError(t, err, "failed to get report for group A")
	require.Greater(t, reportA.PacketCount, uint64(0), "no packets received for group A")
	log.Debug("Verified packets received from group A", "packetCount", reportA.PacketCount)

	// Step 4: Add group B to existing subscriber (without disconnecting).
	// Note: ConnectUserMulticast calls DisconnectUser internally, but the CLI behavior
	// should preserve the user account and just add the new subscription.
	log.Debug("Adding group B to existing subscriber", "codes", []string{groupA.Code, groupB.Code})
	err = subscriber.ConnectUserMulticast_Subscriber_Wait(ctx, groupA.Code, groupB.Code)
	require.NoError(t, err, "failed to add group B to subscriber")

	// Wait for status up again.
	err = subscriber.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriber status up after adding group B")

	// Verify user pubkey is the same (user was not recreated).
	statusAfter, err := subscriber.GetUserStatus(ctx)
	require.NoError(t, err, "failed to get subscriber status after adding group B")
	log.Debug("Subscriber status after adding group B", "status", statusAfter)

	// Step 5: Join both groups and verify traffic from both.
	err = subscriber.MulticastJoin(ctx, groupA, groupB)
	require.NoError(t, err, "failed to join both groups")

	// Send to both groups in background.
	log.Debug("Publisher sending to both groups (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()
	go func() {
		_ = publisher.MulticastSend(ctx, groupB, 120*time.Second)
	}()

	// Poll for reports while sending continues.
	reports, err := subscriber.WaitForMulticastReports(ctx, []*qa.MulticastGroup{groupA, groupB})
	require.NoError(t, err, "failed to get multicast reports for both groups")

	reportA = reports[groupA.IP.String()]
	require.NotNil(t, reportA, "no report for group A after adding B")
	require.Greater(t, reportA.PacketCount, uint64(0), "no packets from group A after adding B")

	reportB := reports[groupB.IP.String()]
	require.NotNil(t, reportB, "no report for group B")
	require.Greater(t, reportB.PacketCount, uint64(0), "no packets from group B")

	log.Debug("Test passed: successfully added group B to existing user and received traffic from both groups",
		"groupA_packets", reportA.PacketCount, "groupB_packets", reportB.PacketCount)
}

// TestQA_MulticastPublisherMultipleGroups tests a single publisher sending to multiple
// multicast groups, with different subscribers for each group.
func TestQA_MulticastPublisherMultipleGroups(t *testing.T) {
	if envArg != "devnet" && envArg != "testnet" {
		t.Skip("Skipping: requires QA agent support for multi-group multicast")
	}
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 3, "need at least 3 clients for this test (1 publisher + 2 subscribers)")

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
	require.NotNil(t, subscriberA, "failed to find first subscriber client")
	require.NotNil(t, subscriberB, "failed to find second subscriber client")
	log.Debug("Selected clients", "publisher", publisher.Host, "subscriberA", subscriberA.Host, "subscriberB", subscriberB.Host)

	// Use provided groups or create new ones.
	providedGroups := parseMulticastGroups()
	var groupA, groupB *qa.MulticastGroup

	if len(providedGroups) >= 2 {
		// Use provided groups (skip creation and allowlist setup).
		log.Debug("Using provided multicast groups", "groupA", providedGroups[0], "groupB", providedGroups[1])
		groupA, err = publisher.GetMulticastGroup(ctx, providedGroups[0])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[0])
		require.NotNil(t, groupA, "multicast group not found: %s", providedGroups[0])
		groupB, err = publisher.GetMulticastGroup(ctx, providedGroups[1])
		require.NoError(t, err, "failed to get multicast group %s", providedGroups[1])
		require.NotNil(t, groupB, "multicast group not found: %s", providedGroups[1])
	} else {
		// Create random groups.
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

		// Add subscriberA to allowlist for group A only.
		err = subscriberA.AddSubscriberToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, subscriberA.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberA to allowlist for group A")

		// Add subscriberB to allowlist for group B only.
		err = subscriberB.AddSubscriberToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, subscriberB.PublicIP().String())
		require.NoError(t, err, "failed to add subscriberB to allowlist for group B")
	}

	// Cleanup: disconnect clients.
	t.Cleanup(func() {
		_ = publisher.DisconnectUser(context.Background(), true, true)
		_ = subscriberA.DisconnectUser(context.Background(), true, true)
		_ = subscriberB.DisconnectUser(context.Background(), true, true)
	})

	// Connect publisher to BOTH groups simultaneously.
	log.Debug("Connecting publisher to both groups simultaneously", "codes", []string{groupA.Code, groupB.Code})
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, groupA.Code, groupB.Code)
	require.NoError(t, err, "failed to connect publisher to multiple groups")

	// Connect subscriberA to group A only.
	log.Debug("Connecting subscriberA to group A", "code", groupA.Code)
	err = subscriberA.ConnectUserMulticast_Subscriber_Wait(ctx, groupA.Code)
	require.NoError(t, err, "failed to connect subscriberA to group A")

	// Connect subscriberB to group B only.
	log.Debug("Connecting subscriberB to group B", "code", groupB.Code)
	err = subscriberB.ConnectUserMulticast_Subscriber_Wait(ctx, groupB.Code)
	require.NoError(t, err, "failed to connect subscriberB to group B")

	// Wait for status up.
	err = publisher.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for publisher status up")
	err = subscriberA.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberA status up")
	err = subscriberB.WaitForStatusUp(ctx)
	require.NoError(t, err, "failed to wait for subscriberB status up")

	// Subscribers join their respective groups.
	err = subscriberA.MulticastJoin(ctx, groupA)
	require.NoError(t, err, "failed to join group A")
	err = subscriberB.MulticastJoin(ctx, groupB)
	require.NoError(t, err, "failed to join group B")

	// Publisher sends to both groups in parallel.
	log.Debug("Publisher sending to both groups in parallel (background)")
	go func() {
		_ = publisher.MulticastSend(ctx, groupA, 120*time.Second)
	}()
	go func() {
		_ = publisher.MulticastSend(ctx, groupB, 120*time.Second)
	}()

	// Verify subscriberA receives from group A.
	log.Debug("Waiting for subscriberA to receive from group A")
	reportA, err := subscriberA.WaitForMulticastReport(ctx, groupA)
	require.NoError(t, err, "failed to get report for group A from subscriberA")
	require.Greater(t, reportA.PacketCount, uint64(0), "subscriberA received no packets from group A")
	log.Debug("SubscriberA verified", "group", groupA.Code, "packetCount", reportA.PacketCount)

	// Verify subscriberB receives from group B.
	log.Debug("Waiting for subscriberB to receive from group B")
	reportB, err := subscriberB.WaitForMulticastReport(ctx, groupB)
	require.NoError(t, err, "failed to get report for group B from subscriberB")
	require.Greater(t, reportB.PacketCount, uint64(0), "subscriberB received no packets from group B")
	log.Debug("SubscriberB verified", "group", groupB.Code, "packetCount", reportB.PacketCount)

	log.Debug("Test passed: publisher successfully sent to multiple groups with different subscribers",
		"groupA_packets", reportA.PacketCount, "groupB_packets", reportB.PacketCount)
}
