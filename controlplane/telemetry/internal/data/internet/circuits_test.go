package data_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	internetdata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Data_Internet_Provider_GetCircuits(t *testing.T) {
	t.Parallel()

	t.Run("basic forward and reverse circuits", func(t *testing.T) {
		t.Parallel()

		exA := serviceability.Exchange{Code: "A", PubKey: solana.NewWallet().PublicKey()}
		exB := serviceability.Exchange{Code: "B", PubKey: solana.NewWallet().PublicKey()}
		exC := serviceability.Exchange{Code: "C", PubKey: solana.NewWallet().PublicKey()}
		exD := serviceability.Exchange{Code: "D", PubKey: solana.NewWallet().PublicKey()}

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{exA, exB, exC, exD},
				}, nil
			},
		}
		provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			AgentPK:          solana.NewWallet().PublicKey(),
			CircuitsCacheTTL: 1 * time.Minute,
		})
		require.NoError(t, err)

		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		require.Len(t, circuits, 6)

		circuitABCode := circuitKey(exA.Code, exB.Code)
		circuitACCode := circuitKey(exA.Code, exC.Code)
		circuitADCode := circuitKey(exA.Code, exD.Code)
		circuitBCCode := circuitKey(exB.Code, exC.Code)
		circuitBDCode := circuitKey(exB.Code, exD.Code)
		circuitCDCode := circuitKey(exC.Code, exD.Code)

		expected := map[string]struct{}{
			circuitABCode: {},
			circuitACCode: {},
			circuitADCode: {},
			circuitBCCode: {},
			circuitBDCode: {},
			circuitCDCode: {},
		}
		for _, c := range circuits {
			_, ok := expected[c.Code]
			assert.True(t, ok, "unexpected circuit %s", c.Code)
		}
	})

	t.Run("load failure returns error", func(t *testing.T) {
		t.Parallel()

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return nil, errors.New("load failed")
			},
		}
		provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			AgentPK:          solana.NewWallet().PublicKey(),
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

		exA := serviceability.Exchange{Code: "A", PubKey: toPubKeyBytes(walletA.PublicKey())}
		exB := serviceability.Exchange{Code: "B", PubKey: toPubKeyBytes(walletB.PublicKey())}

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				if called > 0 {
					return nil, errors.New("GetProgramData should not be called more than once")
				}
				called++
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{
						exA,
						exB,
					},
				}, nil
			},
		}

		provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			AgentPK:          solana.NewWallet().PublicKey(),
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

		provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
			Logger: logger,
			ServiceabilityClient: &mockServiceabilityClient{
				GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
					return &serviceability.ProgramData{
						Exchanges: []serviceability.Exchange{
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
			AgentPK:          solana.NewWallet().PublicKey(),
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

	t.Run("circuit code unique with duplicate link code", func(t *testing.T) {
		t.Parallel()

		exA := serviceability.Exchange{Code: "A", PubKey: toPubKeyBytes(solana.NewWallet().PublicKey())}
		exB := serviceability.Exchange{Code: "B", PubKey: toPubKeyBytes(solana.NewWallet().PublicKey())}

		client := &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return &serviceability.ProgramData{
					Exchanges: []serviceability.Exchange{exA, exB},
				}, nil
			},
		}
		provider, err := internetdata.NewProvider(&internetdata.ProviderConfig{
			Logger:               logger,
			ServiceabilityClient: client,
			TelemetryClient:      &mockTelemetryClient{},
			EpochFinder: &mockEpochFinder{
				ApproximateAtTimeFunc: func(ctx context.Context, target time.Time) (uint64, error) {
					return 1, nil
				},
			},
			AgentPK:          solana.NewWallet().PublicKey(),
			CircuitsCacheTTL: 1 * time.Minute,
		})
		require.NoError(t, err)
		circuits, err := provider.GetCircuits(t.Context())
		require.NoError(t, err)
		require.Len(t, circuits, 1)
	})
}

func toPubKeyBytes(pk solana.PublicKey) [32]byte {
	var arr [32]byte
	copy(arr[:], pk.Bytes())
	return arr
}
