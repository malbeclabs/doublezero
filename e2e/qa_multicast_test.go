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
	multicastGroupFlag     = flag.String("multicast-group", "", "multicast group code to use for tests (optional)")
)

func TestQA_MulticastConnectivity(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
	require.NoError(t, err, "failed to create test")
	clients := test.Clients()

	// Generate multicast group code or use the given one.
	groupCode := *multicastGroupFlag
	if groupCode == "" {
		groupCode = test.RandomMulticastGroupCode()
		log.Info("No multicast group code specified, using generated code", "code", groupCode)
	} else {
		log.Info("Using given multicast group", "code", groupCode)
	}

	// Find publisher client.
	var publisher *qa.Client
	if *multicastPublisherFlag != "" {
		publisher = test.GetClient(*multicastPublisherFlag)
		require.NotNil(t, publisher, "failed to find publisher client for host %s", *multicastPublisherFlag)
	} else {
		publisher = test.RandomClient()
	}
	log.Info("Determined publisher", "host", publisher.Host)

	// Build list of subscribers.
	subscribers := qa.MapFilter(clients, func(client *qa.Client) (*qa.Client, bool) {
		if client.Host == publisher.Host {
			return nil, false
		}
		return client, true
	})
	log.Info("Determined subscribers", "count", len(subscribers), "hosts", strings.Join(qa.Map(subscribers, func(c *qa.Client) string { return c.Host }), ", "))

	var group *qa.MulticastGroup
	if *multicastGroupFlag == "" {
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
			log.Info("Got multicast report", "subscriber", subscriber.Host, "group", group.Code, "report", report)
		})
	}

	// Leave multicast group.
	err = publisher.MulticastLeave(ctx, group.Code)
	require.NoError(t, err, "failed to leave multicast group")
}
