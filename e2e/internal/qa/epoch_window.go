package qa

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// EpochTailWindow classifies whether the target cluster is currently inside
// the epoch-tail window in which the shred oracle closes the program by
// design: the oracle advances OpenForRequests → ClosedForRequests at slot
// first_slot_of_next_epoch − ProgramConfig.closed_for_requests_grace_period_slots,
// then settles seats and updates prices, and reopens only just after the
// epoch boundary. A WaitForOpenForRequests timeout whose current slot falls
// inside [close target, epoch boundary) is therefore expected roughly once
// per epoch, not an outage.
type EpochTailWindow struct {
	// InWindow is true when CurrentSlot is inside [CloseTargetSlot,
	// NextEpochStartSlot).
	InWindow bool
	// Epoch is the cluster's current epoch at classification time.
	Epoch uint64
	// CurrentSlot is the cluster's current absolute slot.
	CurrentSlot uint64
	// CloseTargetSlot is the slot at which the oracle closes the program for
	// this epoch's tail: NextEpochStartSlot − the onchain grace period.
	CloseTargetSlot uint64
	// NextEpochStartSlot is the first slot of the next epoch (the boundary at
	// which the oracle reopens the program).
	NextEpochStartSlot uint64
	// GracePeriodSlots is ProgramConfig.ClosedForRequestsGracePeriodSlots as
	// read onchain.
	GracePeriodSlots uint32
}

func (w EpochTailWindow) String() string {
	return fmt.Sprintf("epoch %d boundary at slot %d, close target slot %d (grace period %d slots), current slot %d",
		w.Epoch, w.NextEpochStartSlot, w.CloseTargetSlot, w.GracePeriodSlots, w.CurrentSlot)
}

// classifyEpochTailWindow computes the epoch-tail close window from the
// cluster's epoch info (as returned by getEpochInfo) and the onchain grace
// period, and reports whether currentSlot falls inside it. Pure so it is
// unit-testable without an RPC. It errors — rather than guessing a
// classification — on inconsistent epoch info or a grace period that covers
// the whole epoch (a misclassification here would either page on-call for a
// benign window or, worse, silence a real outage).
func classifyEpochTailWindow(epoch, currentSlot, slotIndex, slotsInEpoch uint64, gracePeriodSlots uint32) (EpochTailWindow, error) {
	if slotsInEpoch == 0 {
		return EpochTailWindow{}, fmt.Errorf("invalid epoch info: slots-in-epoch is zero")
	}
	if slotIndex >= slotsInEpoch {
		return EpochTailWindow{}, fmt.Errorf("inconsistent epoch info: slot index %d >= slots in epoch %d", slotIndex, slotsInEpoch)
	}
	if slotIndex > currentSlot {
		return EpochTailWindow{}, fmt.Errorf("inconsistent epoch info: slot index %d > current slot %d", slotIndex, currentSlot)
	}
	grace := uint64(gracePeriodSlots)
	if grace >= slotsInEpoch {
		// A grace period spanning the whole epoch would classify every slot as
		// benign and permanently mask real outages; refuse rather than guess.
		return EpochTailWindow{}, fmt.Errorf("grace period (%d slots) covers the entire epoch (%d slots); refusing to classify", grace, slotsInEpoch)
	}
	nextEpochStart := currentSlot - slotIndex + slotsInEpoch
	closeTarget := nextEpochStart - grace
	return EpochTailWindow{
		InWindow:           currentSlot >= closeTarget,
		Epoch:              epoch,
		CurrentSlot:        currentSlot,
		CloseTargetSlot:    closeTarget,
		NextEpochStartSlot: nextEpochStart,
		GracePeriodSlots:   gracePeriodSlots,
	}, nil
}

// EpochTailClosedWindow reads live chain state — the grace period from the
// shred-subscription ProgramConfig and the epoch schedule position from the
// cluster's getEpochInfo — and classifies whether the current slot is inside
// the epoch-tail window in which the program is closed by design. Both reads
// go through the client's failover RPC pool, so they target whatever cluster
// and program the active -env selects; nothing is environment-specific.
func (c *Client) EpochTailClosedWindow(ctx context.Context) (EpochTailWindow, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return EpochTailWindow{}, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}
	cfg, err := c.shredsClient(programID).FetchProgramConfig(ctx)
	if err != nil {
		// Scrub: a fetch error can embed the (possibly API-keyed) endpoint URL.
		return EpochTailWindow{}, fmt.Errorf("failed to fetch program config on host %s: %s", c.Host, c.scrubRPCErr(err))
	}

	var solanaClient *rpc.Client
	if c.solanaRPC != nil {
		solanaClient = c.solanaRPC.RPC()
	} else {
		solanaClient = rpc.New(c.SolanaRPCURL)
	}
	info, err := solanaClient.GetEpochInfo(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return EpochTailWindow{}, fmt.Errorf("failed to get epoch info on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	return classifyEpochTailWindow(info.Epoch, info.AbsoluteSlot, info.SlotIndex, info.SlotsInEpoch, cfg.ClosedForRequestsGracePeriodSlots)
}
