//go:build qa

package e2e

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQA_UnicastConnectivity(t *testing.T) {
	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
	require.NoError(t, err, "failed to create test")

	// Disconnect all clients on cleanup.
	t.Cleanup(func() {
		var wg sync.WaitGroup
		for _, client := range test.Clients() {
			wg.Add(1)
			go func(client *qa.Client) {
				defer wg.Done()
				err := client.DisconnectUser(context.Background(), true, true)
				assert.NoError(t, err, "failed to disconnect user")
			}(client)
		}
		wg.Wait()
	})

	// Connect users to any device without waiting for status.
	// NOTE: We need to do this sequentially to avoid a DZ ledger race condition.
	for _, client := range test.Clients() {
		err := client.ConnectUserUnicast_AnyDevice_NoWait(ctx)
		require.NoError(t, err, "failed to connect user")
	}

	// Wait for status of all users to be up.
	for _, client := range test.Clients() {
		err := client.WaitForStatusUp(ctx)
		require.NoError(t, err, "failed to wait for status")
	}

	// Wait for routes to be installed on each host.
	for _, client := range test.Clients() {
		expectedRoutes := make([]net.IP, 0, len(test.Clients())-1)
		for _, otherClient := range test.Clients() {
			if client.Host == otherClient.Host {
				continue
			}
			expectedRoutes = append(expectedRoutes, otherClient.PublicIP())
		}
		err := client.WaitForRoutes(ctx, expectedRoutes)
		require.NoError(t, err, "failed to wait for routes")
	}

	// Test connectivity between all clients.
	for _, srcClient := range test.Clients() {
		for _, dstClient := range test.Clients() {
			if srcClient.Host == dstClient.Host {
				continue
			}

			t.Run(fmt.Sprintf("connectivity_%s_to_%s", srcClient.Host, dstClient.Host), func(t *testing.T) {
				t.Parallel()

				srcClient.SetLogger(newTestLogger(t))
				srcCtx := t.Context()

				err := srcClient.TestUnicastConnectivity(srcCtx, dstClient)
				require.NoError(t, err, "failed to test connectivity")
			})
		}
	}
}
