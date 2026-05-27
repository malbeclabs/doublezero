// Package agent exposes the AgentRunner interface the orchestrator uses to
// drive doublezero-agent on a device under test (DUT). The skeleton ships a
// no-op implementation; the SSH-backed runner lands in part 3 of #3746.
package agent

import (
	"context"
	"log/slog"
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
//
// The SSH-backed implementation will manage an ssh.Session and parse stdout
// for the two log lines listed under EventKind.
type Runner interface {
	Start(ctx context.Context) error
	Events() <-chan Event
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
}

func (n *noop) Start(ctx context.Context) error {
	if n.log != nil {
		n.log.Debug("agent: noop runner started (no events will be emitted)")
	}
	go func() {
		<-ctx.Done()
		close(n.events)
	}()
	return nil
}

func (n *noop) Events() <-chan Event { return n.events }
