// Package sweep implements the device-orchestrator sweep loop:
//
//   - Provision phase: walks 0 → Target users in batches of UsersPerBatch,
//     using reconcile.PlanFor to query live state and ask the Executor to
//     create the delta, holding for Hold between batches.
//   - Deprovision phase: walks Target → 0 in reverse order of creation,
//     so the youngest user is removed first.
//
// Per #3746, the sweep cooperates with the abort signal between user
// iterations — it never cancels a mid-flight Create/Delete.
package sweep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/agent"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/reconcile"
	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/runlog"
)

// Clock abstracts the wallclock for testability. Real callers pass RealClock;
// tests inject a fake that fires `After` channels manually.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the production wallclock implementation.
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// CreateResult captures the per-user details the sweep emits into the runlog
// for a successful provision. ConfirmedAt and ActivatedAt are sourced from
// the Executor so a future SDK refactor can give them distinct values; today
// they are typically equal because the SDK's `CreateUser` blocks on both
// finalization and account visibility before returning.
type CreateResult struct {
	UserPDA     solana.PublicKey
	TunnelID    uint16
	ConfirmedAt time.Time
	ActivatedAt time.Time
}

// DeleteResult is the deprovision analog of CreateResult.
type DeleteResult struct {
	ConfirmedAt time.Time
	ActivatedAt time.Time
}

// Executor is the interface the sweep depends on for chain I/O. Tests inject
// a fake; the real implementation wraps `serviceability.Executor` plus a small
// post-create fetch to discover the assigned TunnelId.
type Executor interface {
	ListUsers(ctx context.Context) ([]serviceability.User, error)
	CreateUser(ctx context.Context, idx int) (CreateResult, error)
	DeleteUser(ctx context.Context, userPDA solana.PublicKey) (DeleteResult, error)
}

// Config bundles all sweep parameters; pass by value to Run.
type Config struct {
	RunID         string
	Target        int
	UsersPerBatch int
	Hold          time.Duration
	OwnerFilter   solana.PublicKey

	// AgentQuietWindow is how long the agent must be silent (no observed
	// EventApplied) before Run treats deprovision teardown as complete and
	// cancels the SSH session. Zero disables the wait — Run cancels the agent
	// immediately after deprovision returns, which matches the pre-3796
	// behavior and is what tests use to avoid clock coupling. AgentQuiescenceTimeout
	// bounds the wait so a stuck agent can't pin teardown forever, and must be
	// > 0 whenever AgentQuietWindow > 0 (enforced by validate).
	//
	// Why this exists: the orchestrator's DeleteUser returns the moment the
	// deprovision txn finalizes onchain, but the agent applies that change to
	// EOS asynchronously — it polls the controller every 5s, builds a config
	// diff, and runs `configure session` (tens of seconds at high tunnel
	// counts). Cancelling the SSH session mid-commit leaves the deprovisioned
	// tunnels on the device. The wait closes the gap.
	AgentQuietWindow       time.Duration
	AgentQuiescenceTimeout time.Duration

	// ApplyCatchUpTimeout bounds how long Run waits between
	// provision-complete and deprovision-start for the agent's
	// `applied` event count to catch up to the provision-target
	// count. With hold=0 and a slow agent (>1 MB configs take ~40s
	// to diff-check) the orchestrator can finish provisioning all
	// 1024 users and start deleting them before the agent ever
	// applies the peak config — users get added and removed in the
	// same diff and never show up on the device. The wait blocks
	// until applied >= len(created) OR the timeout fires. Zero
	// disables the wait (the legacy behavior). A small grace count
	// (4) is allowed since the last batch's applied events lag by
	// roughly one poll cycle even on a healthy agent.
	ApplyCatchUpTimeout time.Duration

	// ApplyPerBatchCatchUp, when true, makes the provision phase pause
	// after every batch (not just at provision-complete) until the
	// agent's applied count covers the cumulative user count submitted
	// so far. This paces the orchestrator at the agent's throughput,
	// which matches production better than the current "flood the
	// agent" shape — users in real life arrive at human cadence, not
	// in 32/64-user bursts.
	//
	// Trade-off: per-batch waits dramatically reduce the diff-check
	// load per cycle (more, smaller cycles instead of fewer, larger
	// ones), which is the load-bearing measurement the harness was
	// built to surface. Off by default; flip on when measuring per-
	// user latency under steady-state load rather than peak throughput
	// under burst load.
	//
	// Each per-batch wait honors the same ApplyCatchUpTimeout as the
	// provision-complete wait; that knob's grace (applyCatchUpGrace)
	// still applies. With ApplyCatchUpTimeout == 0 this flag is a no-op.
	ApplyPerBatchCatchUp bool

	// NoAgent declares that the run is being driven without a real
	// agent (the caller wired up agent.NewNoop). Used by
	// waitForAgentQuiescence to skip the wait without inferring the
	// answer from tracker state — the previous inference races with
	// the consumer goroutine that updates the tracker, and at high
	// schedule pressure can fire even on a real-agent run that has
	// emitted events that the consumer hasn't yet drained. main.go
	// already turns this flag on when --no-agent is set, alongside
	// zeroing ApplyCatchUpTimeout.
	NoAgent bool

	Executor Executor
	Agent    agent.Runner
	Runlog   *runlog.Writer
	Clock    Clock
	Logger   *slog.Logger
}

