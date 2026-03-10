package epoch_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
	"github.com/stretchr/testify/require"
)

type mockSolanaRPCClient struct {
	GetSlotFunc                         func(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
	GetEpochScheduleFunc                func(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
	GetSignaturesForAddressWithOptsFunc func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error)
}

func (m *mockSolanaRPCClient) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	return m.GetSlotFunc(ctx, commitment)
}

func (m *mockSolanaRPCClient) GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error) {
	return m.GetEpochScheduleFunc(ctx)
}

func (m *mockSolanaRPCClient) GetSignaturesForAddressWithOpts(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
	if m.GetSignaturesForAddressWithOptsFunc != nil {
		return m.GetSignaturesForAddressWithOptsFunc(ctx, account, opts)
	}
	return nil, nil
}

func TestEpochFinder(t *testing.T) {
	sched := &solanarpc.GetEpochScheduleResult{
		FirstNormalEpoch: 0,
		FirstNormalSlot:  0,
		SlotsPerEpoch:    432000,
		Warmup:           false,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	t.Run("approximates correct epoch", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return 1_296_000, nil // epoch 3
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}
		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		target := time.Now().Add(-2 * time.Hour) // ~18,000 slots ago
		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.LessOrEqual(t, got, uint64(3))
		require.GreaterOrEqual(t, got, uint64(2))
	})

	t.Run("errors on future time", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return 1000, nil
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}
		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		target := time.Now().Add(1 * time.Hour)
		_, err = f.ApproximateAtTime(context.Background(), target)
		require.ErrorContains(t, err, "in the future")
	})

	t.Run("errors if too far in past", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return 10_000, nil // small currentEpoch
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}
		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		target := time.Now().Add(-30 * 24 * time.Hour)
		_, err = f.ApproximateAtTime(context.Background(), target)
		require.ErrorContains(t, err, "too far in the past")
	})

	t.Run("caches result", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		var getSlotCalls, getEpochScheduleCalls int
		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				getSlotCalls++
				return 432_000, nil // epoch 1
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				getEpochScheduleCalls++
				return sched, nil
			},
		}
		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		// Compute target once to ensure cache hit
		target := time.Now().Add(-1 * time.Hour).Truncate(time.Minute)

		_, err = f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)

		_, err = f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)

		require.Equal(t, 1, getSlotCalls)
		require.Equal(t, 1, getEpochScheduleCalls)
	})

	t.Run("warmup epoch calculation", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		sched := &solanarpc.GetEpochScheduleResult{
			FirstNormalEpoch: 3,
			FirstNormalSlot:  28,
			SlotsPerEpoch:    8,
			Warmup:           true,
		}

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return 10, nil // Slot 10 falls in epoch 2
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		target := time.Now().Add(-400 * time.Millisecond)
		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, uint64(2), got)
	})

	t.Run("target just after epoch start returns current epoch", func(t *testing.T) {
		t.Parallel()

		epochVal := uint64(3)
		epochStartSlot := sched.SlotsPerEpoch * epochVal
		slotOffset := uint64(5) // 5 slots into epoch = 2s

		mockNow := time.Unix(1_000_000_000, 0) // fixed "now"
		target := mockNow.Add(-epoch.ApproximateSlotDuration * time.Duration(slotOffset))

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				return epochStartSlot + slotOffset, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return &solanarpc.GetEpochScheduleResult{
					FirstNormalEpoch: 0,
					FirstNormalSlot:  0,
					SlotsPerEpoch:    sched.SlotsPerEpoch,
					Warmup:           false,
				}, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, epochVal, got)
	})

	t.Run("target just before epoch end returns current epoch", func(t *testing.T) {
		t.Parallel()

		epochVal := uint64(3)
		epochStartSlot := sched.SlotsPerEpoch * epochVal
		slotOffset := sched.SlotsPerEpoch - 10 // 10 slots before next epoch
		mockSlot := epochStartSlot + slotOffset

		mockNow := time.Unix(1_000_000_000, 0)
		target := mockNow.Add(-epoch.ApproximateSlotDuration * 5) // ~5 slots ago

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				return mockSlot, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return &solanarpc.GetEpochScheduleResult{
					FirstNormalEpoch: 0,
					FirstNormalSlot:  0,
					SlotsPerEpoch:    sched.SlotsPerEpoch,
					Warmup:           false,
				}, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, epochVal, got)
	})

	t.Run("target exactly at epoch start returns new epoch", func(t *testing.T) {
		t.Parallel()

		epochVal := uint64(4)
		epochStartSlot := sched.SlotsPerEpoch * epochVal

		mockNow := time.Unix(1_000_000_000, 0)
		target := mockNow // no offset

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				return epochStartSlot, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return &solanarpc.GetEpochScheduleResult{
					FirstNormalEpoch: 0,
					FirstNormalSlot:  0,
					SlotsPerEpoch:    sched.SlotsPerEpoch,
					Warmup:           false,
				}, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, epochVal, got)
	})

}

