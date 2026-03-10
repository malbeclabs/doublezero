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
	GetEpochInfoFunc                    func(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error)
	GetEpochScheduleFunc                func(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
	GetSignaturesForAddressWithOptsFunc func(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error)
}

func (m *mockSolanaRPCClient) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	if m.GetSlotFunc != nil {
		return m.GetSlotFunc(ctx, commitment)
	}
	return 0, nil
}

func (m *mockSolanaRPCClient) GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
	if m.GetEpochInfoFunc != nil {
		return m.GetEpochInfoFunc(ctx, commitment)
	}
	return nil, nil
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
			GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: 1_296_000, // epoch 3 start
					Epoch:        3,
					SlotIndex:    0,
					SlotsInEpoch: 432_000,
				}, nil
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
			GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: 10_000,
					Epoch:        0,
					SlotIndex:    10_000,
					SlotsInEpoch: 432_000,
				}, nil
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

		var getEpochInfoCalls int
		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				getEpochInfoCalls++
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: 432_000,
					Epoch:        1,
					SlotIndex:    0,
					SlotsInEpoch: 432_000,
				}, nil
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
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

		require.Equal(t, 1, getEpochInfoCalls)
	})

	t.Run("warmup epoch calculation", func(t *testing.T) {
		t.Parallel()
		log := log.With("test", t.Name())

		warmupSched := &solanarpc.GetEpochScheduleResult{
			FirstNormalEpoch: 3,
			FirstNormalSlot:  28,
			SlotsPerEpoch:    8,
			Warmup:           true,
		}

		// Warmup: epoch 0 = 2 slots (0-1), epoch 1 = 4 slots (2-5), epoch 2 = 8 slots (6-13)
		// Slot 10 is in epoch 2, slotIndex = 4
		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(ctx context.Context, _ solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: 10,
					Epoch:        2,
					SlotIndex:    4,
					SlotsInEpoch: 8,
				}, nil
			},
			GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return warmupSched, nil
			},
		}

		f, err := epoch.NewFinder(log, client)
		require.NoError(t, err)

		target := time.Now().Add(-400 * time.Millisecond) // 1 slot ago, within epoch 2
		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, uint64(2), got)
	})

	t.Run("target just after epoch start returns current epoch", func(t *testing.T) {
		t.Parallel()

		epochVal := uint64(3)
		slotOffset := uint64(5) // 5 slots into epoch = 2s

		mockNow := time.Unix(1_000_000_000, 0) // fixed "now"
		target := mockNow.Add(-epoch.ApproximateSlotDuration * time.Duration(slotOffset))

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: sched.SlotsPerEpoch*epochVal + slotOffset,
					Epoch:        epochVal,
					SlotIndex:    slotOffset,
					SlotsInEpoch: sched.SlotsPerEpoch,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
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
		slotOffset := sched.SlotsPerEpoch - 10 // 10 slots before next epoch

		mockNow := time.Unix(1_000_000_000, 0)
		target := mockNow.Add(-epoch.ApproximateSlotDuration * 5) // ~5 slots ago

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: sched.SlotsPerEpoch*epochVal + slotOffset,
					Epoch:        epochVal,
					SlotIndex:    slotOffset,
					SlotsInEpoch: sched.SlotsPerEpoch,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
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

		mockNow := time.Unix(1_000_000_000, 0)
		target := mockNow // no offset

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: sched.SlotsPerEpoch * epochVal,
					Epoch:        epochVal,
					SlotIndex:    0,
					SlotsInEpoch: sched.SlotsPerEpoch,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, epochVal, got)
	})

}

func TestEpochFinder_GetEpochInfoFixesStaleSlotBug(t *testing.T) {
	// Verifies the fix for the 2026-03-10 incident where the old implementation
	// used GetSlot to approximate epochs, and a stale finalized slot caused the
	// epoch finder to return epoch 192 for ~51 minutes after epoch 193 started.
	//
	// The fix uses GetEpochInfo which returns the authoritative epoch directly,
	// eliminating the slot→epoch approximation that was vulnerable to stale slots.

	sched := &solanarpc.GetEpochScheduleResult{
		FirstNormalEpoch: 14,
		FirstNormalSlot:  524_256,
		SlotsPerEpoch:    432_000,
		Warmup:           true,
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Epoch 193 starts at slot: (193-14)*432000 + 524256 = 77,852,256
	epoch193StartSlot := uint64((193-14)*432_000 + 524_256)

	t.Run("returns authoritative epoch for recent records", func(t *testing.T) {
		t.Parallel()

		// GetEpochInfo returns epoch 193 (the truth), even if the underlying
		// slot is only slightly past the boundary. The epoch is authoritative.
		mockNow := time.Unix(2_000_000_000, 0)
		target := mockNow.Add(-2 * time.Minute) // 2 min ago = 300 slots

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: epoch193StartSlot + 1500, // 10 min into epoch 193
					Epoch:        193,
					SlotIndex:    1500,
					SlotsInEpoch: 432_000,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		// slotsAgo=300 <= slotIndex=1500 → uses authoritative epoch 193
		require.Equal(t, uint64(193), got, "uses authoritative epoch from GetEpochInfo")
	})

	t.Run("falls back to slot math for targets in prior epochs", func(t *testing.T) {
		t.Parallel()

		// Target is 50 minutes ago (7500 slots), but we're only 1500 slots
		// into epoch 193. slotsAgo > slotIndex, so falls to slot math.
		mockNow := time.Unix(2_000_000_000, 0)
		target := mockNow.Add(-50 * time.Minute) // 7500 slots ago

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: epoch193StartSlot + 1500,
					Epoch:        193,
					SlotIndex:    1500,
					SlotsInEpoch: 432_000,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		// slotsAgo=7500 > slotIndex=1500, approxSlot = epoch193Start+1500-7500 = epoch193Start-6000
		// That's in epoch 192.
		require.Equal(t, uint64(192), got, "correctly falls back to prior epoch via slot math")
	})

	t.Run("recent target at epoch boundary uses authoritative epoch", func(t *testing.T) {
		t.Parallel()

		// Even with very few slots into the new epoch (slotIndex=10),
		// a recent target (slotsAgo=5) still uses the authoritative epoch.
		mockNow := time.Unix(2_000_000_000, 0)
		target := mockNow.Add(-2 * time.Second) // 5 slots ago

		client := &mockSolanaRPCClient{
			GetEpochInfoFunc: func(context.Context, solanarpc.CommitmentType) (*solanarpc.GetEpochInfoResult, error) {
				return &solanarpc.GetEpochInfoResult{
					AbsoluteSlot: epoch193StartSlot + 10, // just 10 slots into epoch 193
					Epoch:        193,
					SlotIndex:    10,
					SlotsInEpoch: 432_000,
				}, nil
			},
			GetEpochScheduleFunc: func(context.Context) (*solanarpc.GetEpochScheduleResult, error) {
				return sched, nil
			},
		}

		f, err := epoch.NewFinderWithNowFn(log, client, func() time.Time { return mockNow })
		require.NoError(t, err)

		got, err := f.ApproximateAtTime(context.Background(), target)
		require.NoError(t, err)
		// slotsAgo=5 <= slotIndex=10 → epoch 193
		require.Equal(t, uint64(193), got, "boundary case still returns authoritative epoch")
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
