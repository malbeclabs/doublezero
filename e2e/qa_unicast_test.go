//go:build qa

package e2e

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
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
	clients := test.Clients()

	// Disconnect all clients on cleanup.
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

	// Connect users to any device without waiting for status.
	// NOTE: We need to do this sequentially to avoid a DZ ledger race condition.
	for _, client := range clients {
		err := client.ConnectUserUnicast_AnyDevice_NoWait(ctx)
		require.NoError(t, err, "failed to connect user")
	}

	// Wait for status of all users to be up.
	for _, client := range clients {
		err := client.WaitForStatusUp(ctx)
		require.NoError(t, err, "failed to wait for status")
	}

	// Wait for routes to be installed on each host.
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

	// Test connectivity between all clients.
	var testsWithPartialLosses atomic.Uint32
	for _, srcClient := range clients {
		for _, dstClient := range clients {
			if srcClient.Host == dstClient.Host {
				continue
			}

			t.Run(fmt.Sprintf("connectivity_%s_to_%s", srcClient.Host, dstClient.Host), func(t *testing.T) {
				t.Parallel()

				outerLog := log
				srcClient.SetLogger(newTestLogger(t))
				t.Cleanup(func() {
					srcClient.SetLogger(outerLog)
				})
				subCtx := t.Context()

				result, err := srcClient.TestUnicastConnectivity(subCtx, dstClient)
				require.NoError(t, err, "failed to test connectivity")

				if result.PacketsReceived < result.PacketsSent {
					testsWithPartialLosses.Add(1)
				}
			})
		}
	}

	// Tolerate at most one test with partial losses.
	// TestUnicastConnectivity will return error if there are losses that exceed the acceptable
	// threshold, resulting in the QA test to fail earlier than this check. This check is responsible
	// for tolerating at most 1 test with "acceptable" partial loss, or else fail the QA test.
	require.LessOrEqual(t, testsWithPartialLosses.Load(), uint32(1), "too many connectivity tests with partial packet loss")
}
