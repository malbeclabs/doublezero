//go:build qa

package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQA_MulticastConnectivity(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
	require.NoError(t, err, "failed to create test")

	// Generate multicast group code or use the given one.
	code := *useGroupFlag
	if code == "" {
		code = test.RandomMulticastGroupCode()
		log.Info("No multicast group code specified, using generated code", "code", code)
	}

	// Find publisher client.
	var publisher *qa.Client
	if *forcePublisherFlag != "" {
		publisher = test.GetClient(*forcePublisherFlag)
	} else {
		publisher = test.RandomClient()
	}
	require.NotNil(t, publisher, "failed to find publisher client for host %s", *forcePublisherFlag)

	// Build list of subscribers.
	subscribers := make([]*qa.Client, 0, len(test.Clients())-1)
	for _, client := range test.Clients() {
		if client.Host == publisher.Host {
			continue
		}
		subscribers = append(subscribers, client)
	}

	// Create multicast group and delete it on cleanup.
	group, err := publisher.CreateMulticastGroup(ctx, code, "10Gbps")
	require.NoError(t, err, "failed to create multicast group")
	if *useGroupFlag == "" {
		t.Cleanup(func() {
			err := publisher.DeleteMulticastGroup(context.Background(), group.PK)
			assert.NoError(t, err, "failed to delete multicast group")
		})
	}

	// Add publisher to multicast group allowlist.
	err = publisher.AddPublisherToMulticastGroupAllowlist(ctx, code, group.OwnerPK, publisher.PublicIP().String())
	require.NoError(t, err, "failed to add publisher to multicast group allowlist")

	// Add subscribers to multicast group allowlist.
	for _, subscriber := range subscribers {
		err = subscriber.AddSubscriberToMulticastGroupAllowlist(ctx, code, group.OwnerPK, subscriber.PublicIP().String())
		require.NoError(t, err, "failed to add subscriber to multicast group allowlist")
	}

	// Connect publisher to multicast group.
	err = publisher.ConnectUserMulticast_Publisher_Wait(ctx, code)
	require.NoError(t, err, "failed to connect publisher to multicast group")

	// Disconnect source client on cleanup.
	t.Cleanup(func() {
		err := publisher.DisconnectUser(context.Background(), true, true)
		assert.NoError(t, err, "failed to disconnect user")
	})

	// Connect subscribers to multicast group.
	for _, subscriber := range subscribers {
		err = subscriber.ConnectUserMulticast_Subscriber_Wait(ctx, code)
		require.NoError(t, err, "failed to connect subscriber to multicast group")
	}

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

	// Wait for status of all clients to be up.
	for _, client := range test.Clients() {
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
			log := newTestLogger(t)
			subscriber.SetLogger(log)
			subCtx := t.Context()

			report, err := subscriber.WaitForMulticastReport(subCtx, group)
			require.NoError(t, err, "failed to get multicast report for group %s", group.Code)
			require.NotNil(t, report, "multicast report not found for group %s", group.Code)
			log.Info("Got multicast report", "subscriber", subscriber.Host, "group", group.Code, "report", report)
		})
	}

	// Leave multicast group.
	err = publisher.MulticastLeave(ctx, code)
	require.NoError(t, err, "failed to leave multicast group")
}