func TestEpochFinder_StaleGetSlotCausesWrongEpoch(t *testing.T) {
	// Reproduces the issue observed on 2026-03-10 where the epoch finder returned
	// epoch 192 for ~51 minutes after epoch 193 started, because GetSlot(finalized)
	// was returning a stale slot still in epoch 192.
	//
	// The epoch finder computes: approxSlot = currentSlot - (now-target)/400ms
	// If currentSlot is stale (still in the old epoch), approxSlot will also be
	// in the old epoch, even for post-boundary target timestamps.

	sched := &solanarpc.GetEpochScheduleResult{
		FirstNormalEpoch: 14,
		FirstNormalSlot:  524_256,
		SlotsPerEpoch:    432_000,
		Warmup:           true,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Epoch 193 starts at slot: (193-14)*432000 + 524256 = 77,852,256
	epoch193StartSlot := uint64((193-14)*432_000 + 524_256)

	t.Run("stale GetSlot returns wrong epoch for post-boundary records", func(t *testing.T) {
		t.Parallel()

		// Simulate: real time is 10 minutes after epoch boundary, but GetSlot
		// returns a slot from 1 minute BEFORE the boundary (stale by 11 minutes).
		staleSlot := epoch193StartSlot - 150 // ~1 minute before boundary at 400ms/slot

		mockNow := time.Unix(2_000_000_000, 0)
		// Target is a record from 2 minutes ago (well after the epoch boundary)
		target := mockNow.Add(-2 * time.Minute)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				return staleSlot, nil // stale!
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)

		// BUG: With a stale slot, the epoch finder computes:
		//   slotsAgo = 2min / 400ms = 300
		//   approxSlot = (epoch193Start - 150) - 300 = epoch193Start - 450
		//   epoch = 192 (WRONG — should be 193 for a post-boundary record)
		//
		// This is the root cause: if GetSlot returns a stale finalized slot,
		// the epoch finder returns the wrong epoch for all records.
		//
		// We assert the CURRENT (buggy) behavior to document it.
		// The real fix is to use GetEpochInfo instead of approximating from GetSlot.
		require.Equal(t, uint64(192), got, "stale GetSlot causes epoch finder to return old epoch")
	})

	t.Run("fresh GetSlot returns correct epoch for post-boundary records", func(t *testing.T) {
		t.Parallel()

		// Same scenario but GetSlot returns the real current slot (10 min into epoch 193)
		freshSlot := epoch193StartSlot + 1500 // ~10 minutes into epoch 193

		mockNow := time.Unix(2_000_000_000, 0)
		target := mockNow.Add(-2 * time.Minute)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				return freshSlot, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)

		require.Equal(t, uint64(193), got, "fresh GetSlot gives correct epoch")
	})

	t.Run("cache amplifies stale GetSlot across minutes", func(t *testing.T) {
		t.Parallel()

		// The epoch finder caches results with 30-minute TTL per minute-bucket.
		// If GetSlot is briefly stale (say, for 1 minute), the cached wrong epoch
		// persists for 30 minutes for that minute-bucket.

		callCount := 0
		staleSlot := epoch193StartSlot - 150
		freshSlot := epoch193StartSlot + 1500

		mockNow := time.Unix(2_000_000_000, 0)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(context.Context, solanarpc.CommitmentType) (uint64, error) {
				callCount++
				if callCount == 1 {
					return staleSlot, nil // first call is stale
				}
				return freshSlot, nil // subsequent calls are fresh
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		// First call: stale GetSlot → caches epoch 192 for this minute
		target1 := mockNow.Add(-2 * time.Minute)
		got1, err := f.ApproximateAtTime(context.Background(), target1)
		require.NoError(t, err)
		require.Equal(t, uint64(192), got1, "first call with stale slot returns 192")

		// Second call with same minute-bucket: cache hit, still returns 192
		// even though GetSlot would now return a fresh slot
		target2 := mockNow.Add(-2*time.Minute - 10*time.Second)
		got2, err := f.ApproximateAtTime(context.Background(), target2)
		require.NoError(t, err)
		require.Equal(t, uint64(192), got2, "cached value persists even after GetSlot is fresh")

		// Third call with a DIFFERENT minute-bucket: cache miss, fresh GetSlot
		target3 := mockNow.Add(-3 * time.Minute)
		got3, err := f.ApproximateAtTime(context.Background(), target3)
		require.NoError(t, err)
		require.Equal(t, uint64(193), got3, "different minute gets fresh computation")
	})
}

