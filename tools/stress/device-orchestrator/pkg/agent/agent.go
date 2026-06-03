// Package agent exposes the AgentRunner interface the orchestrator uses to
// drive doublezero-agent on a device under test (DUT). The skeleton ships a
// no-op implementation; the SSH-backed runner lands in part 3 of #3746.
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EventKind tags an AgentEvent so runlog row generation can map it onto the
// runlog Event vocabulary (`pre_commit_log`, `applied`).
type EventKind int

const (
	// EventPreCommitLog marks the moment the agent log shows
	// `Committing config session due to diffs detected: <diff>` for a new
	// tunnel interface; carries the parsed tunnel ID.
	EventPreCommitLog EventKind = iota + 1
	// EventApplied marks the moment the agent log shows a commit-success line
	// for a previously-pending tunnel interface.
	EventApplied
	// EventCommit marks the moment the agent finishes any successful commit
	// (the post-commit "Configuration session finalized with command
	// '... commit'" log line) — regardless of whether the diff added,
	// removed, or only modified tunnels. EventApplied only fires when the
	// diff added "+ interface Tunnel<ID>" lines, so deprovision commits emit
	// EventCommit with no accompanying Applieds. TunnelID is always 0.
	// Consumers use it as a generic "agent is doing work" signal — notably
	// the post-deprovision quiescence wait in sweep.Run, which would
	// otherwise see the agent as silent throughout the entire deprovision
	// phase and skip the wait.
	EventCommit
)

// Event is one observation emitted by the agent runner: a timestamped tunnel
// state transition derived from agent log lines.
type Event struct {
	Kind     EventKind
	TunnelID uint16
	At       time.Time
}

// Runner drives doublezero-agent on the DUT and surfaces tunnel-related events
// extracted from its log stream.
//
// Lifecycle:
//
//   - Start(ctx) blocks until the agent stream is healthy enough to emit
//     events (or returns an error). It returns immediately for the no-op impl.
//   - Events() returns a channel that closes when the runner exits.
//   - Err() reports the terminal error after Events() has closed: non-nil if
//     the runner stopped because its stream failed, nil if it shut down because
//     its context was cancelled. Only valid to read once Events() is closed.
//
// The SSH-backed implementation will manage an ssh.Session and parse stdout
// for the two log lines listed under EventKind.
type Runner interface {
	Start(ctx context.Context) error
	Events() <-chan Event
	Err() error
}

// NewNoop returns a Runner that never starts a process and never emits events.
// Used by the skeleton sweep loop and by tests where the agent isn't under test.
func NewNoop(log *slog.Logger) Runner {
	ch := make(chan Event)
	return &noop{log: log, events: ch}
}

type noop struct {
	log    *slog.Logger
	events chan Event
	once   sync.Once
}

// Start is idempotent: a second call is a no-op so the events channel is closed
// exactly once (a double close would panic).
func (n *noop) Start(ctx context.Context) error {
	n.once.Do(func() {
		if n.log != nil {
			n.log.Debug("agent: noop runner started (no events will be emitted)")
		}
		go func() {
			<-ctx.Done()
			close(n.events)
		}()
	})
	return nil
}

func (n *noop) Events() <-chan Event { return n.events }

// Err always returns nil: the no-op runner has no stream to fail.
func (n *noop) Err() error { return nil }