func (c *Config) validate() error {
	switch {
	case c.Target < 0:
		return errors.New("sweep: Target must be >= 0")
	case c.UsersPerBatch <= 0:
		return errors.New("sweep: UsersPerBatch must be > 0")
	case c.Hold < 0:
		return errors.New("sweep: Hold must be >= 0")
	case c.AgentQuietWindow < 0:
		return errors.New("sweep: AgentQuietWindow must be >= 0")
	case c.AgentQuiescenceTimeout < 0:
		return errors.New("sweep: AgentQuiescenceTimeout must be >= 0")
	case c.ApplyCatchUpTimeout < 0:
		return errors.New("sweep: ApplyCatchUpTimeout must be >= 0")
	case c.AgentQuietWindow > 0 && c.AgentQuiescenceTimeout <= 0:
		// The wait loop only enforces the deadline when timeout > 0. A zero
		// timeout paired with a positive window would loop until ctx
		// cancellation — defeats the "stuck agent can't pin teardown" guarantee
		// the field exists for.
		return errors.New("sweep: AgentQuiescenceTimeout must be > 0 when AgentQuietWindow > 0")
	case c.RunID == "":
		return errors.New("sweep: RunID is required")
	case c.OwnerFilter.IsZero():
		return errors.New("sweep: OwnerFilter is required")
	case c.Executor == nil:
		return errors.New("sweep: Executor is required")
	case c.Runlog == nil:
		return errors.New("sweep: Runlog is required")
	}
	return nil
}

// applyDefaults fills the optional dependencies with production implementations.
// Kept separate from validate so validation stays free of mutation; Run calls
// it before validate (Agent's default depends on the resolved Logger).
func (c *Config) applyDefaults() {
	if c.Clock == nil {
		c.Clock = RealClock{}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Agent == nil {
		c.Agent = agent.NewNoop(c.Logger)
	}
}

// quiescenceTracker records the wall-clock time of the most recent observed
// post-commit agent event AND whether the agent is currently mid-cycle
// (a config has been received but not yet committed or aborted). The
// orchestrator polls it after deprovision returns to wait for the agent
// to finish converging EOS to the new (post-deprovision) config before
// cancelling the SSH session.
//
// Why both: post-commit signals (EventApplied / EventCommit) alone are
// insufficient because the agent goes silent during the diff-check
// window between EventConfigReceived and the next EventCommit. At >1 MB
// configs that window can exceed 30s — past the default 15s quiet
// window — so the tracker would falsely declare quiescence mid-cycle
// and the orchestrator would kill the SSH session before the
// deprovision config was committed to EOS. The pending-commit flag
// closes that gap: while it's true the wait blocks regardless of how
// long the agent has been silent.
type quiescenceTracker struct {
	lastEventNanos atomic.Int64
	// pendingCommit is true between an EventConfigReceived and the
	// next terminal cycle event (EventCommit, EventApplied, or — as
	// far as the tracker is concerned — a brand-new
	// EventConfigReceived, which implies the previous cycle finished
	// without an explicit commit).
	pendingCommit atomic.Bool
	// appliedCount counts EventApplied observations across the run.
	// Used by waitForAppliedToCatchUp at the provision→deprovision
	// boundary to ensure the agent has had time to apply the peak
	// config before the orchestrator starts removing users.
	appliedCount atomic.Int64
}

// markEvent records an agent activity beat that resets the silence
// timer. pending is true for events that *start* a new
// commit cycle (EventConfigReceived) and false for events that close
// it (EventCommit / EventApplied). The tracker uses pendingCommit to
// decide whether quiescence is safe — see HasPendingCommit.
func (q *quiescenceTracker) markEvent(at time.Time, pending bool) {
	q.lastEventNanos.Store(at.UnixNano())
	q.pendingCommit.Store(pending)
}

func (q *quiescenceTracker) lastEvent() (time.Time, bool) {
	n := q.lastEventNanos.Load()
	if n == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, n), true
}

