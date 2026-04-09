package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type burnInTimesKey struct{}

// BurnInTimes holds the wall-clock start times for each burn-in category,
// resolved from ledger slot numbers via GetBlockTime once per tick.
type BurnInTimes struct {
	ProvisioningStart time.Time
	DrainedStart      time.Time
}

// DeviceBurnInStart returns the appropriate burn-in start time for the given device status.
func (b BurnInTimes) DeviceBurnInStart(status serviceability.DeviceStatus) time.Time {
	if status == serviceability.DeviceStatusDrained {
		return b.DrainedStart
	}
	return b.ProvisioningStart
}

// ContextWithBurnInTimes returns a new context carrying the given BurnInTimes.
func ContextWithBurnInTimes(ctx context.Context, times BurnInTimes) context.Context {
	return context.WithValue(ctx, burnInTimesKey{}, times)
}

// BurnInTimesFromContext extracts BurnInTimes from the context.
func BurnInTimesFromContext(ctx context.Context) (BurnInTimes, bool) {
	v, ok := ctx.Value(burnInTimesKey{}).(BurnInTimes)
	return v, ok
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

	// Stage 1: Pending/Unknown → ReadyForLinks
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
	devicePubkey := device.PubKey[:]
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

	linkPubkey := link.PubKey[:]
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
