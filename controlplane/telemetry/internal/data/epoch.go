package data

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type SolanaRPCClient interface {
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (out uint64, err error)
	GetBlockTime(ctx context.Context, block uint64) (out *solana.UnixTimeSeconds, err error)
	GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
}

type EpochFinder interface {
	FindEpochAtTime(ctx context.Context, target time.Time) (uint64, error)
}

type epochFinder struct {
	client         SolanaRPCClient
	epochCache     *ristretto.Cache
	blockTimeCache *ristretto.Cache
	epochSchedule  atomic.Pointer[solanarpc.GetEpochScheduleResult]
}

func NewEpochFinder(client SolanaRPCClient) (EpochFinder, error) {
	epochCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1_000_000,
		MaxCost:     10_000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("create epoch cache: %w", err)
	}
	blockTimeCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1_000_000,
		MaxCost:     10_000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("create block time cache: %w", err)
	}
	return &epochFinder{
		client:         client,
		epochCache:     epochCache,
		blockTimeCache: blockTimeCache,
	}, nil
}

func (e *epochFinder) FindEpochAtTime(ctx context.Context, target time.Time) (uint64, error) {
	rounded := target.Truncate(time.Minute)
	key := rounded.Unix()

	if val, ok := e.epochCache.Get(key); ok {
		return val.(uint64), nil
	}

	const slotDuration = 400 * time.Millisecond
	const searchSlack = 50_000
	const searchCap = 200_000

	currentSlot, err := e.client.GetSlot(ctx, solanarpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("get current slot: %w", err)
	}
	currentBlockTime, err := e.getBlockTime(ctx, currentSlot)
	if err != nil {
		return 0, fmt.Errorf("get current block time: %w", err)
	}
	currentTime := time.Unix(int64(*currentBlockTime), 0)
	slotEstimate := int64(currentSlot) + int64(target.Sub(currentTime)/slotDuration)

	high := max(slotEstimate+searchSlack, int64(currentSlot))
	high = min(high, int64(currentSlot)+searchCap)
	low := int64(0)

	var resultSlot uint64
	for low <= high {
		mid := (low + high) / 2
		bt, err := e.getBlockTime(ctx, uint64(mid))
		if err != nil || *bt == 0 {
			high = mid - 1
			continue
		}
		btTime := time.Unix(int64(*bt), 0)
		if btTime.After(target) {
			high = mid - 1
		} else {
			resultSlot = uint64(mid)
			low = mid + 1
		}
	}

	sched := e.epochSchedule.Load()
	if sched == nil {
		schedVal, err := e.client.GetEpochSchedule(ctx)
		if err != nil {
			return 0, fmt.Errorf("get epoch schedule: %w", err)
		}
		e.epochSchedule.Store(schedVal)
		sched = schedVal
	}
	epoch := getEpochForSlot(resultSlot, sched)

	e.epochCache.Set(key, epoch, 1)

	return epoch, nil
}

func (e *epochFinder) getBlockTime(ctx context.Context, slot uint64) (*solana.UnixTimeSeconds, error) {
	if val, ok := e.blockTimeCache.Get(slot); ok {
		return val.(*solana.UnixTimeSeconds), nil
	}
	bt, err := e.client.GetBlockTime(ctx, slot)
	if err == nil && bt != nil {
		e.blockTimeCache.Set(slot, bt, 1)
	}
	return bt, err
}

func getEpochForSlot(slot uint64, sched *solanarpc.GetEpochScheduleResult) uint64 {
	if sched.SlotsPerEpoch == 0 {
		return 0
	}
	if !sched.Warmup {
		return (slot-sched.FirstNormalSlot)/sched.SlotsPerEpoch + sched.FirstNormalEpoch
	}
	if slot >= sched.FirstNormalSlot {
		return (slot-sched.FirstNormalSlot)/sched.SlotsPerEpoch + sched.FirstNormalEpoch
	}

	epoch := uint64(0)
	slotsInEpoch := sched.SlotsPerEpoch / (1 << (sched.FirstNormalEpoch - 1))
	currentSlot := uint64(0)
	for {
		if currentSlot+slotsInEpoch > slot {
			break
		}
		currentSlot += slotsInEpoch
		epoch++
		slotsInEpoch *= 2
	}
	return epoch
}
