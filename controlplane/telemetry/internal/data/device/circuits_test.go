package data_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Device_Provider_GetCircuits(t *testing.T) {
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
		contributor := serviceability.Contributor{
			Code:   "C1",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		link := serviceability.Link{
			Code:              "L1",
			SideAPubKey:       devA.PubKey,
			SideZPubKey:       devB.PubKey,
			ContributorPubKey: contributor.PubKey,
		}

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Devices:      []serviceability.Device{devA, devB},
					Links:        []serviceability.Link{link},
					Contributors: []serviceability.Contributor{contributor},
				}, nil
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			CircuitsCacheTTL: 1 * time.Minute,
		})
		require.NoError(t, err)

		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		require.Len(t, circuits, 2)

		circuitABCode := circuitKey(devA.Code, devB.Code, link.PubKey)
		circuitBACode := circuitKey(devB.Code, devA.Code, link.PubKey)

		expected := map[string]struct{}{
			circuitABCode: {},
			circuitBACode: {},
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
		contributor := serviceability.Contributor{
			Code:   "C1",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		link := serviceability.Link{
			Code:              "L2",
			SideAPubKey:       devA.PubKey,
			SideZPubKey:       toPubKeyBytes(solana.NewWallet().PublicKey()), // Missing device
			ContributorPubKey: contributor.PubKey,
		}
		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Devices:      []serviceability.Device{devA},
					Links:        []serviceability.Link{link},
					Contributors: []serviceability.Contributor{contributor},
				}, nil
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			CircuitsCacheTTL: 1 * time.Minute,
		})
		require.NoError(t, err)
		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		assert.Empty(t, circuits)
	})

	t.Run("load failure returns error", func(t *testing.T) {
		t.Parallel()

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return nil, errors.New("load failed")
			},
		}
		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			CircuitsCacheTTL: 1 * time.Minute,
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
		contributor := serviceability.Contributor{
			Code:   "C1",
			PubKey: toPubKeyBytes(solana.NewWallet().PublicKey()),
		}
		link := serviceability.Link{
			Code:              "L1",
			SideAPubKey:       devA.PubKey,
			SideZPubKey:       devB.PubKey,
			ContributorPubKey: contributor.PubKey,
		}

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				if called > 0 {
					return nil, errors.New("GetProgramData should not be called more than once")
				}
				called++
				return &serviceability.ProgramData{
					Devices:      []serviceability.Device{devA, devB},
					Links:        []serviceability.Link{link},
					Contributors: []serviceability.Contributor{contributor},
				}, nil
			},
		}

		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			CircuitsCacheTTL: 1 * time.Minute,
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

		provider, err := data.NewProvider(&data.ProviderConfig{
			Logger: logger,
			ServiceabilityClient: &mockServiceabilityClient{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						Devices: []serviceability.Device{
							{PubKey: toPubKeyBytes(solana.NewWallet().PublicKey())},
						},
					}, nil
				},
			},
			TelemetryClient: &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			CircuitsCacheTTL: 0, // Disable cache so every call invokes Load()
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
