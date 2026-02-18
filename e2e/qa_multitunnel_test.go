//go:build qa

package e2e

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQA_MultiTunnel(t *testing.T) {
	if !*multiTunnelFlag {
		t.Skip("Skipping: requires -multi-tunnel flag")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()
	providedGroups := parseMulticastGroups()

	// Shared state populated by setup subtests.
	var publisher *qa.Client
	var subscribers []*qa.Client
	var groupA, groupB *qa.MulticastGroup

	// --- PHASE 1: Unicast connect ---
	t.Run("unicast_connect", func(t *testing.T) {
		log := newTestLogger(t)
		log.Info("Connecting all clients unicast")

		// Connect sequentially to avoid DZ ledger race condition (CreateUser
		// transactions write to the shared device account).
		for _, client := range clients {
			err := client.ConnectUserUnicast_AnyDevice_NoWait(ctx)
			require.NoError(t, err, "failed to connect user unicast")
		}

		for _, client := range clients {
			err := client.WaitForStatusUp(ctx)
			require.NoError(t, err, "failed to wait for unicast status up")
		}
	})
	if t.Failed() {
		return
	}

	// --- PHASE 2: Multicast setup ---
	t.Run("multicast_setup", func(t *testing.T) {
		log := newTestLogger(t)
		log.Info("Setting up multicast groups")

		// Cleanup stale test groups from previous runs.
		deleted, err := clients[0].CleanupStaleTestGroups(ctx, clients)
		require.NoError(t, err, "failed to cleanup stale test groups")
		if deleted > 0 {
			log.Info("Cleaned up stale test groups", "count", deleted)
		}

		// Generate multicast group codes or use the given ones.
		var groupCodeA, groupCodeB string
		if len(providedGroups) >= 2 {
			groupCodeA = providedGroups[0]
			groupCodeB = providedGroups[1]
			log.Debug("Using provided multicast groups", "groupA", groupCodeA, "groupB", groupCodeB)
		} else {
			groupCodeA = test.RandomMulticastGroupCode()
			groupCodeB = test.RandomMulticastGroupCode()
			log.Debug("No multicast group codes specified, using generated codes", "groupA", groupCodeA, "groupB", groupCodeB)
		}

		// Determine publisher.
		if *multicastPublisherFlag != "" {
			publisher = test.GetClient(*multicastPublisherFlag)
			require.NotNil(t, publisher, "failed to find publisher client for host %s", *multicastPublisherFlag)
		} else {
			publisher = test.RandomClient()
		}
		log.Debug("Determined publisher", "host", publisher.Host)

		// Build subscriber list (all non-publisher clients).
		subscribers = qa.MapFilter(clients, func(client *qa.Client) (*qa.Client, bool) {
			if client.Host == publisher.Host {
				return nil, false
			}
			return client, true
		})
		log.Debug("Determined subscribers", "count", len(subscribers), "hosts", strings.Join(qa.Map(subscribers, func(c *qa.Client) string { return c.Host }), ", "))

		if len(providedGroups) >= 2 {
			// Get existing multicast groups.
			groupA, err = publisher.GetMulticastGroup(ctx, groupCodeA)
			require.NoError(t, err, "failed to get multicast group A")
			require.NotNil(t, groupA, "multicast group not found: %s", groupCodeA)
			groupB, err = publisher.GetMulticastGroup(ctx, groupCodeB)
			require.NoError(t, err, "failed to get multicast group B")
			require.NotNil(t, groupB, "multicast group not found: %s", groupCodeB)
		} else {
			// Create multicast groups.
			groupA, err = publisher.CreateMulticastGroup(ctx, groupCodeA, "10Gbps")
			require.NoError(t, err, "failed to create multicast group A")
			groupB, err = publisher.CreateMulticastGroup(ctx, groupCodeB, "10Gbps")
			require.NoError(t, err, "failed to create multicast group B")

			// Add publisher to allowlists for both groups.
			err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, publisher.PublicIP().String())
			require.NoError(t, err, "failed to add publisher to allowlist for group A")
			err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, publisher.PublicIP().String())
			require.NoError(t, err, "failed to add publisher to allowlist for group B")

			// Add subscribers to allowlists for both groups.
			for _, subscriber := range subscribers {
				err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, groupA.Code, groupA.OwnerPK, subscriber.PublicIP().String())
				require.NoError(t, err, "failed to add subscriber to allowlist for group A")
				err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, groupB.Code, groupB.OwnerPK, subscriber.PublicIP().String())
				require.NoError(t, err, "failed to add subscriber to allowlist for group B")
			}
		}
	})
	if t.Failed() {
		return
	}

	// Cleanup registrations (LIFO order -- register in reverse of desired execution):
	// 1. Register diagnostics dump (runs FIRST on failure)
	// 2. Register disconnect all clients (runs SECOND)
	// 3. Register delete multicast group (runs THIRD -- after clients disconnected)
	//
	// NOTE: disconnect BEFORE group deletion -- group deletion fails
	// if it has active publishers/subscribers.

	// 3. Delete multicast groups (runs after clients disconnected).
	if len(providedGroups) < 2 {
		t.Cleanup(func() {
			err := publisher.DeleteMulticastGroup(context.Background(), groupA.PK)
			assert.NoError(t, err, "failed to delete multicast group A")
			err = publisher.DeleteMulticastGroup(context.Background(), groupB.PK)
			assert.NoError(t, err, "failed to delete multicast group B")
		})
	}

	// 2. Disconnect all clients.
	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, client := range clients {
			wg.Add(1)
			go func(client *qa.Client) {
				defer wg.Done()
				err := client.DisconnectUser(context.Background(), true, true)
				assert.NoError(t, err, "failed to disconnect user")
			}(client)
		}
		wg.Wait()
	})

	// 1. Dump diagnostics on failure (runs first).
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, client := range clients {
			client.DumpDiagnostics([]*qa.MulticastGroup{groupA, groupB})
		}
	})

	// --- PHASE 3: Add multicast tunnel (no disconnect) ---
	t.Run("add_multicast_tunnel", func(t *testing.T) {
		log := newTestLogger(t)
		log.Info("Adding multicast tunnels without disconnecting unicast")

		// Connect sequentially to avoid DZ ledger race condition (CreateUser
		// transactions write to the shared device account).
		err := publisher.ConnectUserMulticast_Publisher_AddTunnel(ctx, groupA.Code, groupB.Code)
		require.NoError(t, err, "failed to add publisher multicast tunnel")
		for _, client := range clients {
			if client.Host == publisher.Host {
				continue
			}
			err = client.ConnectUserMulticast_Subscriber_AddTunnel(ctx, groupA.Code, groupB.Code)
			require.NoError(t, err, "failed to add subscriber multicast tunnel")
		}

		for _, client := range clients {
			err = client.WaitForAllStatusesUp(ctx, 2)
			require.NoError(t, err, "failed to wait for all statuses up")
		}
	})
	if t.Failed() {
		return
	}

	// --- PHASE 4: Verify IBRL still healthy ---
	t.Run("verify_ibrl_healthy", func(t *testing.T) {
		log := newTestLogger(t)
		log.Info("Verifying IBRL tunnels still healthy")

		for _, client := range clients {
			statuses, err := client.GetUserStatuses(ctx)
			require.NoError(t, err)
			ibrl := qa.FindIBRLStatus(statuses)
			require.NotNil(t, ibrl, "no IBRL status on host %s", client.Host)
			require.True(t, qa.IsStatusUp(ibrl.SessionStatus),
				"IBRL not up on host %s after adding multicast", client.Host)
		}
	})
	if t.Failed() {
		return
	}

	// --- PHASE 5: Run validations as subtests ---
	log.Info("Running connectivity validations")

	t.Run("unicast_connectivity", func(t *testing.T) {
		validateUnicastConnectivity(t, ctx, newTestLogger(t), clients)
	})
	t.Run("multicast_groupA", func(t *testing.T) {
		validateMulticastConnectivity(t, ctx, newTestLogger(t), publisher, subscribers, groupA)
	})
	t.Run("multicast_groupB", func(t *testing.T) {
		validateMulticastConnectivity(t, ctx, newTestLogger(t), publisher, subscribers, groupB)
	})
}