// HasPendingCommit reports whether the agent has received a config it
// hasn't yet committed. While true the quiescence wait must block,
// regardless of how long the agent has been silent.
func (q *quiescenceTracker) HasPendingCommit() bool {
	return q.pendingCommit.Load()
}

// markApplied increments the per-run EventApplied counter. Called
// alongside markEvent for EventApplied observations.
func (q *quiescenceTracker) markApplied() {
	q.appliedCount.Add(1)
}

// AppliedCount returns the running total of EventApplied observations.
func (q *quiescenceTracker) AppliedCount() int64 {
	return q.appliedCount.Load()
}

// createdUser tracks an orchestrator-owned user so the deprovision phase can
// iterate in reverse-creation order, independent of live state.
type createdUser struct {
	idx      int
	pubkey   solana.PublicKey
	tunnelID uint16
}

// tunnelRegistry holds the orchestrator's tunnelID → user metadata mapping,
// shared between the provision goroutine (which writes) and the agent-event
// consumer goroutine (which reads). Lookups for unknown tunnel IDs return
// `ok=false` so the consumer can warn-log and drop the event.
type tunnelRegistry struct {
	mu  sync.RWMutex
	idx map[uint16]createdUser
}

func newTunnelRegistry() *tunnelRegistry {
	return &tunnelRegistry{idx: make(map[uint16]createdUser)}
}

func (r *tunnelRegistry) register(u createdUser) {
	if u.tunnelID == 0 {
		// TunnelId == 0 means the executor didn't surface a real ID; nothing
		// in the agent log can match it, so don't take a map slot.
		return
	}
	r.mu.Lock()
	r.idx[u.tunnelID] = u
	r.mu.Unlock()
}

func (r *tunnelRegistry) lookup(tunnelID uint16) (createdUser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.idx[tunnelID]
	return u, ok
}

