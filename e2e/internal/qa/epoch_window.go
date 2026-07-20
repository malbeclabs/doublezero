package qa

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	shreds "github.com/malbeclabs/doublezero/sdk/shreds/go"
)

// EpochTailWindow classifies whether a WaitForOpenForRequests timeout landed
// in the epoch-tail window in which the shred oracle closes the program by
// design: the oracle advances OpenForRequests → ClosedForRequests at slot
// first_slot_of_next_epoch − ProgramConfig.closed_for_requests_grace_period_slots,
// then settles seats and updates prices, and reopens only just after the
// epoch boundary. A timeout inside [close target, epoch boundary) is
// therefore expected roughly once per epoch, not an outage.
type EpochTailWindow struct {
	// Benign is true when the timeout is consistent with the by-design window:
	// the current slot is inside [CloseTargetSlot, NextEpochStartSlot), the
	// wait started at or after the close target (when known), and the program
	// is observably not in OpenForRequests phase.
	Benign bool
	// Epoch is the cluster's current epoch at classification time.
	Epoch uint64
	// CurrentSlot is the cluster's current absolute slot.
	CurrentSlot uint64
	// WaitStartSlot is the slot at which the timed-out wait began; 0 when the
	// caller could not read it (classification then uses CurrentSlot only).
	WaitStartSlot uint64
	// CloseTargetSlot is the slot at which the oracle closes the program for
	// this epoch's tail: NextEpochStartSlot − the onchain grace period.
	CloseTargetSlot uint64
	// NextEpochStartSlot is the first slot of the next epoch (the boundary at
	// which the oracle reopens the program).
	NextEpochStartSlot uint64
	// GracePeriodSlots is ProgramConfig.ClosedForRequestsGracePeriodSlots as
	// read onchain.
	GracePeriodSlots uint32
	// Phase is the execution controller phase at classification time.
	Phase shreds.ExecutionPhase
}

func (w EpochTailWindow) String() string {
	start := "unknown"
	if w.WaitStartSlot != 0 {
		start = strconv.FormatUint(w.WaitStartSlot, 10)
	}
	return fmt.Sprintf("epoch %d boundary at slot %d, close target slot %d (grace period %d slots), current slot %d, wait started at slot %s, program phase %q",
		w.Epoch, w.NextEpochStartSlot, w.CloseTargetSlot, w.GracePeriodSlots, w.CurrentSlot, start, w.Phase)
}

// classifyEpochTailWindow computes the epoch-tail close window from the
// cluster's epoch info (as returned by getEpochInfo) and the onchain grace
// period, and decides whether a timed-out wait is benign. Pure so it is
// unit-testable without an RPC. It errors — rather than guessing a
// classification — on inconsistent epoch info or a grace period that covers
// the whole epoch (a misclassification here would either page on-call for a
// benign window or, worse, silence a real outage).
//
// Three conditions must all hold for Benign:
//   - currentSlot is inside [close target, epoch boundary);
//   - waitStartSlot (when known) is at or after the close target: a timeout
//     means the program was closed for the entire wait, so a wait that began
//     before the close target observed the program closed before the oracle
//     was due to close it — an anomaly, not the benign window;
//   - the program is observably not in OpenForRequests phase: a timeout can
//     also be minutes of RPC read failures against a program that is in fact
//     open, which must keep failing loudly.
func classifyEpochTailWindow(epoch, currentSlot, slotIndex, slotsInEpoch uint64, gracePeriodSlots uint32, phase shreds.ExecutionPhase, waitStartSlot uint64) (EpochTailWindow, error) {
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
	inWindow := currentSlot >= closeTarget
	startedInWindow := waitStartSlot == 0 || waitStartSlot >= closeTarget
	closedNow := phase != shreds.ExecutionPhaseOpenForRequests
	return EpochTailWindow{
		Benign:             inWindow && startedInWindow && closedNow,
		Epoch:              epoch,
		CurrentSlot:        currentSlot,
		WaitStartSlot:      waitStartSlot,
		CloseTargetSlot:    closeTarget,
		NextEpochStartSlot: nextEpochStart,
		GracePeriodSlots:   gracePeriodSlots,
		Phase:              phase,
	}, nil
}

// solanaRPCClient returns the failover-pool-backed Solana RPC client when the
// pool is present, falling back to a single-endpoint client for hand-built
// test clients (mirrors shredsClient).
func (c *Client) solanaRPCClient() *rpc.Client {
	if c.solanaRPC != nil {
		return c.solanaRPC.RPC()
	}
	return rpc.New(c.SolanaRPCURL)
}

// CurrentSolanaSlot returns the target cluster's current slot. Used to record
// where a phase wait began so an eventual timeout can be classified against
// the epoch-tail window it actually spanned, not just where it ended.
func (c *Client) CurrentSolanaSlot(ctx context.Context) (uint64, error) {
	slot, err := c.solanaRPCClient().GetSlot(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		// Scrub: an RPC error can embed the (possibly API-keyed) endpoint URL.
		return 0, fmt.Errorf("failed to get current slot on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	return slot, nil
}

// EpochTailClosedWindow reads live chain state — the grace period from the
// shred-subscription ProgramConfig, the execution controller phase, and the
// epoch schedule position from the cluster's getEpochInfo — and classifies
// whether a wait that began at waitStartSlot (0 = unknown) and timed out now
// did so inside the epoch-tail window in which the program is closed by
// design. Everything is derived from the client's per-environment config, so
// nothing here is mainnet-specific. waitStartSlot of 0 degrades the
// classification to the timeout-time slot only.
func (c *Client) EpochTailClosedWindow(ctx context.Context, waitStartSlot uint64) (EpochTailWindow, error) {
	programID, err := solana.PublicKeyFromBase58(c.ShredSubscriptionProgramID)
	if err != nil {
		return EpochTailWindow{}, fmt.Errorf("failed to parse shred subscription program ID %q: %w", c.ShredSubscriptionProgramID, err)
	}

	// Rotate off a lagging endpoint before reading: the pool fails over on RPC
	// errors, not on stale-but-valid replies, and a lagging slot view here is
	// what decides whether a real outage pages or is skipped as benign.
	if c.solanaRPC != nil {
		c.solanaRPC.SelectHealthiestEndpoint(ctx)
	}

	shredsClient := c.shredsClient(programID)
	cfg, err := shredsClient.FetchProgramConfig(ctx)
	if err != nil {
		// Scrub: a fetch error can embed the (possibly API-keyed) endpoint URL.
		return EpochTailWindow{}, fmt.Errorf("failed to fetch program config on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	ec, err := shredsClient.FetchExecutionController(ctx)
	if err != nil {
		return EpochTailWindow{}, fmt.Errorf("failed to fetch execution controller on host %s: %s", c.Host, c.scrubRPCErr(err))
	}

	info, err := c.solanaRPCClient().GetEpochInfo(ctx, rpc.CommitmentConfirmed)
	if err != nil {
		return EpochTailWindow{}, fmt.Errorf("failed to get epoch info on host %s: %s", c.Host, c.scrubRPCErr(err))
	}
	if info == nil {
		return EpochTailWindow{}, fmt.Errorf("getEpochInfo returned no result on host %s", c.Host)
	}
	return classifyEpochTailWindow(info.Epoch, info.AbsoluteSlot, info.SlotIndex, info.SlotsInEpoch, cfg.ClosedForRequestsGracePeriodSlots, ec.GetPhase(), waitStartSlot)
}
