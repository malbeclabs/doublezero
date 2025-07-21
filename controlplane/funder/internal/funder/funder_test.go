package funder_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/funder/internal/funder"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_Funder_New(t *testing.T) {
	t.Parallel()

	validCfg := funder.Config{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Serviceability: &mockServiceability{},
		Solana:         &mockSolana{},
		Signer:         solana.NewWallet().PrivateKey,
		MinBalanceSOL:  1,
		TopUpSOL:       1,
		Interval:       1,
	}

	t.Run("valid config", func(t *testing.T) {
		f, err := funder.New(validCfg)
		require.NoError(t, err)
		require.NotNil(t, f)
	})

	mutate := func(cfg funder.Config, f func(cfg *funder.Config)) funder.Config {
		cfgCopy := cfg
		f(&cfgCopy)
		return cfgCopy
	}

	tt := []struct {
		name    string
		cfg     funder.Config
		wantErr error
	}{
		{
			name: "missing logger",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Logger = nil
			}),
			wantErr: funder.ErrLoggerRequired,
		},
		{
			name: "missing serviceability",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Serviceability = nil
			}),
			wantErr: funder.ErrServiceabilityRequired,
		},
		{
			name: "missing solana",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Solana = nil
			}),
			wantErr: funder.ErrSolanaRequired,
		},
		{
			name: "missing signer",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Signer = nil
			}),
			wantErr: funder.ErrSignerRequired,
		},
		{
			name: "invalid empty signer",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Signer = solana.PrivateKey{}
			}),
			wantErr: funder.ErrSignerInvalid,
		},
		{
			name: "invalid signer",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				invalidBytes := make([]byte, 10)
				invalidKey := solana.PrivateKey(invalidBytes)
				cfg.Signer = invalidKey
			}),
			wantErr: funder.ErrSignerInvalid,
		},
		{
			name: "missing min balance",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.MinBalanceSOL = 0
			}),
			wantErr: funder.ErrMinBalanceRequired,
		},
		{
			name: "missing top up",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.TopUpSOL = 0
			}),
			wantErr: funder.ErrTopUpSOLRequired,
		},
		{
			name: "missing interval",
			cfg: mutate(validCfg, func(cfg *funder.Config) {
				cfg.Interval = 0
			}),
			wantErr: funder.ErrIntervalRequired,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			f, err := funder.New(tc.cfg)
			require.ErrorIs(t, err, tc.wantErr)
			require.Nil(t, f)
		})
	}
}