// Run drives the provision-then-deprovision sweep to completion. Returns the
// number of users actually created/deleted alongside the error (if any), so
// callers can report partial progress on abort.
//
// Run additionally starts a goroutine that consumes events from cfg.Agent and
// writes pre_commit_log / applied runlog rows for tunnel IDs the sweep
// registered. The consumer exits when the agent's Events channel closes; we
// derive an agentCtx from ctx and cancel it after deprovision so the agent
// stops cleanly even on a successful run.
func Run(ctx context.Context, cfg Config) error {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	// Tag every sweep log line with the run ID.
	cfg.Logger = cfg.Logger.With("run_id", cfg.RunID)

	registry := newTunnelRegistry()
	tracker := &quiescenceTracker{}

	// runCtx lets the agent-event consumer abort the provisioning loop if the
	// agent stream dies. The agent is required telemetry for a run, so a broken
	// stream fails the run rather than silently degrading to missing runlog
	// rows. deprovision below still runs (it derives from the original ctx).
	runCtx, runCancel := context.WithCancelCause(ctx)
	defer runCancel(nil)

	agentCtx, agentCancel := context.WithCancel(runCtx)
	defer agentCancel()
	if err := cfg.Agent.Start(agentCtx); err != nil {
		return fmt.Errorf("start agent runner: %w", err)
	}

	var agentErr error
	var consumerWG sync.WaitGroup
	consumerWG.Add(1)
	go func() {
		defer consumerWG.Done()
		consumeAgentEvents(&cfg, registry, tracker)
		// Events() has closed. If it closed because the stream errored (rather
		// than our own agentCancel during teardown), abort the run so the
		// provisioning loop stops and Run reports the failure.
		if e := cfg.Agent.Err(); e != nil {
			agentErr = fmt.Errorf("agent stream: %w", e)
			runCancel(agentErr)
		}
	}()

	created, err := provision(runCtx, &cfg, registry, tracker)

	// Wait for the agent to apply the peak config before tearing down.
	// At 1k users / hold=0 the orchestrator's provision finishes ~40s
	// ahead of the agent's slowest diff-check + commit, so without this
	// wait deprovision starts removing users while the agent is still
	// trying to add them — users get added and removed in the same
	// running diff and never show up on the device. We only wait on
	// the success path: a provision error or context cancel means
	// we're aborting, and the goal there is to clean up quickly.
	if err == nil && cfg.ApplyCatchUpTimeout > 0 {
		waitForAppliedToCatchUp(runCtx, &cfg, tracker, len(created))
	}

	// Always attempt deprovision so a provision error (or an abort during
	// provision) still cleans up what the sweep created; the consumer keeps
	// draining in parallel so any straggling agent events for already-created
	// users still land in the runlog. Teardown runs under a context derived
	// with WithoutCancel: an abort/signal that cancelled ctx must not also
	// abandon the users we already created, so deprovision ignores that
	// cancellation (it still inherits ctx's values). Callers wanting a hard
	// stop on teardown must enforce it out of band.
	depErr := deprovision(context.WithoutCancel(ctx), &cfg, created)

	// Give the agent a chance to finish applying the deprovision config to
	// EOS before we kill its SSH session. We wait whenever deprovision
	// completed cleanly (`depErr == nil`) and the agent stream is healthy
	// (`agentErr == nil`) — including the abort path, where ctx was
	// cancelled by the observer's sentinel. The intent of the abort
	// triggers is "something off-device looks bad, tear down the run";
	// it isn't "kill the agent mid-commit". On the success path ctx is
	// not cancelled and the wait listens on it for Ctrl-C; on the abort
	// path ctx is already done, so we pass a derived context that
	// ignores cancellation. The hard `AgentQuiescenceTimeout` (default
	// 300s) caps the wait either way, and a user who really wants out
	// can re-Ctrl-C to kill the orchestrator process.
	if depErr == nil && agentErr == nil {
		waitCtx := ctx
		if ctx.Err() != nil {
			waitCtx = context.WithoutCancel(ctx)
		}
		waitForAgentQuiescence(waitCtx, &cfg, tracker)
	}

	// Tell the agent to stop and wait for the consumer goroutine to drain so
	// no events are dropped between deprovision-end and consumer-exit.
	agentCancel()
	consumerWG.Wait()

	// An agent stream failure takes precedence: it aborted the run, so report
	// it even though provision likely returned context.Canceled as a result.
	if agentErr != nil {
		return agentErr
	}
	if err != nil {
		return err
	}
	return depErr
}

