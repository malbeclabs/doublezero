package parser

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AgentCycle is one commit cycle as observed in orchestrator.agent.log:
// the agent receives a config from the controller, opens a configure
// session, then either commits or aborts. ReceivedAt / ReceivedLines /
// ReceivedBytes are filled in from the immediately preceding "Received N
// lines / N bytes" pair when one is present (the agent emits both lines
// back-to-back on each poll cycle).
type AgentCycle struct {
	// ReceivedAt is the wall-clock time of the matched
	// `Received N lines of configuration` line; zero if no pair preceded
	// the commit marker (e.g. the agent restarted mid-cycle).
	ReceivedAt    time.Time
	ReceivedLines int
	ReceivedBytes int
	// CommitStartedAt is the wall-clock time of the
	// `Committing config session due to diffs detected:` line.
	CommitStartedAt time.Time
	// FinalizedAt is the wall-clock time of the
	// `Configuration session finalized with command '... commit'` (or
	// `'... abort'`) line. Zero if the cycle never finalized (the agent
	// process was killed mid-commit, which we model as Outcome="unfinished").
	FinalizedAt time.Time
	// Outcome is one of "commit", "abort", or "unfinished".
	Outcome string
}

// CommitDuration is the gap from `Committing config session...` to
// `Configuration session finalized...`. Returns 0 for unfinished cycles.
func (c AgentCycle) CommitDuration() time.Duration {
	if c.CommitStartedAt.IsZero() || c.FinalizedAt.IsZero() {
		return 0
	}
	return c.FinalizedAt.Sub(c.CommitStartedAt)
}

// AgentCLIError is one `CLI command N of M '<cmd>' failed: <reason>` line.
// These are commit-time EOS validation failures; reading them as a group
// surfaces hardware-quirk patterns (e.g. all errors being
// `default interface TunnelN invalid command` from chi-dn-dzd5's
// Tunnel-name range cap). The N/M indices and timestamp are intentionally
// not captured — the top-K bucketer in pkg/analyze only reads Command +
// Reason after normalization, so the extra fields would just be unused
// parser state.
type AgentCLIError struct {
	Command string // <cmd>
	Reason  string // <reason>
}

var (
	// EOS agent log timestamps look like "2026/06/04 00:32:01.930732".
	// We accept either microsecond or nanosecond resolution, but the
	// 6-digit microsecond form is what we've seen in practice.
	agentTimeRE = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\.\d+)\s`)
	// Lines we care about (eapi.go line numbers are stable enough across
	// agent builds that we anchor on them).
	receivedLinesRE = regexp.MustCompile(`Received (\d+) lines of configuration from controller`)
	receivedBytesRE = regexp.MustCompile(`Received (\d+) bytes of configuration from controller`)
	committingRE    = regexp.MustCompile(`Committing config session due to diffs detected:`)
	finalizedRE     = regexp.MustCompile(`Configuration session finalized with command '[^']*\s+(commit|abort)'`)
	// The CLI-command failure shape, when seen inside the orchestrator's
	// log, is doubly quoted: `'CLI command N of M '<cmd>' failed: <reason>' at line K`.
	// The outer quotes belong to the orchestrator's error wrapper; the
	// inner ones belong to the agent's reported failure. We stop <cmd> and
	// <reason> at the next single quote to avoid capturing the outer
	// closing quote into the reason field.
	cliCommandFailRE = regexp.MustCompile(`CLI command (\d+) of (\d+) '([^']+)' failed:\s+([^']+)`)
)

const agentTimeLayout = "2006/01/02 15:04:05.000000"

func parseAgentTime(s string) (time.Time, bool) {
	// Accept variable-length fractional seconds by trimming or right-padding
	// to the layout's 6-digit microsecond precision. The agentTimeRE already
	// requires a `.<digits>` segment, so we always land on the dot branch.
	m := agentTimeRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	stamp := m[1]
	dot := strings.IndexByte(stamp, '.')
	frac := stamp[dot+1:]
	if len(frac) > 6 {
		stamp = stamp[:dot+1+6]
	} else if len(frac) < 6 {
		stamp = stamp + strings.Repeat("0", 6-len(frac))
	}
	t, err := time.Parse(agentTimeLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// loadAgentLog streams the file once, threading a small state machine that
// builds AgentCycle records each time it sees `Committing config session`
// followed by `Configuration session finalized`. It also collects every
// `CLI command N of M ...` failure line.
//
// The pairing logic for received-lines/bytes is "most recent precedes the
// Committing marker" — the agent emits both Received lines back-to-back per
// poll cycle, so they always land just before the commit (or, on a no-op
// poll, with no commit following them).
func loadAgentLog(path string) ([]AgentCycle, []AgentCLIError, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var cycles []AgentCycle
	var errs []AgentCLIError

	// Pending: the most recent Received-lines/Received-bytes pair that has
	// not yet been matched to a Committing marker.
	var pendingReceivedAt time.Time
	var pendingLines, pendingBytes int

	// Active: a Committing marker has been seen but not yet finalized.
	var active *AgentCycle

	s := bufio.NewScanner(f)
	// Some lines are huge (the agent dumps the entire received config
	// inline). Allow up to 4 MiB per line.
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		line := s.Text()
		ts, _ := parseAgentTime(line)

		switch {
		case receivedLinesRE.MatchString(line):
			pendingReceivedAt = ts
			if m := receivedLinesRE.FindStringSubmatch(line); m != nil {
				pendingLines, _ = strconv.Atoi(m[1])
			}
		case receivedBytesRE.MatchString(line):
			if m := receivedBytesRE.FindStringSubmatch(line); m != nil {
				pendingBytes, _ = strconv.Atoi(m[1])
			}
			// Bytes always immediately follows lines on the same poll, so
			// leave pendingReceivedAt at the lines-line time (microsecond
			// difference; the lines marker is the canonical "received").
		case committingRE.MatchString(line):
			// If a prior cycle was active but never finalized (e.g. the
			// agent process exited between `Committing` and the matching
			// `Configuration session finalized`, then restarted and began
			// a fresh commit), record it as `unfinished` before replacing.
			// Otherwise we'd silently drop it and skew commit counts +
			// downstream stats.
			if active != nil {
				active.Outcome = "unfinished"
				cycles = append(cycles, *active)
			}
			// Open a new cycle; absorb the pending received-pair into it
			// (the most recent one is the closest match by construction).
			active = &AgentCycle{
				ReceivedAt:      pendingReceivedAt,
				ReceivedLines:   pendingLines,
				ReceivedBytes:   pendingBytes,
				CommitStartedAt: ts,
			}
			pendingReceivedAt = time.Time{}
			pendingLines, pendingBytes = 0, 0
		case finalizedRE.MatchString(line):
			if active != nil {
				active.FinalizedAt = ts
				m := finalizedRE.FindStringSubmatch(line)
				if m != nil {
					active.Outcome = m[1] // "commit" or "abort"
				}
				cycles = append(cycles, *active)
				active = nil
			}
		}

		// CLI command failures land scattered through the log; capture
		// independently of the cycle state machine.
		if m := cliCommandFailRE.FindStringSubmatch(line); m != nil {
			errs = append(errs, AgentCLIError{
				Command: m[3],
				Reason:  m[4],
			})
		}
	}
	if err := s.Err(); err != nil {
		return cycles, errs, fmt.Errorf("scan: %w", err)
	}
	// If a Committing was active but never finalized, record it as an
	// unfinished cycle so callers can spot mid-commit kills.
	if active != nil {
		active.Outcome = "unfinished"
		cycles = append(cycles, *active)
	}
	return cycles, errs, nil
}
