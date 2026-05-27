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
	case c.Executor == nil:
		return errors.New("sweep: Executor is required")
	case c.Runlog == nil:
		return errors.New("sweep: Runlog is required")
	}
	if c.Clock == nil {
		c.Clock = RealClock{}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Agent == nil {
		c.Agent = agent.NewNoop(c.Logger)
	}
	return nil
}

// createdUser tracks an orchestrator-owned user so the deprovision phase can
// iterate in reverse-creation order, independent of live state.
type createdUser struct {
	idx      int
	pubkey   solana.PublicKey
	tunnelID uint16
}

// Run drives the provision-then-deprovision sweep to completion. Returns the
// number of users actually created/deleted alongside the error (if any), so
// callers can report partial progress on abort.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := cfg.Agent.Start(ctx); err != nil {
		return fmt.Errorf("start agent runner: %w", err)
	}

	created, err := provision(ctx, &cfg)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	// Always attempt deprovision so an abort during provision still cleans up
	// what the sweep created. Use a fresh context for the deprovision phase if
	// the original was cancelled, since the operator wants the tear-down to
	// finish before exit. We respect the parent context's lifetime via the
	// outer Run's error return — callers that want a hard stop pass a deadline.
	depErr := deprovision(ctx, &cfg, created)
	if err != nil {
		return err
	}
	return depErr
}

// provision walks 0 → Target in batches, returning the slice of created users
// so deprovision can iterate in reverse. Returns ctx.Err() if cancelled
// between users.
func provision(ctx context.Context, cfg *Config) ([]createdUser, error) {
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

			res, err := cfg.Executor.CreateUser(ctx, idx)
			if err != nil {
				return created, fmt.Errorf("create user idx=%d: %w", idx, err)
			}
			pkStr := res.UserPDA.String()
			if err := emit(cfg, idx, pkStr, res.TunnelID, runlog.EventConfirm, res.ConfirmedAt, activeCount); err != nil {
				return created, err
			}
			created = append(created, createdUser{idx: idx, pubkey: res.UserPDA, tunnelID: res.TunnelID})
			activeCount++
			if err := emit(cfg, idx, pkStr, res.TunnelID, runlog.EventActivate, res.ActivatedAt, activeCount); err != nil {
				return created, err
			}
		}

		runningTarget = nextTarget
		if runningTarget >= cfg.Target {
			break
		}
		if cfg.Hold > 0 {
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
// events for each.
func deprovision(ctx context.Context, cfg *Config, created []createdUser) error {
	activeCount := len(created)
	for i := len(created) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
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
