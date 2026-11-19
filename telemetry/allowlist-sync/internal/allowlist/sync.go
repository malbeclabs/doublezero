package allowlist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

// Fail-safe limits (spec section 9): a pass that would remove more than this
// many devices, or more than this share of the addresses, from any target is
// refused.
const (
	MaxRemovedDevices      = 5
	MaxRemovedAddrsPercent = 10
)

// DefaultReportTimeout bounds the caller address read, so a ClickHouse that
// accepts the connection and stalls cannot use up the pass and stop the list.
const DefaultReportTimeout = 20 * time.Second

// Refusal reasons, the refused_passes_total label.
const (
	RefusedEmpty          = "empty_list"
	RefusedTooManyDevices = "too_many_devices_removed"
	RefusedTooManyAddrs   = "too_many_addresses_removed"
)

type LedgerReader interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type Config struct {
	Log     *slog.Logger
	Clock   clockwork.Clock
	Metrics *Metrics

	Ledger         LedgerReader
	ExceptionsFile string
	Callers        CallerReader
	// CallerWindow is how far back the report reads calls: a live device with
	// no call from its own addresses in it reads 0.
	CallerWindow time.Duration
	// Targets are written in order, gnmic's map first: gnmic refuses a pubkey
	// with no entry (spec section 5, step 3). A target that fails to read or
	// write does not stop the others: Cloudflare is still written when the
	// map cannot be, and the failed target is tried again on the next pass.
	Targets []Target

	Interval    time.Duration
	PassTimeout time.Duration
	// ReportTimeout bounds the caller address read, and separately each
	// pass's AOP reads; zero means DefaultReportTimeout.
	ReportTimeout time.Duration
	// DryRun computes and logs each pass's changes and writes nothing.
	DryRun bool
	// AllowShrinkTo is the digest of one list that may be applied although a
	// removal limit refuses it. The refusal logs the digest to set. It allows
	// that exact list and no other, so leaving it set does no harm.
	AllowShrinkTo string

	// AOP reads the AOP setting of each of AOPHostnames after every pass, for
	// the certificate expiry alert. Without AOP nothing is read.
	AOP          AOPReader
	AOPHostnames []string
}

type Syncer struct {
	cfg Config
	// warned holds the unlisted callers already logged, so each is logged once
	// while it keeps calling, not on every pass.
	warned map[string]bool
}

func New(cfg Config) (*Syncer, error) {
	if cfg.Log == nil || cfg.Clock == nil || cfg.Metrics == nil || cfg.Ledger == nil || cfg.Callers == nil {
		return nil, errors.New("log, clock, metrics, ledger and callers are required")
	}
	if cfg.ExceptionsFile == "" {
		return nil, errors.New("exceptions file is required")
	}
	if len(cfg.Targets) == 0 {
		return nil, errors.New("at least one target is required")
	}
	if cfg.Interval <= 0 || cfg.PassTimeout <= 0 || cfg.CallerWindow <= 0 {
		return nil, errors.New("interval, pass timeout and caller window must be positive")
	}
	dryRun := 0.0
	if cfg.DryRun {
		dryRun = 1
	}
	cfg.Metrics.DryRun.Set(dryRun)
	if cfg.ReportTimeout <= 0 {
		cfg.ReportTimeout = DefaultReportTimeout
	}
	// The caller read runs inside the pass, before the writes, so a stalled
	// read must leave the pass time to write.
	if cfg.PassTimeout <= cfg.ReportTimeout {
		return nil, fmt.Errorf("pass timeout %s must be longer than the report timeout %s", cfg.PassTimeout, cfg.ReportTimeout)
	}
	return &Syncer{cfg: cfg}, nil
}

// Run passes once at start and then every interval until ctx is done.
func (s *Syncer) Run(ctx context.Context) error {
	ticker := s.cfg.Clock.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		s.runPass(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
		}
	}
}

