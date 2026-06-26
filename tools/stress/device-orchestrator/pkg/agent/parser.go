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
// Two diff shapes need to be handled. The containerized cEOS path emits the
// header itself as an addition ("+interface Tunnel500"), so one regex hit on
// the header line is enough. Real EOS on chi-dn-dzd5 emits the diff against
// the running-config: the "interface TunnelN" header lands unprefixed (or
// space-prefixed as a unified-diff context line) and the additions appear
// only on the property lines that follow ("+   description ..."). The parser
// runs a small per-section state machine so both shapes resolve to one
// EventPreCommitLog per tunnel section that contains any additions.
//
// A single Parser is goroutine-safe only against the calling Parse goroutine;
// callers should funnel all lines through one Parse loop.
type Parser struct {
	pending []uint16
	inDiff  bool // open between a "Committing..." marker and its finalize line

	// sectionTunnel is the most recently opened "interface TunnelN" section
	// in the running diff (0 = no section open). sectionPromoted tracks
	// whether the section has already produced an EventPreCommitLog; we
	// only emit one per section even though the section contains many `+`
	// property lines.
	sectionTunnel   uint16
	sectionPromoted bool

	now func() time.Time // injectable for tests
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
	// Receipt of a fresh config from the controller is an activity
	// signal. We match the "bytes" line specifically (the agent emits
	// both "lines" and "bytes" back-to-back per poll; one signal per
	// cycle is enough) and emit an EventConfigReceived so the
	// quiescence tracker doesn't time us out during the diff-check
	// window that follows.
	if configReceivedRE.MatchString(line) {
		return []Event{{Kind: EventConfigReceived, TunnelID: 0, At: p.now()}}
	}
	if m := committingRE.FindStringSubmatch(line); m != nil {
		// Open the diff block and scan the inline remainder of the marker line.
		// cEOS appends the first diff line after the colon; real EOS appends
		// the "--- file" header that we ignore.
		p.inDiff = true
		p.resetSection()
		return p.scanInlineAdditions(m[1])
	}
	if finalizedCommitRE.MatchString(line) {
		p.inDiff = false
		p.resetSection()
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
		// Abort cleared the session — drop pending Applieds without emitting,
		// but emit one EventCommitAborted so the quiescence tracker can
		// clear its pending-commit flag (set when the matching
		// `Received N bytes` line arrived).
		p.inDiff = false
		p.resetSection()
		p.pending = p.pending[:0]
		return []Event{{Kind: EventCommitAborted, TunnelID: 0, At: p.now()}}
	}
	if p.inDiff {
		return p.parseDiffLine(line)
	}
	return nil
}

// resetSection clears the in-flight interface-section bookkeeping.
func (p *Parser) resetSection() {
	p.sectionTunnel = 0
	p.sectionPromoted = false
}

// scanInlineAdditions handles the inline payload trailing the "Committing
// config session due to diffs detected:" marker. This is the cEOS shape,
// where the entire diff can land on one line. Real EOS only puts the
// "--- file" unified-diff header here; the diff body itself arrives on
// subsequent lines and is processed by parseDiffLine.
func (p *Parser) scanInlineAdditions(s string) []Event {
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

// parseDiffLine processes one line inside an open Committing diff block.
// It handles three shapes:
//
//   - cEOS additive header  "+interface TunnelN" / "+ interface TunnelN"
//     emits EventPreCommitLog directly and closes any open section.
//   - Real-EOS section header  "interface TunnelN" or " interface TunnelN"
//     (unified-diff context prefix) opens a section without emitting yet.
//   - Property addition inside a section  "+   description ..."  promotes
//     the section: emits EventPreCommitLog for the section's tunnel ID once.
//
// A " !" terminator (or the start of the next section) closes the current
// section without emitting if it had no `+` lines (pure-removal section).
func (p *Parser) parseDiffLine(line string) []Event {
	// cEOS shape: the header line itself is an addition. The added-tunnel
	// regex is anchored on `+\s*interface Tunnel`, so this only fires when
	// the section *header* is in the diff body — not on `+   description ...`
	// property lines (no "interface" token).
	if ids := extractAddedTunnelIDs(line); len(ids) > 0 {
		p.pending = append(p.pending, ids...)
		now := p.now()
		out := make([]Event, 0, len(ids))
		for _, id := range ids {
			out = append(out, Event{Kind: EventPreCommitLog, TunnelID: id, At: now})
		}
		// The cEOS shape declares a new interface block in its entirety;
		// any in-flight real-EOS section is no longer the relevant frame.
		p.resetSection()
		return out
	}
	// Real-EOS section header — context (space-prefixed) or unprefixed.
	if m := sectionStartRE.FindStringSubmatch(line); m != nil {
		id, err := strconv.ParseUint(m[1], 10, 16)
		if err != nil {
			// uint16 overflow on the section ID: drop it so we can't
			// later promote the wrong tunnel into pending.
			p.resetSection()
			return nil
		}
		p.sectionTunnel = uint16(id)
		p.sectionPromoted = false
		return nil
	}
	// Section terminator ("!" as a context line). Closes without emitting.
	if sectionTerminatorRE.MatchString(line) {
		p.resetSection()
		return nil
	}
	// Property addition inside an open section: promote once.
	if p.sectionTunnel != 0 && !p.sectionPromoted && addedPropertyRE.MatchString(line) {
		id := p.sectionTunnel
		p.sectionPromoted = true
		p.pending = append(p.pending, id)
		return []Event{{Kind: EventPreCommitLog, TunnelID: id, At: p.now()}}
	}
	return nil
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

	// configReceivedRE matches the "Received N bytes of configuration"
	// agent log line. The agent emits both a "lines" and a "bytes"
	// counterpart back-to-back per poll cycle; we anchor on bytes so
	// the EventConfigReceived signal fires exactly once per cycle.
	configReceivedRE = regexp.MustCompile(`Received \d+ bytes of configuration from controller`)

	// addedTunnelRE matches the cEOS additive header shape, where the
	// "interface TunnelN" line is itself diff-added: "+interface TunnelN"
	// or "+ interface TunnelN". The `\b` keeps "Tunnel50001" out of a
	// "Tunnel500" match.
	addedTunnelRE = regexp.MustCompile(`\+\s*interface Tunnel(\d+)\b`)

	// sectionStartRE matches the real-EOS section header inside a diff
	// body. EOS emits "interface TunnelN" with no prefix on the first
	// section and " interface TunnelN" (a unified-diff context prefix) on
	// subsequent sections when the interface already exists in
	// running-config but has no properties yet.
	sectionStartRE = regexp.MustCompile(`^[ ]?interface Tunnel(\d+)\b`)

	// addedPropertyRE matches a "+   description ..." (or other property)
	// addition line inside an interface section. The `\s+\S` rules out
	// the "+++ session:/..." unified-diff file marker (no whitespace
	// between the +s) and bare "+" lines.
	addedPropertyRE = regexp.MustCompile(`^\+\s+\S`)

	// sectionTerminatorRE matches the EOS "!" section terminator emitted
	// as a context line (" !"). cEOS prefixes its terminator with "+", so
	// it does not hit this regex — fine, because cEOS sections close
	// implicitly via the next addedTunnelRE-matched header.
	sectionTerminatorRE = regexp.MustCompile(`^[ ]!`)

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