func TestTelemetry_Funder_Run(t *testing.T) {
	t.Parallel()

	t.Run("top up when device balance is below min", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tracker := newTracker("funder-balance", "device-balance", "transfer")

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{{MetricsPublisherPubKey: devicePK}}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		var (
			transferAmount uint64
			mu             sync.Mutex
		)

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, _ solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				switch pubkey {
				case signer.PublicKey():
					tracker.mark("funder-balance")
					return &solanarpc.GetBalanceResult{Value: 10 * solana.LAMPORTS_PER_SOL}, nil
				case devicePK:
					tracker.mark("device-balance")
					return &solanarpc.GetBalanceResult{Value: uint64(0.1 * float64(solana.LAMPORTS_PER_SOL))}, nil
				default:
					t.Fatalf("unexpected pubkey %s", pubkey)
					return nil, nil
				}
			},
			GetLatestBlockhashFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return &solanarpc.GetLatestBlockhashResult{
					Value: &solanarpc.LatestBlockhashResult{
						Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
					},
				}, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
				from, to, amount := parseTransferInstruction(t, tx)
				require.Equal(t, signer.PublicKey(), from)
				require.Equal(t, devicePK, to)
				require.Equal(t, 5*solana.LAMPORTS_PER_SOL, amount)

				mu.Lock()
				transferAmount = amount
				mu.Unlock()

				tracker.mark("transfer")
				cancel()
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       5,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() { _ = f.Run(ctx) }()

		tracker.wait(t, 200*time.Millisecond)

		mu.Lock()
		got := transferAmount
		mu.Unlock()

		require.Equal(t, 5*solana.LAMPORTS_PER_SOL, got)
	})

	t.Run("no top up when device balance is above min", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc := &mockServiceability{
			LoadFunc: func(ctx context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				if pubkey == signer.PublicKey() {
					return &solanarpc.GetBalanceResult{
						Value: 10 * solana.LAMPORTS_PER_SOL,
					}, nil
				}
				if pubkey == devicePK {
					return &solanarpc.GetBalanceResult{
						Value: 5 * solana.LAMPORTS_PER_SOL,
					}, nil // already above min
				}
				t.Fatalf("unexpected pubkey %s", pubkey)
				return nil, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				t.Fatal("should not transfer")
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       5,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()
	})

	t.Run("no top up when funder balance is below top up amount", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc := &mockServiceability{
			LoadFunc: func(ctx context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		var funderBalance atomic.Int32

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				if pubkey == signer.PublicKey() {
					funderBalance.Add(1)
					return &solanarpc.GetBalanceResult{
						Value: 1 * solana.LAMPORTS_PER_SOL,
					}, nil // less than TopUpSOL (2 SOL)
				}
				t.Fatalf("unexpected balance check for pubkey %s", pubkey)
				return nil, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				t.Fatal("should not transfer when funder is underfunded")
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       2,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()

		require.GreaterOrEqual(t, funderBalance.Load(), int32(1))
	})

	t.Run("skips loop when Serviceability.Load returns error", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		loadCalled := make(chan struct{}, 1)
		var called struct {
			getDevices bool
			getBalance bool
			transfer   bool
		}

		svc := &mockServiceability{
			LoadFunc: func(ctx context.Context) error {
				loadCalled <- struct{}{}
				return context.DeadlineExceeded
			},
			GetDevicesFunc: func() []serviceability.Device {
				called.getDevices = true
				return nil
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				called.getBalance = true
				return nil, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				called.transfer = true
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       1,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		select {
		case <-loadCalled:
			cancel()
		case <-time.After(100 * time.Millisecond):
			cancel()
			t.Fatal("timed out waiting for Load to be called")
		}

		require.False(t, called.getDevices, "should not call GetDevices")
		require.False(t, called.getBalance, "should not call GetBalance")
		require.False(t, called.transfer, "should not call Transfer")
	})

	t.Run("skips loop when funder GetBalance returns error", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		loadCalled := make(chan struct{}, 1)
		var called struct {
			getBalanceDevice bool
			getDevices       bool
			transfer         bool
		}

		svc := &mockServiceability{
			LoadFunc: func(ctx context.Context) error {
				loadCalled <- struct{}{}
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				called.getDevices = true
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				if pubkey == signer.PublicKey() {
					return nil, context.DeadlineExceeded // simulate error
				}
				called.getBalanceDevice = true
				return &solanarpc.GetBalanceResult{
					Value: 1 * solana.LAMPORTS_PER_SOL,
				}, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				called.transfer = true
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       1,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		select {
		case <-loadCalled:
			cancel()
		case <-time.After(100 * time.Millisecond):
			cancel()
			t.Fatal("timed out waiting for Load to be called")
		}

		require.False(t, called.getDevices, "should not call GetDevices when funder balance fails")
		require.False(t, called.getBalanceDevice, "should not call GetBalance for device")
		require.False(t, called.transfer, "should not attempt transfer")
	})

	t.Run("skips device when device GetBalance returns error", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		loadCalled := make(chan struct{}, 1)
		var called struct {
			transfer bool
		}

		svc := &mockServiceability{
			LoadFunc: func(ctx context.Context) error {
				loadCalled <- struct{}{}
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, commitment solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				if pubkey == signer.PublicKey() {
					return &solanarpc.GetBalanceResult{
						Value: 10 * solana.LAMPORTS_PER_SOL,
					}, nil
				}
				if pubkey == devicePK {
					return nil, context.DeadlineExceeded // simulate error for device
				}
				t.Fatalf("unexpected pubkey %s", pubkey)
				return nil, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
				called.transfer = true
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       1,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		select {
		case <-loadCalled:
			cancel()
		case <-time.After(100 * time.Millisecond):
			cancel()
			t.Fatal("timed out waiting for Load to be called")
		}

		require.False(t, called.transfer, "should not transfer if device balance check fails")
	})

	t.Run("logs and continues when Transfer returns error", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tracker := newTracker("load", "transfer")

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error {
				tracker.mark("load")
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.NewWallet().PublicKey() },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, _ solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				switch pubkey {
				case signer.PublicKey():
					return &solanarpc.GetBalanceResult{Value: 10 * solana.LAMPORTS_PER_SOL}, nil
				case devicePK:
					return &solanarpc.GetBalanceResult{Value: uint64(0.1 * float64(solana.LAMPORTS_PER_SOL))}, nil
				default:
					t.Fatalf("unexpected pubkey %s", pubkey)
					return nil, nil
				}
			},
			GetLatestBlockhashFunc: func(_ context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return &solanarpc.GetLatestBlockhashResult{
					Value: &solanarpc.LatestBlockhashResult{
						Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
					},
				}, nil
			},
			SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
				from, to, amount := parseTransferInstruction(t, tx)
				require.Equal(t, signer.PublicKey(), from)
				require.Equal(t, devicePK, to)
				require.Equal(t, 5*solana.LAMPORTS_PER_SOL, amount)

				tracker.mark("transfer")
				cancel()
				return solana.Signature{}, context.DeadlineExceeded
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       5,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() {
			_ = f.Run(ctx)
		}()

		tracker.wait(t, 200*time.Millisecond)
	})

	t.Run("handles multiple devices with mixed balances", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		deviceOK := solana.NewWallet().PublicKey()
		deviceLow := solana.NewWallet().PublicKey()
		deviceError := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		tracker := newTracker(
			"load",
			"device-ok",
			"device-low",
			"device-error",
			"transfer",
		)

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error {
				tracker.mark("load")
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: deviceOK},
					{MetricsPublisherPubKey: deviceLow},
					{MetricsPublisherPubKey: deviceError},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		var (
			transferTo            solana.PublicKey
			deviceLowBalanceCalls atomic.Int32
			mu                    sync.Mutex
		)

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, _ solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				switch pubkey {
				case signer.PublicKey():
					return &solanarpc.GetBalanceResult{Value: 100 * solana.LAMPORTS_PER_SOL}, nil
				case deviceOK:
					tracker.mark("device-ok")
					return &solanarpc.GetBalanceResult{Value: 5 * solana.LAMPORTS_PER_SOL}, nil
				case deviceLow:
					tracker.mark("device-low")
					if deviceLowBalanceCalls.Add(1) >= 2 {
						return &solanarpc.GetBalanceResult{Value: 5 * solana.LAMPORTS_PER_SOL}, nil
					}
					return &solanarpc.GetBalanceResult{Value: uint64(0.1 * float64(solana.LAMPORTS_PER_SOL))}, nil
				case deviceError:
					tracker.mark("device-error")
					return nil, context.DeadlineExceeded
				default:
					t.Fatalf("unexpected pubkey %s", pubkey)
					return nil, nil
				}
			},
			GetLatestBlockhashFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return &solanarpc.GetLatestBlockhashResult{
					Value: &solanarpc.LatestBlockhashResult{
						Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
					},
				}, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
				from, to, amount := parseTransferInstruction(t, tx)
				require.Equal(t, signer.PublicKey(), from)
				require.Equal(t, deviceLow, to)
				require.Equal(t, 5*solana.LAMPORTS_PER_SOL, amount)

				mu.Lock()
				transferTo = to
				mu.Unlock()

				tracker.mark("transfer")
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       5,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() { _ = f.Run(ctx) }()
		tracker.wait(t, 500*time.Millisecond)
		cancel()

		mu.Lock()
		got := transferTo
		mu.Unlock()
		require.Equal(t, deviceLow, got, "should only transfer to underfunded device")
	})

	t.Run("skips devices with zero MetricsPublisherPubKey", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		validPK := solana.NewWallet().PublicKey()
		zeroPK := solana.PublicKey{} // this should be skipped

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tracker := newTracker("load", "valid-device-checked")

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error {
				tracker.mark("load")
				return nil
			},
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: validPK},
					{MetricsPublisherPubKey: zeroPK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, _ solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				switch pubkey {
				case signer.PublicKey():
					return &solanarpc.GetBalanceResult{Value: 10 * solana.LAMPORTS_PER_SOL}, nil
				case validPK:
					tracker.mark("valid-device-checked")
					return &solanarpc.GetBalanceResult{Value: uint64(0.1 * float64(solana.LAMPORTS_PER_SOL))}, nil
				case zeroPK:
					t.Fatalf("should not check balance for zero MetricsPublisherPubKey")
				default:
					t.Fatalf("unexpected pubkey %s", pubkey)
				}
				return nil, nil
			},
			GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return &solanarpc.GetLatestBlockhashResult{
					Value: &solanarpc.LatestBlockhashResult{
						Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
					},
				}, nil
			},
			SendTransactionWithOptsFunc: func(ctx context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
				_, to, _ := parseTransferInstruction(t, tx)
				require.NotEqual(t, zeroPK, to, "should not transfer to zero pubkey")
				cancel()
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       5,
			Interval:       1 * time.Millisecond,
		})
		require.NoError(t, err)

		go func() { _ = f.Run(ctx) }()
		tracker.wait(t, 200*time.Millisecond)
	})

	t.Run("waitForBalance times out if balance does not increase", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey
		devicePK := solana.NewWallet().PublicKey()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const waitForBalanceTimeout = 100 * time.Millisecond

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				return []serviceability.Device{
					{MetricsPublisherPubKey: devicePK},
				}
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		var (
			transferCalled    atomic.Bool
			balanceCheckCount atomic.Int32
		)

		sol := &mockSolana{
			GetBalanceFunc: func(ctx context.Context, pubkey solana.PublicKey, _ solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				switch pubkey {
				case signer.PublicKey():
					return &solanarpc.GetBalanceResult{Value: 100 * solana.LAMPORTS_PER_SOL}, nil
				case devicePK:
					if transferCalled.Load() {
						balanceCheckCount.Add(1)
						return &solanarpc.GetBalanceResult{Value: uint64(0.01 * float64(solana.LAMPORTS_PER_SOL))}, nil
					}
					return &solanarpc.GetBalanceResult{Value: uint64(0.01 * float64(solana.LAMPORTS_PER_SOL))}, nil
				default:
					t.Fatalf("unexpected pubkey %s", pubkey)
					return nil, nil
				}
			},
			GetLatestBlockhashFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetLatestBlockhashResult, error) {
				return &solanarpc.GetLatestBlockhashResult{
					Value: &solanarpc.LatestBlockhashResult{
						Blockhash: solana.MustHashFromBase58("5NzX7jrPWeTkGsDnVnszdEa7T3Yyr3nSgyc78z3CwjWQ"),
					},
				}, nil
			},
			SendTransactionWithOptsFunc: func(_ context.Context, tx *solana.Transaction, _ solanarpc.TransactionOpts) (solana.Signature, error) {
				from, to, _ := parseTransferInstruction(t, tx)
				require.Equal(t, signer.PublicKey(), from)
				require.Equal(t, devicePK, to)
				transferCalled.Store(true)
				return solana.Signature{}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:                     logger,
			Serviceability:             svc,
			Solana:                     sol,
			Signer:                     signer,
			MinBalanceSOL:              1,
			TopUpSOL:                   1,
			Interval:                   1 * time.Millisecond,
			WaitForBalanceTimeout:      waitForBalanceTimeout,
			WaitForBalancePollInterval: 100 * time.Millisecond,
		})
		require.NoError(t, err)

		errCh := make(chan error, 1)
		var once sync.Once
		go func() {
			err := f.Run(ctx)
			once.Do(func() { errCh <- err })
		}()

		select {
		case err := <-errCh:
			t.Fatalf("funder unexpectedly exited early: %v", err)
		case <-time.After(waitForBalanceTimeout + 200*time.Millisecond):
			cancel()
		}

		require.GreaterOrEqual(t, balanceCheckCount.Load(), int32(2), "should retry balance during wait")
	})

	t.Run("returns nil on context cancellation", func(t *testing.T) {
		t.Parallel()

		signer := solana.NewWallet().PrivateKey

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc := &mockServiceability{
			LoadFunc: func(context.Context) error { return nil },
			GetDevicesFunc: func() []serviceability.Device {
				// Minimal valid device to trigger loop
				return nil
			},
			ProgramIDFunc: func() solana.PublicKey { return solana.PublicKey{} },
		}

		sol := &mockSolana{
			GetBalanceFunc: func(context.Context, solana.PublicKey, solanarpc.CommitmentType) (*solanarpc.GetBalanceResult, error) {
				return &solanarpc.GetBalanceResult{Value: 10 * solana.LAMPORTS_PER_SOL}, nil
			},
		}

		f, err := funder.New(funder.Config{
			Logger:         logger,
			Serviceability: svc,
			Solana:         sol,
			Signer:         signer,
			MinBalanceSOL:  1,
			TopUpSOL:       1,
			Interval:       10 * time.Millisecond,
		})
		require.NoError(t, err)

		done := make(chan error, 1)
		go func() {
			done <- f.Run(ctx)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err, "expected nil, got %v", err)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for Run to return")
		}
	})
}

