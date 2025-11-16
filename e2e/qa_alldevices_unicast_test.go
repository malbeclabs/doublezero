//go:build qa

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

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

	// Filter devices to only include those with sufficient capacity and skip test devices.
	devices := test.ValidDevices(2)
	if len(devices) == 0 {
		t.Skip("No valid devices found with sufficient capacity")
	}
	deviceCodes := qa.Map(devices, func(d *qa.Device) string { return d.Code })
	log.Info("Planning to test", "devices", strings.Join(deviceCodes, ","), "device_count", len(devices), "client_count", len(clients))

	// Batch size is number of clients, but we never reuse devices within a batch.
	batchSize := min(len(clients), len(devices))

	// Random source used to shuffle clients so we don't always test the same client-device pairs.
	rs := rand.New(rand.NewSource(time.Now().UnixNano()))

	for start := 0; start < len(devices); start += batchSize {
		end := min(start+batchSize, len(devices))
		batch := devices[start:end]

		// Take all clients, shuffle, then take the first len(batch)
		shuffled := make([]*qa.Client, len(clients))
		copy(shuffled, clients)
		rs.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		activeClients := shuffled[:len(batch)]

		t.Run(fmt.Sprintf("batch_%d", (start/batchSize)+1), func(t *testing.T) {
			log.Info("Testing batch", "devices", strings.Join(qa.Map(batch, func(d *qa.Device) string { return d.Code }), ","))

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
				log.Info("Connecting client to device", "client", c.Host, "device", d.Code)
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
					errCh := make(chan error, len(activeClients)-1)

					for _, dst := range activeClients {
						if dst.Host == srcClient.Host {
							continue
						}
						src := srcClient
						target := dst

						wg.Add(1)
						go func() {
							defer wg.Done()
							if e := src.TestUnicastConnectivity(subCtx, target); e != nil {
								errCh <- fmt.Errorf("connectivity %s -> %s on device %s: %w", src.Host, target.Host, device.Code, e)
								return
							}
							log.Info("Connectivity test passed", "device", device.Code, "source", src.Host, "target", target.Host)
						}()
					}

					wg.Wait()
					close(errCh)

					for e := range errCh {
						require.NoError(t, e)
					}
				})
			}
		})
	}
}
