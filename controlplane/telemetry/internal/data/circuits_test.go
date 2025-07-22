package data_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
)

func TestTelemetry_Data_Provider_GetCircuits(t *testing.T) {
	t.Parallel()

	t.Run("basic forward and reverse circuits", func(t *testing.T) {
		t.Parallel()

		devA := serviceability.Device{
			Code:   "A",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		devB := serviceability.Device{
			Code:   "B",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		link := serviceability.Link{
			Code:        "L1",
			SideAPubKey: devA.PubKey,
			SideZPubKey: devB.PubKey,
		}

		client := &mockServiceabilityClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{devA, devB}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{link}
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			CircuitsCacheTTL:     1 * time.Minute,
		})
		require.NoError(t, err)

		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		require.Len(t, circuits, 2)

		expected := map[string]struct{}{
			"A → B (L1)": {},
			"B → A (L1)": {},
		}
		for _, c := range circuits {
			_, ok := expected[c.Code]
			assert.True(t, ok, "unexpected circuit %s", c.Code)
		}
	})

	t.Run("device missing skips link", func(t *testing.T) {
		t.Parallel()

		devA := serviceability.Device{
			Code:   "A",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		link := serviceability.Link{
			Code:        "L2",
			SideAPubKey: devA.PubKey,
			SideZPubKey: toPubKeyBytes(solana.NewWallet().PublicKey()), // Missing device
		}
		client := &mockServiceabilityClient{
			LoadFunc: func(ctx context.Context) error {
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{devA}
			},
			GetLinksFunc: func() []serviceability.Link {
				return []serviceability.Link{link}
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			CircuitsCacheTTL:     1 * time.Minute,
		})
		require.NoError(t, err)
		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		assert.Empty(t, circuits)
	})

	t.Run("load failure returns error", func(t *testing.T) {
		t.Parallel()

		client := &mockServiceabilityClient{
			LoadFunc: func(ctx context.Context) error {
				return errors.New("load failed")
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			CircuitsCacheTTL:     1 * time.Minute,
		})
		require.NoError(t, err)
		_, err = provider.GetCircuits(t.Context())
		assert.ErrorContains(t, err, "load failed")
	})

	t.Run("cache hit returns early", func(t *testing.T) {
		t.Parallel()

		var called int
		walletA := solana.NewWallet()
		walletB := solana.NewWallet()

		devA := serviceability.Device{
			Code:   "A",
			PubKey: toPubKeyBytes(walletA.PublicKey()),
		}
		devB := serviceability.Device{
			Code:   "B",
			PubKey: toPubKeyBytes(walletB.PublicKey()),
		}
		link := serviceability.Link{
			Code:        "L1",
			SideAPubKey: devA.PubKey,
			SideZPubKey: devB.PubKey,
		}

		client := &mockServiceabilityClient{
			LoadFunc: func(ctx context.Context) error {
				if called > 0 {
					return errors.New("Load should not be called more than once")
				}
				called++
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				if called > 1 {
					require.Fail(t, "GetDevices called after cache populated")
				}
				return []serviceability.Device{devA, devB}
			},
			GetLinksFunc: func() []serviceability.Link {
				if called > 1 {
					require.Fail(t, "GetLinks called after cache populated")
				}
				return []serviceability.Link{link}
			},
		}

		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			CircuitsCacheTTL:     1 * time.Minute,
		})
		require.NoError(t, err)

		first, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		require.NotEmpty(t, first)

		second, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		assert.Equal(t, first, second)
	})

	t.Run("concurrent GetCircuits triggers race without lock", func(t *testing.T) {
		t.Parallel()

		racey := &raceyMockClient{
			devices: make(map[string]serviceability.Device),
		}

		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: racey,
			TelemetryClient:      &mockTelemetryClient{},
			CircuitsCacheTTL:     0, // Disable cache so every call invokes Load()
		})
		require.NoError(t, err)

		const concurrency = 10
		var wg sync.WaitGroup
		start := make(chan struct{})

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _ = provider.GetCircuits(context.Background())
			}()
		}

		// Give all goroutines time to reach the start line
		time.Sleep(100 * time.Millisecond)
		close(start)
		wg.Wait()
	})
}

func toPubKeyBytes(pk solana.PublicKey) [32]byte {
	var arr [32]byte
	copy(arr[:], pk.Bytes())
	return arr
}

type raceyMockClient struct {
	devices map[string]serviceability.Device
}

func (m *raceyMockClient) Load(ctx context.Context) error {
	for i := range 10 {
		m.devices[fmt.Sprintf("dev-%d", i)] = serviceability.Device{
			Code:   fmt.Sprintf("dev-%d", i),
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
	}
	return nil
}

func (m *raceyMockClient) GetDevices() []serviceability.Device {
	devs := make([]serviceability.Device, 0, len(m.devices))
	for _, d := range m.devices {
		devs = append(devs, d)
	}
	return devs
}

func (m *raceyMockClient) GetLinks() []serviceability.Link {
	devs := m.GetDevices()
	if len(devs) < 2 {
		return nil
	}
	return []serviceability.Link{
		{
			Code:        "L1",
			SideAPubKey: devs[0].PubKey,
			SideZPubKey: devs[1].PubKey,
		},
	}
}
