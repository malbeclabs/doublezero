package agent

import (
	"regexp"
	"strconv"
	"time"
)

// Parser turns lines from a doublezero-agent log stream into AgentEvents.
//
// It tracks two log lines from controlplane/agent/pkg/arista/eapi.go:
//
//   - "Committing config session due to diffs detected: <diff>"
//     opens a diff block. The agent logs the diff with a single log.Printf, but
//     Go's log package only prefixes the first line, so the body of the diff —
//     the "+ interface Tunnel<ID>" lines — arrives on the lines that *follow*
//     the marker. The parser therefore stays in the block and scans every
//     subsequent line, emitting one EventPreCommitLog per added tunnel and
//     remembering those IDs as "pending".
//   - "Configuration session finalized with command '... commit'"
//     → emit one EventApplied per pending ID, then close the block and clear
//     the buffer.
//   - "Configuration session finalized with command '... abort'"
//     → close the block and clear the buffer with no Applied events.
//
// A single Parser is goroutine-safe only against the calling Parse goroutine;
// callers should funnel all lines through one Parse loop.
type Parser struct {
	pending []uint16
	inDiff  bool             // open between a "Committing..." marker and its finalize line
	now     func() time.Time // injectable for tests
}

// NewParser returns a Parser that stamps events with the current wallclock.
// Pass WithClock to override (testing).
func NewParser(opts ...ParserOption) *Parser {
	p := &Parser{now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ParserOption configures NewParser.
type ParserOption func(*Parser)

// WithClock overrides time.Now for the parser; used by tests.
func WithClock(now func() time.Time) ParserOption {
	return func(p *Parser) { p.now = now }
}

// Parse advances the parser by one log line and returns any events produced.
// The returned slice is freshly allocated per call and safe for the caller to
// retain.
func (p *Parser) Parse(line string) []Event {
	if m := committingRE.FindStringSubmatch(line); m != nil {
		// Open the diff block and scan the inline remainder of the marker line
		// (the agent appends the first diff line after the colon).
		p.inDiff = true
		return p.scanAddedTunnels(m[1])
	}
	if finalizedCommitRE.MatchString(line) {
		p.inDiff = false
		now := p.now()
		// Always emit EventCommit so the consumer can see commit activity
		// during deprovision (pure-removal diffs leave p.pending empty).
		out := make([]Event, 0, len(p.pending)+1)
		out = append(out, Event{Kind: EventCommit, TunnelID: 0, At: now})
		for _, id := range p.pending {
			out = append(out, Event{Kind: EventApplied, TunnelID: id, At: now})
		}
		p.pending = p.pending[:0]
		return out
	}
	if finalizedAbortRE.MatchString(line) {
		// Abort cleared the session — drop pending without emitting Applied.
		p.inDiff = false
		p.pending = p.pending[:0]
		return nil
	}
	if p.inDiff {
		// A diff-body line inside an open commit block: the "+ interface
		// Tunnel<ID>" additions arrive here, one (or more) per line.
		return p.scanAddedTunnels(line)
	}
	return nil
}

// scanAddedTunnels emits one EventPreCommitLog per "+ interface Tunnel<ID>"
// found in s, recording the IDs as pending. Returns nil when s adds no tunnels
// (context lines, "- interface" deletions, or unrelated diff text).
func (p *Parser) scanAddedTunnels(s string) []Event {
	ids := extractAddedTunnelIDs(s)
	if len(ids) == 0 {
		return nil
	}
	p.pending = append(p.pending, ids...)
	now := p.now()
	out := make([]Event, 0, len(ids))
	for _, id := range ids {
		out = append(out, Event{Kind: EventPreCommitLog, TunnelID: id, At: now})
	}
	return out
}

// Pending exposes the in-flight tunnel IDs awaiting an Applied event; tests
// inspect this to assert state transitions.
func (p *Parser) Pending() []uint16 {
	out := make([]uint16, len(p.pending))
	copy(out, p.pending)
	return out
}

var (
	// committingRE matches the agent's pre-commit marker and captures whatever
	// trails the colon on that same line. Only the first diff line lands here:
	// Go's log package prefixes a timestamp to the marker line, and the rest of
	// the multi-line diff arrives on subsequent lines (handled via inDiff).
	committingRE = regexp.MustCompile(`Committing config session due to diffs detected:\s*(.*)$`)

	// addedTunnelRE matches an additive interface-Tunnel diff line; the `\b` on
	// the right keeps "Tunnel50001" out of a "Tunnel500" match.
	addedTunnelRE = regexp.MustCompile(`\+\s*interface Tunnel(\d+)\b`)

	// finalizedCommitRE matches the post-commit log line on a successful
	// commit. The quoted command always ends in "...commit" for actual commits
	// and "...abort" for no-op sessions.
	finalizedCommitRE = regexp.MustCompile(`Configuration session finalized with command '.*\s+commit'`)
	finalizedAbortRE  = regexp.MustCompile(`Configuration session finalized with command '.*\s+abort'`)
)

// extractAddedTunnelIDs pulls every "+ interface Tunnel<ID>" out of a diff
// payload. Returns nil when no additive lines are present (e.g., pure
// deprovision diffs).
func extractAddedTunnelIDs(diff string) []uint16 {
	matches := addedTunnelRE.FindAllStringSubmatch(diff, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]uint16, 0, len(matches))
	for _, m := range matches {
		id, err := strconv.ParseUint(m[1], 10, 16)
		if err != nil {
			continue
		}
		out = append(out, uint16(id))
	}
	return out
}