// consumeAgentEvents reads from cfg.Agent.Events() until the channel closes
// and writes pre_commit_log / applied rows for tunnel IDs the sweep has
// registered. Events for unknown tunnel IDs are warn-logged and dropped — the
// most likely cause is a tunnel that belongs to a non-orchestrator user.
//
// Every observed EventApplied or EventCommit bumps the quiescence tracker so
// Run can wait for the agent to settle before teardown. EventCommit covers
// deprovision: pure-removal diffs emit no Applied events, but every
// successful commit (creation, removal, or modification) emits one
// EventCommit. Filtering by registered tunnels here would miss legitimate
// agent activity for orchestrator-adjacent tunnels, which is still "agent
// is doing work" for the purpose of timing the SSH cancel.
func consumeAgentEvents(cfg *Config, registry *tunnelRegistry, tracker *quiescenceTracker) {
	for ev := range cfg.Agent.Events() {
		// All three "agent is doing work" signals reset the silence
		// timer; only EventConfigReceived sets the pending-commit flag.
		// EventCommit and EventApplied are terminal — receiving them
		// means the commit cycle finished, so they clear pending.
		// EventApplied is per-tunnel and redundant with EventCommit
		// for tracker purposes, but feeding it through keeps the
		// silence timer accurate even when a commit's per-tunnel
		// Applieds arrive out-of-order with the Commit signal.
		switch ev.Kind {
		case agent.EventConfigReceived:
			tracker.markEvent(cfg.Clock.Now(), true)
		case agent.EventCommit, agent.EventApplied, agent.EventCommitAborted:
			// All three close a commit cycle from the tracker's
			// perspective: EventCommit and EventApplied report a
			// successful finalize; EventCommitAborted reports the
			// agent gave up on the session (no-op diff in steady-
			// state polling, most commonly post-deprovision).
			tracker.markEvent(cfg.Clock.Now(), false)
		}
		if ev.Kind == agent.EventApplied {
			tracker.markApplied()
		}
		if ev.Kind == agent.EventCommit || ev.Kind == agent.EventConfigReceived || ev.Kind == agent.EventCommitAborted {
			// Pure activity signals — no per-tunnel runlog row to emit.
			continue
		}
		u, ok := registry.lookup(ev.TunnelID)
		if !ok {
			cfg.Logger.Debug("sweep: agent event for unregistered tunnel; dropping",
				"tunnel_id", ev.TunnelID, "kind", ev.Kind)
			continue
		}
		var runlogEvent runlog.Event
		switch ev.Kind {
		case agent.EventPreCommitLog:
			runlogEvent = runlog.EventPreCommitLog
		case agent.EventApplied:
			runlogEvent = runlog.EventApplied
		default:
			continue
		}
		row := runlog.Row{
			RunID:       cfg.RunID,
			UserIndex:   u.idx,
			UserPubkey:  u.pubkey.String(),
			TunnelID:    u.tunnelID,
			Event:       runlogEvent,
			TNs:         ev.At.UnixNano(),
			NAfterEvent: 0, // active-count state is owned by the sweep goroutine and not safe to read here
		}
		if err := cfg.Runlog.Append(row); err != nil {
			cfg.Logger.Warn("sweep: runlog append failed for agent event",
				"err", err, "kind", runlogEvent, "tunnel_id", ev.TunnelID)
		}
	}
}

// waitForAgentQuiescence blocks until BOTH (a) the agent has been silent
// for cfg.AgentQuietWindow AND (b) at least cfg.AgentQuietWindow has
// elapsed since the wait started. The dual condition handles the case
// where the agent went silent BEFORE deprovision returned — the absolute
// "silent for N seconds" predicate alone would return instantly without
// giving the agent any time to commit the post-deprovision config push
// from the controller. The added "elapsed since wait start" floor
// guarantees we always block at least one quiet window after deprovision
// finishes, even if the agent had been silent for minutes during a slow
// commit cycle.
//
// The wait returns early on cfg.AgentQuiescenceTimeout (warned) or
// ctx.Done() (warned). It is a no-op when cfg.AgentQuietWindow is zero
// (the library default) or when cfg.NoAgent is true (the caller wired a
// noop runner). An older version of this function also skipped when
// tracker.lastEvent() reported no events; that inference raced with
// the consumer goroutine that updates the tracker (event sitting in
// the channel buffer but not yet observed) and could falsely fast-path
// during a real-agent run, killing the SSH session mid-commit. The
// explicit NoAgent flag from main.go replaces that inference.
//
// The wait polls in 1s ticks. Polling avoids needing a separate
// "applied observed" signal channel — the tracker's atomic.Int64 is
// read each tick and compared against the current clock. At 1024
// users / batch=32 the agent emits commits in clumps tens of seconds
// apart, so a 1s poll is well below the natural event cadence.
//
// When tracker.lastEvent() reports no events yet (zero-time), the
// loop still progresses correctly: sinceLast is "now - zero" which is
// trivially larger than AgentQuietWindow, so the wait blocks for
// AgentQuietWindow against the elapsed-since-start guard before
// returning. That gives the consumer goroutine a generous window to
// pick up any in-flight events before we declare quiescence.
func waitForAgentQuiescence(ctx context.Context, cfg *Config, tracker *quiescenceTracker) {
	if cfg.AgentQuietWindow <= 0 {
		return
	}
	if cfg.NoAgent {
		cfg.Logger.Info("sweep: --no-agent set; skipping agent quiescence wait")
		return
	}
	start := cfg.Clock.Now()
	deadline := start.Add(cfg.AgentQuiescenceTimeout)
	cfg.Logger.Info("sweep: waiting for agent to quiesce",
		"quiet_window", cfg.AgentQuietWindow,
		"timeout", cfg.AgentQuiescenceTimeout)
	for {
		now := cfg.Clock.Now()
		last, _ := tracker.lastEvent()
		sinceLast := now.Sub(last)
		elapsed := now.Sub(start)
		// Quiescence requires: silent for AgentQuietWindow AND wait
		// has been running at least AgentQuietWindow AND no commit
		// cycle is mid-flight (config received, not yet committed).
		// The last predicate prevents declaring quiescence during a
		// long diff-check window — the agent goes silent between
		// `Received N bytes` and the subsequent `Committing config
		// session` log line for the duration of the diff compute,
		// which at >1 MB configs can exceed AgentQuietWindow.
		if elapsed >= cfg.AgentQuietWindow && sinceLast >= cfg.AgentQuietWindow && !tracker.HasPendingCommit() {
			cfg.Logger.Info("sweep: agent quiesced",
				"quiet_for", sinceLast,
				"wait_elapsed", elapsed)
			return
		}
		if !now.Before(deadline) {
			cfg.Logger.Warn("sweep: agent quiescence timed out; proceeding with shutdown anyway",
				"wait_elapsed", elapsed,
				"since_last_event", sinceLast)
			return
		}
		select {
		case <-cfg.Clock.After(time.Second):
		case <-ctx.Done():
			cfg.Logger.Warn("sweep: agent quiescence wait cancelled", "err", ctx.Err())
			return
		}
	}
}

