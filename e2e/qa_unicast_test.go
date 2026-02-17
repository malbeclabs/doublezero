//go:build qa

package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQA_UnicastConnectivity(t *testing.T) {
	if *multiTunnelFlag {
		t.Skip("Skipping: use TestQA_MultiTunnel in multi-tunnel mode")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig, nil)
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

	validateUnicastConnectivity(t, ctx, log, clients)
}

// validateUnicastConnectivity verifies unicast routes and ping connectivity
// between all client pairs. Clients must already be connected with status up.
func validateUnicastConnectivity(t *testing.T, ctx context.Context, log *slog.Logger, clients []*qa.Client) {
	t.Helper()

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

				_, err := srcClient.TestUnicastConnectivity(t, subCtx, dstClient, nil, nil)
				if err != nil {
					log.Error("Connectivity test failed", "error", err, "source", srcClient.Host, "target", dstClient.Host)
					require.NoError(t, err, "failed to test connectivity")
				}
			})
		}
	}
}
