package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// LinkHealthMode controls which time window LinkHealthCriterion evaluates.
type LinkHealthMode int

const (
	// LinkHealthModeImpairment checks the most recent link_rollup_5m bucket.
	// Used to detect impairment fast (RFS → Impaired transition).
	LinkHealthModeImpairment LinkHealthMode = iota
	// LinkHealthModeRecovery checks every bucket in the recovery window.
	// Used to gate recovery (Impaired → RFS) — every bucket must be clean.
	LinkHealthModeRecovery
)

// linkHealthRecentMaxAge is how recent the latest rollup bucket must be for
// the impairment check to act on it. Anything older is treated as "no data" —
// neither demoting nor recovering on stale telemetry. Sized at 3× the 5-minute
// rollup cadence to absorb a single missed bucket plus an ingest delay.
const linkHealthRecentMaxAge = 15 * time.Minute

// LinkHealthCriterion evaluates link impairment from the link_rollup_5m table.
// In ImpairmentMode it inspects the latest bucket; in RecoveryMode it requires
// every bucket in the recovery window (resolved via LinkBurnIn) to be clean.
//
// "Clean" means: isis_down=false AND a_loss_pct <= LossThreshold AND
// z_loss_pct <= LossThreshold. Buckets with provisioning=true are excluded
// to avoid flagging links that are still being brought up.
//
// "No data" handling differs by mode: in ImpairmentMode, missing data is
// treated as a pass (we cannot conclude a link is impaired without telemetry);
// in RecoveryMode, missing data is treated as a fail (we cannot conclude a
// link has been continuously clean without telemetry). The net effect is that
// a link without telemetry stays at its current health.
type LinkHealthCriterion struct {
	mode          LinkHealthMode
	checker       LinkHealthChecker
	lossThreshold float64
	log           *slog.Logger
}

func NewLinkHealthCriterion(mode LinkHealthMode, checker LinkHealthChecker, lossThreshold float64, log *slog.Logger) *LinkHealthCriterion {
	return &LinkHealthCriterion{
		mode:          mode,
		checker:       checker,
		lossThreshold: lossThreshold,
		log:           log,
	}
}

func (c *LinkHealthCriterion) Name() string {
	if c.mode == LinkHealthModeRecovery {
		return "link_health_recovery"
	}
	return "link_health_impairment"
}

func (c *LinkHealthCriterion) Check(ctx context.Context, link serviceability.Link) (bool, string) {
	pubkey := solana.PublicKeyFromBytes(link.PubKey[:]).String()

	if c.mode == LinkHealthModeRecovery {
		return c.checkRecovery(ctx, link, pubkey)
	}
	return c.checkImpairment(ctx, link, pubkey)
}

func (c *LinkHealthCriterion) checkImpairment(ctx context.Context, link serviceability.Link, pubkey string) (bool, string) {
	r, found, err := c.checker.LinkHealthRecent(ctx, pubkey)
	if err != nil {
		c.log.Error("Failed to query link health recent",
			"link", pubkey, "code", link.Code, "error", err)
		return false, fmt.Sprintf("clickhouse query failed: %v", err)
	}
	if !found {
		// No telemetry → cannot conclude impairment. Hold current health.
		return true, ""
	}

	// A stale latest bucket means the rollup pipeline is broken for this link;
	// treat it like "no data" rather than acting on a frozen snapshot. Without
	// this floor, a stuck pipeline at the moment of an ISIS flap would keep the
	// link Impaired indefinitely even after the link recovered.
	age := time.Since(r.BucketTs)
	if age > linkHealthRecentMaxAge {
		c.log.Debug("Link health latest bucket is stale; treating as no data",
			"link", pubkey, "code", link.Code,
			"bucketTs", r.BucketTs, "age", age, "maxAge", linkHealthRecentMaxAge)
		return true, ""
	}

	c.log.Debug("Link health recent",
		"link", pubkey, "code", link.Code,
		"bucketTs", r.BucketTs,
		"isisDown", r.IsisDown,
		"aLossPct", r.ALossPct,
		"zLossPct", r.ZLossPct,
		"lossThreshold", c.lossThreshold)

	if r.IsisDown {
		return false, fmt.Sprintf("isis adjacency down (bucket=%s)", r.BucketTs.UTC().Format(time.RFC3339))
	}
	if r.ALossPct > c.lossThreshold {
		return false, fmt.Sprintf("a-side loss %.2f%% > %.2f%% (bucket=%s)", r.ALossPct, c.lossThreshold, r.BucketTs.UTC().Format(time.RFC3339))
	}
	if r.ZLossPct > c.lossThreshold {
		return false, fmt.Sprintf("z-side loss %.2f%% > %.2f%% (bucket=%s)", r.ZLossPct, c.lossThreshold, r.BucketTs.UTC().Format(time.RFC3339))
	}
	return true, ""
}

func (c *LinkHealthCriterion) checkRecovery(ctx context.Context, link serviceability.Link, pubkey string) (bool, string) {
	start, now, expectedMinutes, ok := LinkBurnIn(ctx)
	if !ok {
		return false, "burn-in times not available in context"
	}
	// Zero-length window means we can't recover yet — keep at Impaired. (This
	// is the inverse of the device-side "expectedMinutes==0 ⇒ pass" rule:
	// here the window is a recovery dwell, not a burn-in.)
	if expectedMinutes == 0 {
		return false, "recovery window not yet established"
	}

	r, found, err := c.checker.LinkHealthWindowAllClean(ctx, pubkey, start, now, c.lossThreshold)
	if err != nil {
		c.log.Error("Failed to query link health recovery window",
			"link", pubkey, "code", link.Code, "error", err)
		return false, fmt.Sprintf("clickhouse query failed: %v", err)
	}
	if !found {
		return false, "no rollup data in recovery window"
	}

	c.log.Debug("Link health recovery window",
		"link", pubkey, "code", link.Code,
		"allClean", r.AllClean,
		"badBuckets", r.Bad,
		"totalBuckets", r.Total,
		"start", start,
		"end", now,
		"expectedMinutes", expectedMinutes,
		"lossThreshold", c.lossThreshold)

	if !r.AllClean {
		return false, fmt.Sprintf("recovery window has %d/%d impaired buckets", r.Bad, r.Total)
	}
	return true, ""
}