func TestSlotTimeFinder(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	t.Run("IsAccountOlderThanBurnInPeriod returns true when transaction older than burn-in exists", func(t *testing.T) {
		t.Parallel()

		currentSlot := uint64(300_000)
		burnInSlots := uint64(200_000)
		// targetSlot = currentSlot - burnInSlots = 100_000

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return currentSlot, nil
			},
			GetSignaturesForAddressWithOptsFunc: func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
				return []*solanarpc.TransactionSignature{
					{Slot: 250_000}, // newer than target
					{Slot: 150_000}, // newer than target
					{Slot: 50_000},  // older than target - should trigger return
				}, nil
			},
		}

		f, err := epoch.NewSlotTimeFinder(log, client)
		require.NoError(t, err)

		pubkey := solana.NewWallet().PublicKey()
		pastBurnIn, err := f.IsAccountOlderThanBurnInPeriod(context.Background(), pubkey, burnInSlots)
		require.NoError(t, err)
		require.True(t, pastBurnIn)
	})

	t.Run("IsAccountOlderThanBurnInPeriod returns false when all transactions are newer than burn-in", func(t *testing.T) {
		t.Parallel()

		currentSlot := uint64(300_000)
		burnInSlots := uint64(200_000)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return currentSlot, nil
			},
			GetSignaturesForAddressWithOptsFunc: func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
				if opts.Before.IsZero() {
					return []*solanarpc.TransactionSignature{
						{Slot: 250_000, Signature: solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW")},
						{Slot: 200_000, Signature: solana.MustSignatureFromBase58("4oCEqwGbMcbTYHZ8ZVjLu6Z8HZMyvCpWJoLtxV3j3EGPKA5V6RFZT7y4rjDwKkD6gxmD4dL7RJXFvU7LzuXNexT3")},
					}, nil
				}
				return []*solanarpc.TransactionSignature{}, nil
			},
		}

		f, err := epoch.NewSlotTimeFinder(log, client)
		require.NoError(t, err)

		pubkey := solana.NewWallet().PublicKey()
		pastBurnIn, err := f.IsAccountOlderThanBurnInPeriod(context.Background(), pubkey, burnInSlots)
		require.NoError(t, err)
		require.False(t, pastBurnIn)
	})

	t.Run("IsAccountOlderThanBurnInPeriod returns false when no transactions exist", func(t *testing.T) {
		t.Parallel()

		currentSlot := uint64(300_000)
		burnInSlots := uint64(200_000)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return currentSlot, nil
			},
			GetSignaturesForAddressWithOptsFunc: func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
				return []*solanarpc.TransactionSignature{}, nil
			},
		}

		f, err := epoch.NewSlotTimeFinder(log, client)
		require.NoError(t, err)

		pubkey := solana.NewWallet().PublicKey()
		pastBurnIn, err := f.IsAccountOlderThanBurnInPeriod(context.Background(), pubkey, burnInSlots)
		require.NoError(t, err)
		require.False(t, pastBurnIn)
	})

	t.Run("IsAccountOlderThanBurnInPeriod returns false when the ledger has fewer slots than the burn-in period", func(t *testing.T) {
		t.Parallel()

		currentSlot := uint64(100_000)
		burnInSlots := uint64(200_000)

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return currentSlot, nil
			},
		}

		f, err := epoch.NewSlotTimeFinder(log, client)
		require.NoError(t, err)

		pubkey := solana.NewWallet().PublicKey()
		pastBurnIn, err := f.IsAccountOlderThanBurnInPeriod(context.Background(), pubkey, burnInSlots)
		require.NoError(t, err)
		require.False(t, pastBurnIn)
	})

	t.Run("IsAccountOlderThanBurnInPeriod paginates through results", func(t *testing.T) {
		t.Parallel()

		currentSlot := uint64(500_000)
		burnInSlots := uint64(200_000)
		calls := 0

		sig1 := solana.MustSignatureFromBase58("5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW")
		sig2 := solana.MustSignatureFromBase58("4oCEqwGbMcbTYHZ8ZVjLu6Z8HZMyvCpWJoLtxV3j3EGPKA5V6RFZT7y4rjDwKkD6gxmD4dL7RJXFvU7LzuXNexT3")

		client := &mockSolanaRPCClient{
			GetSlotFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (uint64, error) {
				return currentSlot, nil
			},
			GetSignaturesForAddressWithOptsFunc: func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
				calls++
				switch calls {
				case 1:
					return []*solanarpc.TransactionSignature{
						{Slot: 450_000, Signature: sig1},
						{Slot: 400_000, Signature: sig2},
					}, nil
				case 2:
					return []*solanarpc.TransactionSignature{
						{Slot: 350_000, Signature: sig1},
						{Slot: 310_000, Signature: sig2},
					}, nil
				default:
					return []*solanarpc.TransactionSignature{}, nil
				}
			},
		}

		f, err := epoch.NewSlotTimeFinder(log, client)
		require.NoError(t, err)

		pubkey := solana.NewWallet().PublicKey()
		pastBurnIn, err := f.IsAccountOlderThanBurnInPeriod(context.Background(), pubkey, burnInSlots)
		require.NoError(t, err)
		require.False(t, pastBurnIn)
		require.Equal(t, 3, calls)
	})
}
