package qa

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/mr-tron/base58"
)

type Test struct {
	log            *slog.Logger
	clients        map[string]*Client
	serviceability *serviceability.Client
	devices        map[string]*Device
	rand           *rand.Rand
}

func NewTest(ctx context.Context, log *slog.Logger, hosts []string, port int, networkConfig *config.NetworkConfig, allocateAddrHosts map[string]struct{}) (*Test, error) {
	serviceabilityClient := serviceability.New(rpc.New(networkConfig.LedgerPublicRPCURL), networkConfig.ServiceabilityProgramID)

	devices, err := getDevices(context.Background(), serviceabilityClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %v", err)
	}

	clients := make(map[string]*Client)
	for _, host := range hosts {
		_, allocateAddr := allocateAddrHosts[host]
		client, err := NewClient(ctx, log, host, port, networkConfig, devices, allocateAddr)
		if err != nil {
			return nil, err
		}
		clients[host] = client
	}

	rand := rand.New(rand.NewSource(time.Now().UnixNano()))

	return &Test{
		log:            log,
		clients:        clients,
		serviceability: serviceabilityClient,
		devices:        devices,
		rand:           rand,
	}, nil
}

func (t *Test) Clients() []*Client {
	clients := make([]*Client, 0, len(t.clients))
	for _, client := range t.clients {
		clients = append(clients, client)
	}
	return clients
}

func (t *Test) RandomClient() *Client {
	clients := make([]*Client, 0, len(t.clients))
	for _, client := range t.clients {
		clients = append(clients, client)
	}
	return clients[t.rand.Intn(len(clients))]
}

func (t *Test) RandomMulticastGroupCode() string {
	suffix := t.rand.Intn(1000000)
	return fmt.Sprintf("qa-test-group-%06d", suffix)
}

// CleanupStaleMulticastState cleans up stale qa-test-group-* multicast groups
// and associated users left over from previous interrupted test runs.
// It disconnects all test clients first (with waitForDeletion) so the activator
// can delete onchain users while the groups still exist, then deletes the groups.
func (t *Test) CleanupStaleMulticastState(ctx context.Context) error {
	data, err := getProgramDataWithRetry(ctx, t.serviceability)
	if err != nil {
		return fmt.Errorf("failed to get program data: %w", err)
	}

	// Check if there are any stale qa-test-group-* groups.
	hasStaleGroups := false
	for _, group := range data.MulticastGroups {
		if strings.HasPrefix(group.Code, "qa-test-group-") {
			hasStaleGroups = true
			break
		}
	}
	if !hasStaleGroups {
		return nil
	}

	// Disconnect all test clients first and wait for onchain user deletion.
	// This must happen before deleting groups, otherwise orphaned users that
	// reference deleted groups may become impossible to clean up.
	for _, client := range t.clients {
		t.log.Info("Disconnecting client before stale group cleanup", "host", client.Host)
		if err := client.DisconnectUser(ctx, true, true); err != nil {
			t.log.Warn("Failed to disconnect client during cleanup", "host", client.Host, "error", err)
		}
	}

	// Get any client to perform the delete operations.
	var deleteClient *Client
	for _, c := range t.clients {
		deleteClient = c
		break
	}

	// Now delete the stale groups.
	for _, group := range data.MulticastGroups {
		if !strings.HasPrefix(group.Code, "qa-test-group-") {
			continue
		}
		pk := solana.PublicKeyFromBytes(group.PubKey[:])
		t.log.Info("Cleaning up stale multicast group", "code", group.Code, "pubkey", pk)
		if err := deleteClient.DeleteMulticastGroup(ctx, pk); err != nil {
			t.log.Warn("Failed to cleanup stale multicast group", "code", group.Code, "error", err)
		}
	}
	return nil
}

func (t *Test) Devices() map[string]*Device {
	return t.devices
}

// ValidDevices returns devices that pass filtering criteria.
// If skipCapacityCheck is true (e.g., when using a QA identity that bypasses on-chain capacity checks),
// devices are not filtered by available capacity.
func (t *Test) ValidDevices(minCapacity int, skipCapacityCheck bool) []*Device {
	devices := make([]*Device, 0, len(t.devices))

	for _, device := range t.Devices() {
		// Skip devices with "test" in the code as these are typically not real hardware
		if strings.Contains(strings.ToLower(device.Code), "test") {
			t.log.Debug("Skipping test device", "device", device.Code)
			continue
		}

		// Skip capacity check if using QA identity (bypasses on-chain max_users check)
		if !skipCapacityCheck {
			// Check if device has capacity for at least minCapacity users
			availableSlots := device.MaxUsers - device.UsersCount
			if availableSlots < minCapacity {
				t.log.Info("Skipping device with insufficient capacity", "device", device.Code, "users", device.UsersCount, "maxUsers", device.MaxUsers)
				continue
			}
		}
		devices = append(devices, device)
	}

	// Sort devices by code for consistent ordering
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Code < devices[j].Code
	})

	return devices
}

func (t *Test) ShuffleDevices(devices []*Device) {
	t.rand.Shuffle(len(devices), func(i, j int) {
		devices[i], devices[j] = devices[j], devices[i]
	})
}

func (c *Test) Close() error {
	for _, client := range c.clients {
		err := client.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Test) GetClient(host string) *Client {
	return c.clients[host]
}

func getDevices(ctx context.Context, serviceabilityClient *serviceability.Client) (map[string]*Device, error) {
	devices := make(map[string]*Device)
	data, err := getProgramDataWithRetry(ctx, serviceabilityClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %v", err)
	}
	exchanges := make(map[[32]uint8]string)
	for _, e := range data.Exchanges {
		exchanges[e.PubKey] = e.Code
	}
	for _, device := range data.Devices {
		exchangeCode := exchanges[device.ExchangePubKey]
		devices[device.Code] = &Device{
			PubKey:       base58.Encode(device.PubKey[:]),
			Code:         device.Code,
			ExchangeCode: exchangeCode,
			MaxUsers:     int(device.MaxUsers),
			UsersCount:   int(device.UsersCount),
			Status:       device.Status,
			DeviceType:   device.DeviceType,
		}
	}
	return devices, nil
}

func getProgramDataWithRetry(ctx context.Context, serviceabilityClient *serviceability.Client) (*serviceability.ProgramData, error) {
	var result *serviceability.ProgramData

	operation := func() error {
		data, err := serviceabilityClient.GetProgramData(ctx)
		if err != nil {
			return err
		}
		result = data
		return nil
	}

	exp := backoff.NewExponentialBackOff()
	retryPolicy := backoff.WithMaxRetries(exp, 5)
	retryPolicy = backoff.WithContext(retryPolicy, ctx)

	if err := backoff.Retry(operation, retryPolicy); err != nil {
		return nil, fmt.Errorf("failed to get program data after retries: %v", err)
	}

	return result, nil
}