type callTracker struct {
	mu     sync.Mutex
	events map[string]bool
	wg     sync.WaitGroup
}

func newTracker(expected ...string) *callTracker {
	ct := &callTracker{
		events: make(map[string]bool),
	}
	ct.wg.Add(len(expected))
	for _, e := range expected {
		ct.events[e] = false
	}
	return ct
}

func (ct *callTracker) mark(label string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if _, ok := ct.events[label]; ok && !ct.events[label] {
		ct.events[label] = true
		ct.wg.Done()
	}
}

func (ct *callTracker) wait(t *testing.T, timeout time.Duration) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		ct.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for expected events: %v", ct.pending())
	}
}

func (ct *callTracker) pending() []string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	var out []string
	for k, v := range ct.events {
		if !v {
			out = append(out, k)
		}
	}
	return out
}

func parseTransferInstruction(t *testing.T, tx *solana.Transaction) (from, to solana.PublicKey, amount uint64) {
	t.Helper()

	msg := tx.Message
	require.Len(t, msg.Instructions, 1)
	inst := msg.Instructions[0]
	require.Equal(t, system.ProgramID, msg.AccountKeys[inst.ProgramIDIndex])
	discriminator := bin.TypeIDFromBytes(inst.Data[:4])
	require.Equal(t, system.Instruction_Transfer, discriminator.Uvarint32())
	require.GreaterOrEqual(t, len(inst.Data), 9)

	transfer := system.Transfer{}
	err := transfer.UnmarshalWithDecoder(bin.NewBorshDecoder(inst.Data[4:]))
	require.NoError(t, err)

	from = msg.AccountKeys[inst.Accounts[0]]
	to = msg.AccountKeys[inst.Accounts[1]]
	amount = *transfer.Lamports

	return
}
