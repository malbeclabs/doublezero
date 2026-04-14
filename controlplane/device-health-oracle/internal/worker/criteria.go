package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type burnInTimesKey struct{}

// BurnInTimes holds the wall-clock start times for each burn-in category,
// resolved from ledger slot numbers via GetBlockTime once per tick.
type BurnInTimes struct {
	ProvisioningStart time.Time
	DrainedStart      time.Time
	Now               time.Time // tick timestamp, used as the end of the burn-in window
}

// ContextWithBurnInTimes returns a new context carrying the given BurnInTimes.
func ContextWithBurnInTimes(ctx context.Context, times BurnInTimes) context.Context {
	return context.WithValue(ctx, burnInTimesKey{}, times)
}

// DeviceBurnIn extracts BurnInTimes from the context and returns the burn-in
// start time and expected number of minutes for the given device status.
// Returns ok=false if the context has no BurnInTimes, and expectedMinutes=0
// when the burn-in window has zero length (e.g. a newly created environment).
func DeviceBurnIn(ctx context.Context, status serviceability.DeviceStatus) (start time.Time, now time.Time, expectedMinutes int64, ok bool) {
	burnIn, ok := ctx.Value(burnInTimesKey{}).(BurnInTimes)
	if !ok {
		return time.Time{}, time.Time{}, 0, false
	}
	start = burnIn.ProvisioningStart
	if status.IsDrained() {
		start = burnIn.DrainedStart
	}
	expectedMinutes = max(int64(burnIn.Now.Sub(start).Minutes()), 0)
	return start, burnIn.Now, expectedMinutes, true
}

// DeviceCriterion evaluates whether a device meets a specific readiness requirement.
// Check returns (passed, reason). Reason is a human-readable explanation when passed is false.
type DeviceCriterion interface {
	Name() string
	Check(ctx context.Context, device serviceability.Device) (bool, string)
}

// LinkCriterion evaluates whether a link meets a specific readiness requirement.
type LinkCriterion interface {
	Name() string
	Check(ctx context.Context, link serviceability.Link) (bool, string)
}

// DeviceHealthEvaluator evaluates a device's health based on stage-specific criteria.
// Devices must progress through stages in order: Pending → ReadyForLinks → ReadyForUsers.
type DeviceHealthEvaluator struct {
	ReadyForLinksCriteria []DeviceCriterion
	ReadyForUsersCriteria []DeviceCriterion
	Log                   *slog.Logger
}

// Evaluate determines the target health for a device based on its current health and criteria results.
// It returns the device's current health if criteria are not met, or the next stage if they are.
func (e *DeviceHealthEvaluator) Evaluate(ctx context.Context, device serviceability.Device) serviceability.DeviceHealth {
	current := device.DeviceHealth

	// Already at highest level — nothing to do.
	if current == serviceability.DeviceHealthReadyForUsers {
		return current
	}

	// Stage 1: Pending/Unknown → ReadyForLinks.
	// Evaluate advances at most one stage per call, so a device needs a minimum
	// of two ticks (two worker intervals) to go from Pending to ReadyForUsers.
	if current < serviceability.DeviceHealthReadyForLinks {
		if !e.checkAll(ctx, device, e.ReadyForLinksCriteria) {
			return current
		}
		return serviceability.DeviceHealthReadyForLinks
	}

	// Stage 2: ReadyForLinks → ReadyForUsers
	// Re-check links criteria (device must still be calling controller) plus any user-specific criteria.
	if !e.checkAll(ctx, device, e.ReadyForLinksCriteria) {
		return current
	}
	if !e.checkAll(ctx, device, e.ReadyForUsersCriteria) {
		return current
	}
	return serviceability.DeviceHealthReadyForUsers
}

func (e *DeviceHealthEvaluator) checkAll(ctx context.Context, device serviceability.Device, criteria []DeviceCriterion) bool {
	devicePubkey := solana.PublicKeyFromBytes(device.PubKey[:]).String()
	for _, c := range criteria {
		passed, reason := c.Check(ctx, device)
		if !passed {
			e.Log.Info("Device criterion not met",
				"device", devicePubkey,
				"code", device.Code,
				"criterion", c.Name(),
				"reason", reason)
			MetricCriterionResults.WithLabelValues(c.Name(), "fail").Inc()
			return false
		}
		MetricCriterionResults.WithLabelValues(c.Name(), "pass").Inc()
	}
	return true
}

// LinkHealthEvaluator evaluates a link's health based on criteria.
// Links have a single stage: Pending → ReadyForService.
type LinkHealthEvaluator struct {
	ReadyForServiceCriteria []LinkCriterion
	Log                     *slog.Logger
}

// Evaluate determines the target health for a link based on its current health and criteria results.
func (e *LinkHealthEvaluator) Evaluate(ctx context.Context, link serviceability.Link) serviceability.LinkHealth {
	current := link.LinkHealth

	if current == serviceability.LinkHealthReadyForService {
		return current
	}

	linkPubkey := solana.PublicKeyFromBytes(link.PubKey[:]).String()
	for _, c := range e.ReadyForServiceCriteria {
		passed, reason := c.Check(ctx, link)
		if !passed {
			e.Log.Info("Link criterion not met",
				"link", linkPubkey,
				"code", link.Code,
				"criterion", c.Name(),
				"reason", reason)
			return current
		}
	}

	return serviceability.LinkHealthReadyForService
}