func (s *Syncer) runPass(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, s.cfg.PassTimeout)
	defer cancel()
	if err := s.Pass(pctx); err != nil && ctx.Err() == nil {
		s.cfg.Log.Error("allowlist pass failed or was refused; the next pass tries again", "error", err)
	}
	if ctx.Err() == nil {
		s.checkAOP(ctx)
	}
}

// RefusedError is a pass a fail-safe refused.
type RefusedError struct {
	Reason string
	Detail string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("pass refused (%s): %s", e.Reason, e.Detail)
}

// Pass reads, builds, checks and applies once. It writes nothing unless the
// ledger and exception file read whole and the list is not empty. A target it
// cannot read is skipped, and the others get only the list's additions: the
// removal limits are checked against every target, and one unread could not
// stop a removal. When a target would lose more than the removal limits allow,
// and AllowShrinkTo does not name the list, the pass is refused: every target
// gets the list's additions and keeps everything it holds.
func (s *Syncer) Pass(ctx context.Context) error {
	m := s.cfg.Metrics
	fail := func(stage string, err error) error {
		m.Errors.WithLabelValues(stage).Inc()
		return fmt.Errorf("%s: %w", stage, err)
	}

	pd, err := s.cfg.Ledger.GetProgramData(ctx)
	if err != nil {
		return fail("ledger", err)
	}
	if err := checkComplete(pd); err != nil {
		return fail("ledger", err)
	}
	exc, err := LoadExceptions(s.cfg.ExceptionsFile)
	if err != nil {
		return fail("exceptions", err)
	}
	built := Build(pd.Devices, exc)
	m.setBuilt(built)
	for _, r := range built.Review {
		s.cfg.Log.Warn("exception file entry needs review", "kind", r.Kind, "pubkey", r.Pubkey, "code", r.Code, "address", r.Address, "reason", r.Reason)
	}

	// The report never blocks the list: an outside caller cannot stop the
	// sync, and a ClickHouse outage does not freeze it.
	s.report(ctx, built)

	list := built.List
	if len(list.Addrs()) == 0 {
		return s.refuse(&RefusedError{Reason: RefusedEmpty, Detail: "the list has no address"})
	}

	type planned struct {
		target Target
		plan   Plan
	}
	var errs []error
	plans := make([]planned, 0, len(s.cfg.Targets))
	for _, t := range s.cfg.Targets {
		p, err := t.Plan(ctx, list)
		if err != nil {
			errs = append(errs, fail("read_"+t.Name(), err))
			continue
		}
		if len(p.ExternalAdded) > 0 || len(p.ExternalRemoved) > 0 {
			m.ExternalChanges.WithLabelValues(p.Target).Inc()
			s.cfg.Log.Warn("target changed since the last pass by a write the sync did not make",
				"target", p.Target, "added_or_changed", p.ExternalAdded, "removed", p.ExternalRemoved)
		}
		plans = append(plans, planned{t, p})
	}

	// A refusal holds every target's removals, not only the refused target's,
	// so the targets never disagree about a device the ledger dropped.
	digest := list.Digest()
	var refusal *RefusedError
	for _, p := range plans {
		r := checkRemovals(p.plan)
		switch {
		case r == nil:
		case s.cfg.AllowShrinkTo == digest:
			s.cfg.Log.Warn("removal limit overridden for this list", "target", p.plan.Target, "reason", r.Reason, "detail", r.Detail, "digest", digest)
		case refusal == nil:
			refusal = r
		}
	}
	if refusal != nil {
		refusal.Detail += fmt.Sprintf("; additions are written and every removal is held; after checking the ledger, set --allow-shrink-to=%s to apply this list once", digest)
	}
	// An unread target could not be checked: only the map counts devices, so
	// without it Cloudflare alone could lose 10% of the fleet.
	if refusal != nil || len(plans) < len(s.cfg.Targets) {
		for i := range plans {
			plans[i].plan = plans[i].target.HoldRemovals(plans[i].plan)
		}
	}

	for _, pl := range plans {
		p := pl.plan
		log := s.cfg.Log.With("target", p.Target, "dry_run", s.cfg.DryRun)
		if !p.Changed {
			log.Debug("target unchanged")
			continue
		}
		log.Info("target changes",
			"added", p.AddedAddrs, "removed", p.RemovedAddrs, "removed_other", p.RemovedOther,
			"removed_devices", p.RemovedDevices, "addresses", len(list.Addrs()), "devices", len(list.Devices))
		if s.cfg.DryRun {
			continue
		}
		if err := pl.target.Apply(ctx, p); err != nil {
			m.Errors.WithLabelValues("write_" + p.Target).Inc()
			errs = append(errs, fmt.Errorf("write %s: %w", p.Target, err))
			continue
		}
		m.Writes.WithLabelValues(p.Target).Inc()
	}
	if refusal != nil {
		errs = append(errs, s.refuse(refusal))
	}
	if len(errs) > 0 {
		// Retried on the next pass: each target is compared with what it
		// holds then.
		return errors.Join(errs...)
	}
	m.LastSuccess.Set(unixOrZero(s.cfg.Clock.Now()))
	return nil
}

