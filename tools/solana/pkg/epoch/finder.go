package epoch

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/dgraph-io/ristretto"
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

const (
	ApproximateSlotDuration = 400 * time.Millisecond
)

type SolanaRPCClient interface {
	GetSlot(ctx context.Context, commitment solanarpc.CommitmentType) (out uint64, err error)
	GetEpochInfo(ctx context.Context, commitment solanarpc.CommitmentType) (out *solanarpc.GetEpochInfoResult, err error)
	GetEpochSchedule(ctx context.Context) (out *solanarpc.GetEpochScheduleResult, err error)
	GetSignaturesForAddressWithOpts(ctx context.Context, account solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error)
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

	// Use GetEpochInfo for the authoritative current epoch and slot. This avoids
	// the failure mode where GetSlot returns a stale finalized slot from the previous
	// epoch, causing all records to be assigned the wrong epoch.
	epochInfo, err := e.getEpochInfoWithRetry(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get epoch info: %w", err)
	}

	slotsAgo := uint64(now.Sub(target) / ApproximateSlotDuration)

	var ep uint64
	if slotsAgo <= epochInfo.SlotIndex {
		// Target is within the current epoch — use the authoritative epoch directly.
		ep = epochInfo.Epoch
	} else {
		// Target is before the current epoch — fall back to slot math.
		sched := e.sched.Load()
		if sched == nil {
			val, err := e.getEpochScheduleWithRetry(ctx)
			if err != nil {
				return 0, fmt.Errorf("failed to get epoch schedule: %w", err)
			}
			e.sched.Store(val)
			sched = val
		}
		if slotsAgo > epochInfo.AbsoluteSlot {
			return 0, fmt.Errorf("target time %v is too far in the past", target)
		}
		approxSlot := epochInfo.AbsoluteSlot - slotsAgo
		ep = e.getEpochForSlot(approxSlot, sched)
	}

	e.cache.SetWithTTL(cacheKey, ep, 0, 30*time.Minute)
	e.cache.Wait()

	return ep, nil
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

func (e *epochFinder) getEpochInfoWithRetry(ctx context.Context) (*solanarpc.GetEpochInfoResult, error) {
	attempt := 0
	info, err := backoff.Retry(ctx, func() (*solanarpc.GetEpochInfoResult, error) {
		if attempt > 1 {
			e.log.Warn("Failed to get epoch info, retrying", "attempt", attempt)
		}
		attempt++
		return e.client.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return nil, fmt.Errorf("failed to get epoch info: %w", err)
	}
	return info, nil
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

// SlotTimeFinder provides methods for checking burn-in periods based on account transaction history.
type SlotTimeFinder interface {
	// IsAccountOlderThanBurnInPeriod checks if an account has a transaction older than the burn-in period.
	// It traverses transaction history in reverse chronological order and returns true as soon as
	// it finds a transaction in a slot older than (currentSlot - burnInSlots).
	IsAccountOlderThanBurnInPeriod(ctx context.Context, pubkey solana.PublicKey, burnInSlots uint64) (bool, error)
}

type slotTimeFinder struct {
	log    *slog.Logger
	client SolanaRPCClient
}

// NewSlotTimeFinder creates a new SlotTimeFinder for checking burn-in periods.
func NewSlotTimeFinder(log *slog.Logger, client SolanaRPCClient) (SlotTimeFinder, error) {
	return &slotTimeFinder{
		log:    log,
		client: client,
	}, nil
}

func (s *slotTimeFinder) getCurrentSlot(ctx context.Context) (uint64, error) {
	attempt := 0
	slot, err := backoff.Retry(ctx, func() (uint64, error) {
		if attempt > 1 {
			s.log.Warn("Failed to get current slot, retrying", "attempt", attempt)
		}
		attempt++
		return s.client.GetSlot(ctx, solanarpc.CommitmentFinalized)
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return 0, fmt.Errorf("failed to get current slot: %w", err)
	}
	return slot, nil
}

// IsAccountOlderThanBurnInPeriod determines whether the given device or link record has been onchain for longer than the burn-in timestamp.
// It will be expensive if there are an unexpectedly large number of transactions for the account. If it turns out to be too expensive, we
// don't necessarily need this check at all, because we will be adding metrics checks anyway, and (for example) the device needs to be
// onchain for the controller to emit metrics for it.it
func (s *slotTimeFinder) IsAccountOlderThanBurnInPeriod(ctx context.Context, pubkey solana.PublicKey, burnInSlots uint64) (bool, error) {
	currentSlot, err := s.getCurrentSlot(ctx)
	if err != nil {
		return false, err
	}

	if currentSlot < burnInSlots {
		return false, nil
	}
	targetSlot := currentSlot - burnInSlots

	var before solana.Signature
	limit := 100

	for {
		opts := &solanarpc.GetSignaturesForAddressOpts{
			Limit:      &limit,
			Commitment: solanarpc.CommitmentFinalized,
		}
		if !before.IsZero() {
			opts.Before = before
		}

		sigs, err := s.getSignaturesWithRetry(ctx, pubkey, opts)
		if err != nil {
			return false, fmt.Errorf("failed to get signatures for %s: %w", pubkey, err)
		}

		if len(sigs) == 0 {
			return false, nil
		}

		for _, sig := range sigs {
			if sig.Slot <= targetSlot {
				return true, nil
			}
		}

		before = sigs[len(sigs)-1].Signature
	}
}

func (s *slotTimeFinder) getSignaturesWithRetry(ctx context.Context, pubkey solana.PublicKey, opts *solanarpc.GetSignaturesForAddressOpts) ([]*solanarpc.TransactionSignature, error) {
	attempt := 0
	sigs, err := backoff.Retry(ctx, func() ([]*solanarpc.TransactionSignature, error) {
		if attempt > 1 {
			s.log.Warn("Failed to get signatures, retrying", "attempt", attempt, "pubkey", pubkey)
		}
		attempt++
		return s.client.GetSignaturesForAddressWithOpts(ctx, pubkey, opts)
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures: %w", err)
	}
	return sigs, nil
}
