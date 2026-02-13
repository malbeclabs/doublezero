//go:build qa

package e2e

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQA_MultiTunnel validates simultaneous unicast + multicast tunnel coexistence.
// It connects unicast first, runs unicast tests, then adds a multicast tunnel on top
// without disconnecting unicast, runs multicast tests, and finally disconnects everything.
//
// This test only runs when -multi-tunnel is passed. When not set, the existing
// TestQA_UnicastConnectivity and TestQA_MulticastConnectivity run independently.
func TestQA_MultiTunnel(t *testing.T) {
	if !*multiTunnelFlag {
		t.Skip("Skipping: requires -multi-tunnel flag")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	// Disconnect all clients on cleanup (handles both tunnels).
	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, client := range clients {
			wg.Add(1)
			go func(client *qa.Client) {
				defer wg.Done()
				_ = client.DisconnectUser(context.Background(), true, true)
			}(client)
		}
		wg.Wait()
	})

	// Parse multicast groups once at function scope.
	providedGroups := parseMulticastGroups()

	// Multicast role assignments, populated in the setup phase.
	var publisher *qa.Client
	var subscribers []*qa.Client
	var group *qa.MulticastGroup

	// --- PHASE 1: Unicast connect ---
	// Connect users to any device without waiting for status.
	// Sequential to avoid DZ ledger race condition (same as existing test).
	if !t.Run("unicast_connect", func(t *testing.T) {
		for _, client := range clients {
			err := client.ConnectUserUnicast_AnyDevice_NoWait(ctx)
			require.NoError(t, err, "failed to connect user unicast")
		}
		for _, client := range clients {
			err := client.WaitForStatusUp(ctx)
			require.NoError(t, err, "failed to wait for unicast status up")
		}
	}) {
		t.Fatal("unicast_connect phase failed, aborting")
	}

	// --- PHASE 2: Unicast route verification ---
	if !t.Run("unicast_routes", func(t *testing.T) {
		for _, c := range clients {
			device, err := c.GetCurrentDevice(ctx)
			require.NoError(t, err, "failed to get current device for client %s", c.Host)
			err = c.WaitForRoutes(ctx, qa.MapFilter(clients, func(other *qa.Client) (net.IP, bool) {
				otherDevice, err := other.GetCurrentDevice(ctx)
				if err != nil {
					return nil, false
				}
				if other.Host == c.Host || otherDevice.ExchangeCode == device.ExchangeCode {
					return nil, false
				}
				return other.PublicIP(), true
			}))
			require.NoError(t, err, "failed to wait for routes")
		}
	}) {
		t.Fatal("unicast_routes phase failed, aborting")
	}

	// --- PHASE 3: Unicast connectivity tests ---
	// Run ping tests between all client pairs (same logic as TestQA_UnicastConnectivity).
	if !t.Run("unicast_connectivity", func(t *testing.T) {
		for _, srcClient := range clients {
			for _, dstClient := range clients {
				if srcClient.Host == dstClient.Host {
					continue
				}
				t.Run(fmt.Sprintf("%s_to_%s", srcClient.Host, dstClient.Host), func(t *testing.T) {
					t.Parallel()
					_, err := srcClient.TestUnicastConnectivity(t, t.Context(), dstClient, nil, nil)
					require.NoError(t, err, "unicast connectivity failed")
				})
			}
		}
	}) {
		t.Fatal("unicast_connectivity phase failed, aborting")
	}

	// --- PHASE 4: Multicast setup ---
	// Cleanup stale test groups, create/get group, set up allowlists.
	if !t.Run("multicast_setup", func(t *testing.T) {
		deleted, err := clients[0].CleanupStaleTestGroups(ctx, clients)
		require.NoError(t, err, "failed to cleanup stale test groups")
		if deleted > 0 {
			log.Info("Cleaned up stale test groups", "count", deleted)
		}

		var groupCode string
		if len(providedGroups) > 0 {
			groupCode = providedGroups[0]
		} else {
			groupCode = test.RandomMulticastGroupCode()
		}

		// Determine publisher.
		if *multicastPublisherFlag != "" {
			publisher = test.GetClient(*multicastPublisherFlag)
			require.NotNil(t, publisher, "publisher host not found: %s", *multicastPublisherFlag)
		} else {
			publisher = test.RandomClient()
		}

		// Build subscriber list.
		subscribers = qa.MapFilter(clients, func(client *qa.Client) (*qa.Client, bool) {
			if client.Host == publisher.Host {
				return nil, false
			}
			return client, true
		})

		if len(providedGroups) == 0 {
			group, err = publisher.CreateMulticastGroup(ctx, groupCode, "10Gbps")
			require.NoError(t, err, "failed to create multicast group")

			err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, publisher.PublicIP().String())
			require.NoError(t, err, "failed to add publisher to multicast group allowlist")
			for _, sub := range subscribers {
				err = sub.AddSubscriberToMulticastGroupAllowlist(ctx, group.Code, group.OwnerPK, sub.PublicIP().String())
				require.NoError(t, err, "failed to add subscriber to multicast group allowlist")
			}
		} else {
			group, err = publisher.GetMulticastGroup(ctx, groupCode)
			require.NoError(t, err, "failed to get multicast group")
			require.NotNil(t, group, "multicast group not found: %s", groupCode)
		}
	}) {
		t.Fatal("multicast_setup phase failed, aborting")
	}

	// Register cleanup: delete group if we created it.
	if len(providedGroups) == 0 && group != nil {
		t.Cleanup(func() {
			err := publisher.DeleteMulticastGroup(context.Background(), group.PK)
			assert.NoError(t, err, "failed to delete multicast group")
		})
	}

	// Dump diagnostics on failure.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, c := range clients {
			c.DumpDiagnostics([]*qa.MulticastGroup{group})
		}
	})

	// --- PHASE 5: Multicast connect (ADD tunnel, no disconnect) ---
	if !t.Run("multicast_connect", func(t *testing.T) {
		// Publisher adds multicast tunnel on top of existing unicast.
		err := publisher.ConnectUserMulticast_Publisher_AddTunnel(ctx, group.Code)
		require.NoError(t, err, "failed to add multicast publisher tunnel")

		// Subscribers add multicast tunnel on top of existing unicast.
		for _, sub := range subscribers {
			err := sub.ConnectUserMulticast_Subscriber_AddTunnel(ctx, group.Code)
			require.NoError(t, err, "failed to add multicast subscriber tunnel")
		}

		// Wait for ALL tunnel statuses (unicast + multicast) to be up on all clients.
		for _, client := range clients {
			err := client.WaitForAllStatusesUp(ctx, 2)
			require.NoError(t, err, "failed to wait for all statuses up")
		}

		// Verify the unicast (IBRL) tunnel is still healthy after adding multicast.
		for _, client := range clients {
			statuses, err := client.GetUserStatuses(ctx)
			require.NoError(t, err, "failed to get user statuses on host %s", client.Host)
			ibrlUp := false
			for _, s := range statuses {
				if strings.HasPrefix(s.UserType, "IBRL") && qa.IsStatusUp(s.SessionStatus) {
					ibrlUp = true
					break
				}
			}
			require.True(t, ibrlUp, "IBRL tunnel is not up on host %s after adding multicast", client.Host)
		}
	}) {
		t.Fatal("multicast_connect phase failed, aborting")
	}

	// --- PHASE 6: Multicast connectivity tests ---
	t.Run("multicast_connectivity", func(t *testing.T) {
		// Join all subscribers to the multicast group.
		for _, sub := range subscribers {
			err := sub.MulticastJoin(ctx, group)
			require.NoError(t, err, "failed to join multicast group")
		}

		// Send multicast data from publisher in background.
		go func() {
			_ = publisher.MulticastSend(ctx, group, 120*time.Second)
		}()

		// Verify each subscriber receives packets (parallel to avoid missing the send window).
		for _, sub := range subscribers {
			t.Run(fmt.Sprintf("subscriber_%s", sub.Host), func(t *testing.T) {
				t.Parallel()
				report, err := sub.WaitForMulticastReport(t.Context(), group)
				require.NoError(t, err, "failed to get multicast report")
				require.NotNil(t, report)
				t.Logf("Received multicast packets: subscriber=%s group=%s packetCount=%d",
					sub.Host, group.Code, report.PacketCount)
			})
		}
	})

	// Leave multicast group after all parallel subscriber subtests have completed.
	// t.Run blocks until its parallel children finish, so these calls are safe.
	// Use assert (not require) so all participants attempt to leave even if one fails.
	assert.NoError(t, publisher.MulticastLeave(ctx, group.Code),
		"failed to leave multicast group (publisher)")
	for _, sub := range subscribers {
		assert.NoError(t, sub.MulticastLeave(ctx, group.Code),
			"failed to leave multicast group (subscriber %s)", sub.Host)
	}
}