func (s *Syncer) refuse(r *RefusedError) error {
	s.cfg.Metrics.RefusedPasses.WithLabelValues(r.Reason).Inc()
	return r
}

// checkRemovals applies the removal limits to one target's plan. A target
// that holds nothing yet has nothing to lose.
func checkRemovals(p Plan) *RefusedError {
	if p.TracksDevices && len(p.RemovedDevices) > MaxRemovedDevices {
		return &RefusedError{Reason: RefusedTooManyDevices, Detail: fmt.Sprintf(
			"%s would remove %d of %d devices (limit %d): %v", p.Target, len(p.RemovedDevices), p.PrevDevices, MaxRemovedDevices, p.RemovedDevices)}
	}
	if removed := p.Removed(); removed*100 > p.PrevAddrs*MaxRemovedAddrsPercent {
		return &RefusedError{Reason: RefusedTooManyAddrs, Detail: fmt.Sprintf(
			"%s would remove %d of %d addresses (limit %d%%): %v %v", p.Target, removed, p.PrevAddrs, MaxRemovedAddrsPercent, p.RemovedAddrs, p.RemovedOther)}
	}
	return nil
}

func (s *Syncer) report(ctx context.Context, b Built) {
	now := s.cfg.Clock.Now()
	rctx, cancel := context.WithTimeout(ctx, s.cfg.ReportTimeout)
	defer cancel()
	calls, cut, err := s.cfg.Callers.RecentCalls(rctx, now.Add(-s.cfg.CallerWindow))
	if err != nil {
		// The report gauges keep their last values; report_last_success
		// says how old they are.
		s.cfg.Metrics.Errors.WithLabelValues("report").Inc()
		s.cfg.Log.Error("reading caller addresses failed; the report is stale", "error", err)
		return
	}
	if cut {
		s.cfg.Log.Warn("caller read reached its limit; the report holds only the newest calls, and a flood of callers is the likely cause", "limit", MaxCallerRows, "oldest_kept", calls[len(calls)-1].LastSeen)
		s.cfg.Metrics.CallerReadCut.Set(1)
	} else {
		s.cfg.Metrics.CallerReadCut.Set(0)
	}
	r := BuildReport(b, calls)
	s.cfg.Metrics.setReport(b, r)
	s.cfg.Metrics.ReportLastSuccess.Set(unixOrZero(now))
	warned := make(map[string]bool)
	for _, c := range r.Offchain {
		if c.Listed {
			continue
		}
		key := c.Pubkey + "|" + c.CallerIP.String()
		if !s.warned[key] {
			s.cfg.Log.Warn("device calls from an address not on its entry", "pubkey", c.Pubkey, "code", c.Code, "caller_ip", c.CallerIP, "last_seen", c.LastSeen)
		}
		warned[key] = true
	}
	s.warned = warned
	if r.OffchainDropped > 0 {
		s.cfg.Log.Warn("calling addresses over the per-device limit left out of the report", "dropped", r.OffchainDropped, "limit", MaxOffchainCallersPerDevice)
	}
}

func unixOrZero(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}
