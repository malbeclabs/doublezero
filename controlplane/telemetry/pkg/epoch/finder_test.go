package epoch_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/epoch"
	"github.com/stretchr/testify/require"
)

type mockSolanaRPCClient struct {
	GetSlotFunc          func(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
	GetEpochScheduleFunc func(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
}

func (m *mockSolanaRPCClient) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	return m.GetSlotFunc(ctx, commitment)
}

func (m *mockSolanaRPCClient) GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error) {
	return m.GetEpochScheduleFunc(ctx)
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
