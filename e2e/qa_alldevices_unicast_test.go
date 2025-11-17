//go:build qa

package e2e

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/malbeclabs/doublezero/e2e/internal/qa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQA_AllDevices_UnicastConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-devices test in short mode")
	}

	log := newTestLogger(t)
	ctx := t.Context()
	test, err := qa.NewTest(ctx, log, hostsArg, portArg, networkConfig)
	require.NoError(t, err, "failed to create test")

	clients := test.Clients()
	require.GreaterOrEqual(t, len(clients), 2, "At least 2 clients are required for connectivity testing")

	// Filter devices to only include those with sufficient capacity and skip test devices, and
	// shuffle them to avoid always testing connectivity via the same devices.
	devices := test.ShuffledValidDevices(2)
	if len(devices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
	}

	// Batch size is number of clients, but we never reuse devices within a batch.
	batchSize := min(len(clients), len(devices))

	deviceCodes := qa.Map(devices, func(d *qa.Device) string { return d.Code })
	log.Info("Planning to test",
		"devices", strings.Join(deviceCodes, ","),
		"deviceCount", len(devices),
		"clientCount", len(clients),
		"batchSize", batchSize,
		"totalBatches", len(devices)/batchSize,
	)

	for start := 0; start < len(devices); start += batchSize {
		end := min(start+batchSize, len(devices))
		batch := devices[start:end]

		activeClients := clients[:len(batch)]

		batchNumber := (start / batchSize) + 1
		t.Run(fmt.Sprintf("batch_%d", batchNumber), func(t *testing.T) {
			log.Info("Testing batch", "batch", batchNumber, "devices", strings.Join(qa.Map(batch, func(d *qa.Device) string { return d.Code }), ","))

			// Build 1:1 assignments: client -> device, and inverse device -> client.
			clientToDevice := make(map[*qa.Client]*qa.Device, len(batch))
			deviceToClient := make(map[*qa.Device]*qa.Client, len(batch))
			for i, c := range activeClients {
				d := batch[i]
				clientToDevice[c] = d
				deviceToClient[d] = c
			}

			// Disconnect clients on cleanup.
			t.Cleanup(func() {
				var wg sync.WaitGroup
				for _, client := range activeClients {
					wg.Add(1)
					go func(client *qa.Client) {
						defer wg.Done()
						err := client.DisconnectUser(context.Background(), true, true)
						assert.NoError(t, err, "failed to disconnect user")
					}(client)
				}
				wg.Wait()
			})

			// Connect clients to their assigned devices sequentially
			// to avoid ledger race conditions.
			for _, c := range activeClients {
				d := clientToDevice[c]
				err := c.ConnectUserUnicast_NoWait(ctx, d.Code)
				require.NoError(t, err, "failed to connect user %s to device %s", c.Host, d.Code)
			}

			// Wait for clients to be up.
			for _, c := range activeClients {
				err := c.WaitForStatusUp(ctx)
				require.NoError(t, err, "failed to wait for status for client %s", c.Host)
			}

			// Wait for routes between clients.
			for _, c := range activeClients {
				err := c.WaitForRoutes(ctx, qa.MapFilter(activeClients, func(other *qa.Client) (net.IP, bool) {
					if other.Host == c.Host || clientToDevice[other].ExchangeCode == clientToDevice[c].ExchangeCode {
						return nil, false
					}
					return other.PublicIP(), true
				}))
				require.NoError(t, err, "failed to wait for routes on client %s", c.Host)
			}

			// Now run per-device subtests for this batch.
			// Each subtest:
			//   - Uses the client assigned to that device as the source
			//   - Tests connectivity from that client to all other clients
			for _, device := range batch {
				srcClient := deviceToClient[device]
				require.NotNil(t, srcClient, "no client assigned to device %s in batch", device.Code)

				t.Run(fmt.Sprintf("device_%s__from_%s", device.Code, srcClient.Host), func(t *testing.T) {
					t.Parallel()

					outerLog := log
					log := newTestLogger(t)
					srcClient.SetLogger(log)
					t.Cleanup(func() {
						srcClient.SetLogger(outerLog)
					})
					subCtx := t.Context()

					var wg sync.WaitGroup
					for _, target := range activeClients {
						if target.Host == srcClient.Host {
							continue
						}

						wg.Add(1)
						go func(src, target *qa.Client) {
							defer wg.Done()
							err := src.TestUnicastConnectivity(subCtx, target)
							require.NoError(t, err)
						}(srcClient, target)
					}
					wg.Wait()
				})
			}
		})
	}
}