// waitForAppliedToCatchUp blocks at the provision→deprovision boundary
// until either (a) the agent has emitted at least `target` EventApplied
// observations OR (b) cfg.ApplyCatchUpTimeout elapses. The function is
// a no-op when cfg.ApplyCatchUpTimeout == 0 (the legacy behavior).
//
// We allow a small grace (`applyCatchUpGrace`) so the last batch's
// applied events, which lag by roughly one poll cycle even on a healthy
// agent, don't pin the wait to its timeout. `target` is len(created):
// the orchestrator's count of users it actually provisioned (excludes
// users the executor declined to create).
//
// The wait returns silently on hit, warns on ctx cancel or timeout, and
// never fails the run — deprovision still runs afterwards regardless.
const applyCatchUpGrace = int64(4)

func waitForAppliedToCatchUp(ctx context.Context, cfg *Config, tracker *quiescenceTracker, target int) {
	if cfg.ApplyCatchUpTimeout <= 0 || target == 0 {
		return
	}
	start := cfg.Clock.Now()
	deadline := start.Add(cfg.ApplyCatchUpTimeout)
	cfg.Logger.Info("sweep: waiting for agent applied to catch up to provision",
		"target", target,
		"applied", tracker.AppliedCount(),
		"grace", applyCatchUpGrace,
		"timeout", cfg.ApplyCatchUpTimeout)
	for {
		applied := tracker.AppliedCount()
		if applied+applyCatchUpGrace >= int64(target) {
			cfg.Logger.Info("sweep: applied caught up",
				"applied", applied,
				"target", target,
				"elapsed", cfg.Clock.Now().Sub(start))
			return
		}
		now := cfg.Clock.Now()
		if !now.Before(deadline) {
			cfg.Logger.Warn("sweep: apply catch-up timed out; proceeding with deprovision anyway",
				"applied", applied,
				"target", target,
				"shortfall", int64(target)-applied,
				"elapsed", now.Sub(start))
			return
		}
		select {
		case <-cfg.Clock.After(time.Second):
		case <-ctx.Done():
			cfg.Logger.Warn("sweep: apply catch-up wait cancelled", "err", ctx.Err())
			return
		}
	}
}

