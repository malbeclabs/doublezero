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
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()
	if err := cfg.Agent.Start(agentCtx); err != nil {
		return fmt.Errorf("start agent runner: %w", err)
	}

	var consumerWG sync.WaitGroup
	consumerWG.Add(1)
	go func() {
		defer consumerWG.Done()
		consumeAgentEvents(&cfg, registry)
	}()

	created, err := provision(ctx, &cfg, registry)
	if err != nil && !errors.Is(err, context.Canceled) {
		// On a non-cancel error from provision we still want deprovision to
		// run (clean up what was created); the consumer keeps draining in
		// parallel so any straggling agent events for already-created users
		// still land in the runlog.
		_ = err
	}
	// Always attempt deprovision so an abort during provision still cleans up
	// what the sweep created. Teardown runs under a context derived with
	// WithoutCancel: an abort/signal that cancelled ctx must not also abandon
	// the users we already created, so deprovision ignores that cancellation
	// (it still inherits ctx's values). Callers wanting a hard stop on teardown
	// must enforce it out of band.
	depErr := deprovision(context.WithoutCancel(ctx), &cfg, created)

	// Tell the agent to stop and wait for the consumer goroutine to drain so
	// no events are dropped between deprovision-end and consumer-exit.
	agentCancel()
	consumerWG.Wait()

	if err != nil {
		return err
	}
	return depErr
}

// consumeAgentEvents reads from cfg.Agent.Events() until the channel closes
// and writes pre_commit_log / applied rows for tunnel IDs the sweep has
// registered. Events for unknown tunnel IDs are warn-logged and dropped — the
// most likely cause is a tunnel that belongs to a non-orchestrator user.
func consumeAgentEvents(cfg *Config, registry *tunnelRegistry) {
	for ev := range cfg.Agent.Events() {
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

// provision walks 0 → Target in batches, returning the slice of created users
// so deprovision can iterate in reverse. Returns ctx.Err() if cancelled
// between users. Each created user is also registered with the tunnel
// registry so the agent-event consumer can attribute pre_commit_log /
// applied events back to a user_index.
func provision(ctx context.Context, cfg *Config, registry *tunnelRegistry) ([]createdUser, error) {
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

		for i := 0; i < plan.ToCreate; i++ {
			if err := ctx.Err(); err != nil {
				return created, err
			}
			idx := activeCount
			submitAt := cfg.Clock.Now()
			if err := emit(cfg, idx, "", 0, runlog.EventSubmit, submitAt, activeCount); err != nil {
				return created, err
			}

			// Don't let an abort interrupt an in-flight create. A cancelled
			// CreateUser can return an error even after the transaction landed
			// onchain, which would orphan a user the deprovision phase never
			// learns about. Abort is observed at the iteration boundary above.
			res, err := cfg.Executor.CreateUser(context.WithoutCancel(ctx), idx)
			if err != nil {
				return created, fmt.Errorf("create user idx=%d: %w", idx, err)
			}
			pkStr := res.UserPDA.String()
			if err := emit(cfg, idx, pkStr, res.TunnelID, runlog.EventConfirm, res.ConfirmedAt, activeCount); err != nil {
				return created, err
			}
			cu := createdUser{idx: idx, pubkey: res.UserPDA, tunnelID: res.TunnelID}
			created = append(created, cu)
			registry.register(cu)
			activeCount++
			if err := emit(cfg, idx, pkStr, res.TunnelID, runlog.EventActivate, res.ActivatedAt, activeCount); err != nil {
				return created, err
			}
		}

		runningTarget = nextTarget
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

// deprovision walks the created slice in reverse, emitting deprovision_*
// events for each. It runs to completion regardless of ctx cancellation (Run
// passes an uncancellable teardown context) so an aborted sweep never leaks
// the users it created.
func deprovision(ctx context.Context, cfg *Config, created []createdUser) error {
	activeCount := len(created)
	for i := len(created) - 1; i >= 0; i-- {
		u := created[i]
		pkStr := u.pubkey.String()
		submitAt := cfg.Clock.Now()
		if err := emit(cfg, u.idx, pkStr, u.tunnelID, runlog.EventDeprovisionSubmit, submitAt, activeCount); err != nil {
			return err
		}

		res, err := cfg.Executor.DeleteUser(ctx, u.pubkey)
		if err != nil {
			return fmt.Errorf("delete user idx=%d pubkey=%s: %w", u.idx, pkStr, err)
		}
		if err := emit(cfg, u.idx, pkStr, u.tunnelID, runlog.EventDeprovisionConfirm, res.ConfirmedAt, activeCount); err != nil {
			return err
		}
		activeCount--
		if err := emit(cfg, u.idx, pkStr, u.tunnelID, runlog.EventDeprovisionActivate, res.ActivatedAt, activeCount); err != nil {
			return err
		}
	}
	return nil
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
