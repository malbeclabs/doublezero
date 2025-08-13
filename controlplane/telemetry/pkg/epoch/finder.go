package epoch

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dgraph-io/ristretto"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

const (
	ApproximateSlotDuration = 400 * time.Millisecond
)

type SolanaRPCClient interface {
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (out uint64, err error)
	GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
}

type Finder interface {
	ApproximateAtTime(ctx context.Context, target time.Time) (uint64, error)
}

type epochFinder struct {
	log    *slog.Logger
	client SolanaRPCClient
	cache  *ristretto.Cache
	sched  atomic.Pointer[solanarpc.GetEpochScheduleResult]
	nowFn  func() time.Time
}

func NewFinder(log *slog.Logger, client SolanaRPCClient) (Finder, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1_000_000,
		MaxCost:     100_000,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("create epoch cache: %w", err)
	}

	return &epochFinder{
		log:    log,
		client: client,
		cache:  cache,
		nowFn:  time.Now,
	}, nil
}

func NewFinderWithNowFn(log *slog.Logger, client SolanaRPCClient, nowFn func() time.Time) (Finder, error) {
	f, err := NewFinder(log, client)
	if err != nil {
		return nil, err
	}
	f.(*epochFinder).nowFn = nowFn
	return f, nil
}

func (e *epochFinder) ApproximateAtTime(ctx context.Context, target time.Time) (uint64, error) {
	now := e.nowFn()
	if target.After(now) {
		return 0, fmt.Errorf("target time %v is in the future", target)
	}

	cacheKey := target.Truncate(time.Minute).Unix()
	if val, ok := e.cache.Get(cacheKey); ok {
		return val.(uint64), nil
	}

	sched := e.sched.Load()
	if sched == nil {
		val, err := e.getEpochScheduleWithRetry(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get epoch schedule: %w", err)
		}
		e.sched.Store(val)
		sched = val
	}

	currentSlot, err := e.getSlotWithRetry(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get current slot: %w", err)
	}

	slotsAgo := now.Sub(target) / ApproximateSlotDuration

	if uint64(slotsAgo) > currentSlot {
		return 0, fmt.Errorf("target time %v is too far in the past", target)
	}
	approxSlot := currentSlot - uint64(slotsAgo)

	epoch := e.getEpochForSlot(approxSlot, sched)

	e.cache.SetWithTTL(cacheKey, epoch, 0, 30*time.Minute)
	e.cache.Wait()

	return epoch, nil
}

func (e *epochFinder) getEpochForSlot(slot uint64, sched *solanarpc.GetEpochScheduleResult) uint64 {
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

func (e *epochFinder) getSlotWithRetry(ctx context.Context) (uint64, error) {
	attempt := 0
	slot, err := backoff.Retry(ctx, func() (uint64, error) {
		if attempt > 1 {
			e.log.Warn("Failed to get current slot, retrying", "attempt", attempt)
		}
		attempt++
		slot, err := e.client.GetSlot(ctx, solanarpc.CommitmentFinalized)
		if err != nil {
			return 0, err
		}
		return slot, nil
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return 0, fmt.Errorf("failed to get current slot: %w", err)
	}
	return slot, nil
}

func (e *epochFinder) getEpochScheduleWithRetry(ctx context.Context) (*solanarpc.GetEpochScheduleResult, error) {
	attempt := 0
	sched, err := backoff.Retry(ctx, func() (*solanarpc.GetEpochScheduleResult, error) {
		if attempt > 1 {
			e.log.Warn("Failed to get epoch schedule, retrying", "attempt", attempt)
		}
		attempt++
		return e.client.GetEpochSchedule(ctx)
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch schedule: %w", err)
	}
	return sched, nil
}