// provision walks 0 → Target in batches, returning the slice of created users
// so deprovision can iterate in reverse. Returns ctx.Err() if cancelled
// between batches. Within each batch, all UsersPerBatch creates run
// concurrently — finalization is ~14 s per CreateUser and dominates wall
// time, so pipelining a batch of N drops the per-batch cost from N × 14 s
// to ~14 s. Each created user is registered with the tunnel registry so
// the agent-event consumer can attribute pre_commit_log / applied events
// back to a user_index.
func provision(ctx context.Context, cfg *Config, registry *tunnelRegistry, tracker *quiescenceTracker) ([]createdUser, error) {
	if cfg.Target == 0 {
		return nil, nil
	}
	var created []createdUser
	runningTarget := 0
	activeCount := 0

	for runningTarget < cfg.Target {
		if err := ctx.Err(); err != nil {
			return created, err
		}

		nextTarget := runningTarget + cfg.UsersPerBatch
		if nextTarget > cfg.Target {
			nextTarget = cfg.Target
		}

		users, err := cfg.Executor.ListUsers(ctx)
		if err != nil {
			return created, fmt.Errorf("list users for batch starting at %d: %w", activeCount, err)
		}
		plan := reconcile.PlanFor(users, nextTarget, cfg.OwnerFilter)
		if len(plan.ToDelete) > 0 {
			cfg.Logger.Warn("sweep: PlanFor wants to delete pre-existing users; skipping (orchestrator only creates this run)",
				"count", len(plan.ToDelete))
		}

		newUsers, newActive, err := provisionBatch(ctx, cfg, registry, activeCount, plan.ToCreate)
		created = append(created, newUsers...)
		activeCount = newActive
		if err != nil {
			return created, err
		}

		runningTarget = nextTarget
		// Per-batch catch-up: pace the orchestrator at the agent's
		// throughput so each batch's users land on the device before
		// the next batch is submitted. Skipped on the final batch
		// (the provision-complete wait covers it) and when the
		// flag's off. Honors the same ApplyCatchUpTimeout as the
		// provision-complete wait.
		if cfg.ApplyPerBatchCatchUp && cfg.ApplyCatchUpTimeout > 0 && runningTarget < cfg.Target {
			waitForAppliedToCatchUp(ctx, cfg, tracker, len(created))
			if err := ctx.Err(); err != nil {
				return created, err
			}
		}
		if runningTarget >= cfg.Target {
			break
		}
		// Only hold when this batch actually created users; a no-op batch
		// (target already satisfied by pre-existing state) shouldn't burn the
		// hold interval.
		if plan.ToCreate > 0 && cfg.Hold > 0 {
			select {
			case <-cfg.Clock.After(cfg.Hold):
			case <-ctx.Done():
				return created, ctx.Err()
			}
		}
	}
	return created, nil
}

// provisionBatch fires `count` concurrent CreateUser calls assigned indexes
// [baseIdx, baseIdx+count). It emits submit events for every user before
// launching the goroutines (so the runlog captures submission order and
// wall-clock baseline), then confirm/activate pairs in idx order after every
// goroutine completes. Returns the newly-created users, the updated active
// count, and the first error observed.
func provisionBatch(ctx context.Context, cfg *Config, registry *tunnelRegistry, baseIdx, count int) ([]createdUser, int, error) {
	if count <= 0 {
		return nil, baseIdx, nil
	}

	// Emit all submit events up-front so their timestamps reflect when the
	// batch was actually dispatched, not when each goroutine completed.
	for i := 0; i < count; i++ {
		idx := baseIdx + i
		submitAt := cfg.Clock.Now()
		// activeCount on submit is the count *before* this user activates.
		// We pass baseIdx (the count before the batch started) so all submits
		// in a batch report the same pre-batch active count, matching the
		// observable state at the time the user was submitted.
		if err := emit(cfg, idx, "", 0, runlog.EventSubmit, submitAt, baseIdx); err != nil {
			return nil, baseIdx, err
		}
	}

	type outcome struct {
		res CreateResult
		err error
	}
	outcomes := make([]outcome, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		i, idx := i, baseIdx+i
		go func() {
			defer wg.Done()
			// Don't let an abort interrupt an in-flight create. A cancelled
			// CreateUser can return an error even after the transaction landed
			// onchain, which would orphan a user the deprovision phase never
			// learns about. Abort is observed between batches in provision().
			res, err := cfg.Executor.CreateUser(context.WithoutCancel(ctx), idx)
			outcomes[i] = outcome{res: res, err: err}
		}()
	}
	wg.Wait()

	// Iterate in idx order so the runlog stays user-deterministic; collect
	// the first error after registering all successes so deprovision can clean
	// up everything the batch actually committed.
	var created []createdUser
	activeCount := baseIdx
	var firstErr error
	for i := 0; i < count; i++ {
		idx := baseIdx + i
		o := outcomes[i]
		if o.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("create user idx=%d: %w", idx, o.err)
			}
			continue
		}
		pkStr := o.res.UserPDA.String()
		if err := emit(cfg, idx, pkStr, o.res.TunnelID, runlog.EventConfirm, o.res.ConfirmedAt, activeCount); err != nil {
			return created, activeCount, err
		}
		cu := createdUser{idx: idx, pubkey: o.res.UserPDA, tunnelID: o.res.TunnelID}
		created = append(created, cu)
		registry.register(cu)
		activeCount++
		if err := emit(cfg, idx, pkStr, o.res.TunnelID, runlog.EventActivate, o.res.ActivatedAt, activeCount); err != nil {
			return created, activeCount, err
		}
	}
	return created, activeCount, firstErr
}

