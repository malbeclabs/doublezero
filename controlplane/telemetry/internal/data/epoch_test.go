package data_test

import (
	"context"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	"github.com/stretchr/testify/require"
)

type mockSolanaRPCClient struct {
	GetSlotFunc          func(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error)
	GetBlockTimeFunc     func(ctx context.Context, block uint64) (out *solana.UnixTimeSeconds, err error)
	GetEpochScheduleFunc func(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
}

func (m *mockSolanaRPCClient) GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
	return m.GetSlotFunc(ctx, commitment)
}

func (m *mockSolanaRPCClient) GetBlockTime(ctx context.Context, block uint64) (out *solana.UnixTimeSeconds, err error) {
	return m.GetBlockTimeFunc(ctx, block)
}

func (m *mockSolanaRPCClient) GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error) {
	return m.GetEpochScheduleFunc(ctx)
}

func TestEpochFinder_FindEpochAtTime(t *testing.T) {
	ctx := context.Background()
	const slotDuration = 400 * time.Millisecond
	const testSlot = uint64(3600)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	target := start.Add(time.Duration(testSlot) * slotDuration)

	blockTimes := make(map[uint64]time.Time)
	for i := uint64(0); i <= 5000; i++ {
		blockTimes[i] = start.Add(time.Duration(i) * slotDuration)
	}

	mock := &mockSolanaRPCClient{
		GetSlotFunc: func(ctx context.Context, commitment solanarpc.CommitmentType) (uint64, error) {
			return testSlot, nil
		},
		GetBlockTimeFunc: func(ctx context.Context, slot uint64) (*solana.UnixTimeSeconds, error) {
			t, ok := blockTimes[slot]
			if !ok {
				zero := solana.UnixTimeSeconds(0)
				return &zero, nil
			}
			v := solana.UnixTimeSeconds(t.Unix())
			return &v, nil
		},
		GetEpochScheduleFunc: func(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
			return &solanarpc.GetEpochScheduleResult{
				SlotsPerEpoch:    1000,
				FirstNormalEpoch: 2,
				FirstNormalSlot:  2000,
				Warmup:           false,
			}, nil
		},
	}

	finder, err := data.NewEpochFinder(mock)
	require.NoError(t, err)

	wantEpoch := uint64(3)

	gotEpoch, err := finder.FindEpochAtTime(ctx, target)
	require.NoError(t, err)
	t.Logf("target=%s → gotEpoch=%d", target.Format(time.RFC3339), gotEpoch)
	require.Equal(t, wantEpoch, gotEpoch)

	gotCached, err := finder.FindEpochAtTime(ctx, target)
	require.NoError(t, err)
	require.Equal(t, gotEpoch, gotCached)
}