// deprovision walks the created slice in reverse, processing each batch of
// UsersPerBatch concurrently (same pipelining rationale as provision). It
// runs to completion regardless of ctx cancellation (Run passes an
// uncancellable teardown context) so an aborted sweep never leaks the users
// it created.
func deprovision(ctx context.Context, cfg *Config, created []createdUser) error {
	if len(created) == 0 {
		return nil
	}
	activeCount := len(created)
	batchSize := cfg.UsersPerBatch
	if batchSize <= 0 {
		batchSize = 1
	}
	// Walk created in reverse-creation order, processing one batch per loop.
	for end := len(created); end > 0; {
		start := end - batchSize
		if start < 0 {
			start = 0
		}
		// batch[0] is the newest user in the slice (highest idx); deleting
		// in reverse-creation order means we process [end-1, end-2, …, start].
		batch := make([]createdUser, end-start)
		for i := range batch {
			batch[i] = created[end-1-i]
		}
		newActive, err := deprovisionBatch(ctx, cfg, batch, activeCount)
		activeCount = newActive
		if err != nil {
			return err
		}
		end = start
	}
	return nil
}

// deprovisionBatch fires len(batch) concurrent DeleteUsers. It emits
// deprovision_submit events for every user before launching, then
// deprovision_confirm/deprovision_activate pairs in the order given by
// `batch` (which the caller orders newest-first to preserve the
// reverse-creation contract).
func deprovisionBatch(ctx context.Context, cfg *Config, batch []createdUser, activeCountIn int) (int, error) {
	if len(batch) == 0 {
		return activeCountIn, nil
	}

	for _, u := range batch {
		submitAt := cfg.Clock.Now()
		if err := emit(cfg, u.idx, u.pubkey.String(), u.tunnelID, runlog.EventDeprovisionSubmit, submitAt, activeCountIn); err != nil {
			return activeCountIn, err
		}
	}

	type outcome struct {
		res DeleteResult
		err error
	}
	outcomes := make([]outcome, len(batch))
	var wg sync.WaitGroup
	wg.Add(len(batch))
	for i := range batch {
		i := i
		u := batch[i]
		go func() {
			defer wg.Done()
			res, err := cfg.Executor.DeleteUser(ctx, u.pubkey)
			outcomes[i] = outcome{res: res, err: err}
		}()
	}
	wg.Wait()

	activeCount := activeCountIn
	for i, u := range batch {
		o := outcomes[i]
		if o.err != nil {
			return activeCount, fmt.Errorf("delete user idx=%d pubkey=%s: %w", u.idx, u.pubkey.String(), o.err)
		}
		pkStr := u.pubkey.String()
		if err := emit(cfg, u.idx, pkStr, u.tunnelID, runlog.EventDeprovisionConfirm, o.res.ConfirmedAt, activeCount); err != nil {
			return activeCount, err
		}
		activeCount--
		if err := emit(cfg, u.idx, pkStr, u.tunnelID, runlog.EventDeprovisionActivate, o.res.ActivatedAt, activeCount); err != nil {
			return activeCount, err
		}
	}
	return activeCount, nil
}

func emit(cfg *Config, idx int, pubkey string, tunnelID uint16, ev runlog.Event, at time.Time, nAfter int) error {
	if at.IsZero() {
		at = cfg.Clock.Now()
	}
	row := runlog.Row{
		RunID:       cfg.RunID,
		UserIndex:   idx,
		UserPubkey:  pubkey,
		TunnelID:    tunnelID,
		Event:       ev,
		TNs:         at.UnixNano(),
		NAfterEvent: nAfter,
	}
	if err := cfg.Runlog.Append(row); err != nil {
		return fmt.Errorf("runlog append %s: %w", ev, err)
	}
	return nil
}
