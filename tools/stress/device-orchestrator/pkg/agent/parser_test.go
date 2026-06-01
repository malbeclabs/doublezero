package agent_test

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedClock returns a constant time for deterministic event timestamps.
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func TestParser_SingleTunnelDiffThenCommit(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	p := agent.NewParser(agent.WithClock(fixedClock(now)))

	events := p.Parse(`2026/05/27 12:00:01 Committing config session due to diffs detected: + interface Tunnel500   ip address 169.254.0.1/30`)
	require.Len(t, events, 1)
	assert.Equal(t, agent.EventPreCommitLog, events[0].Kind)
	assert.Equal(t, uint16(500), events[0].TunnelID)
	assert.Equal(t, now, events[0].At)
	assert.Equal(t, []uint16{500}, p.Pending())

	events = p.Parse(`2026/05/27 12:00:02 Configuration session finalized with command 'configure session doublezero-agent-abc123 commit'`)
	require.Len(t, events, 1)
	assert.Equal(t, agent.EventApplied, events[0].Kind)
	assert.Equal(t, uint16(500), events[0].TunnelID)
	assert.Empty(t, p.Pending(), "pending should clear after commit-success")
}

func TestParser_MultiTunnelDiffEmitsOneEventPerTunnel(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	// streamLines feeds one line per Parse call. The agent logs the diff with a
	// single log.Printf, so only the marker line carries the timestamp prefix
	// and every "+ interface Tunnel<ID>" lands on its own subsequent line.
	lines := []string{
		`2026/05/27 12:00:01 Committing config session due to diffs detected: `,
		`+ interface Tunnel500`,
		`    ip address 169.254.0.1/30`,
		`+ interface Tunnel501`,
		`- interface Tunnel499`,
		`+ interface Tunnel502`,
	}
	var events []agent.Event
	for _, l := range lines {
		events = append(events, p.Parse(l)...)
	}
	require.Len(t, events, 3, "only + lines, not - lines, produce events")
	assert.Equal(t, []uint16{500, 501, 502}, []uint16{events[0].TunnelID, events[1].TunnelID, events[2].TunnelID})
	for _, e := range events {
		assert.Equal(t, agent.EventPreCommitLog, e.Kind)
	}

	applied := p.Parse(`Configuration session finalized with command 'configure session foo commit'`)
	require.Len(t, applied, 3, "Applied fires once per pending tunnel")
	assert.Equal(t, []uint16{500, 501, 502}, []uint16{applied[0].TunnelID, applied[1].TunnelID, applied[2].TunnelID})
}

func TestParser_AddedInterfaceOutsideDiffBlockIgnored(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	// A "+ interface Tunnel" line with no preceding "Committing..." marker is
	// not part of a commit diff and must not produce events.
	assert.Empty(t, p.Parse(`+ interface Tunnel500`))
	assert.Empty(t, p.Pending())

	// Once a commit closes the block, later diff-shaped lines are ignored again
	// until the next marker opens a new block.
	require.Len(t, p.Parse(`Committing config session due to diffs detected: + interface Tunnel600`), 1)
	require.Len(t, p.Parse(`Configuration session finalized with command 'configure session foo commit'`), 1)
	assert.Empty(t, p.Parse(`+ interface Tunnel601`), "diff-shaped line after block close is ignored")
	assert.Empty(t, p.Pending())
}

func TestParser_DeprovisionOnlyDiffEmitsNothing(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	events := p.Parse(`Committing config session due to diffs detected: - interface Tunnel500 - interface Tunnel501`)
	assert.Empty(t, events)
	assert.Empty(t, p.Pending())
}

func TestParser_AbortClearsBufferWithoutAppliedEvents(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	events := p.Parse(`Committing config session due to diffs detected: + interface Tunnel500`)
	require.Len(t, events, 1)
	require.Equal(t, []uint16{500}, p.Pending())

	events = p.Parse(`Configuration session finalized with command 'configure session foo abort'`)
	assert.Empty(t, events, "abort emits no events")
	assert.Empty(t, p.Pending(), "abort still clears pending")
}

func TestParser_CommitWithoutPendingDiffIsNoOp(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	events := p.Parse(`Configuration session finalized with command 'configure session foo commit'`)
	assert.Empty(t, events)
}

func TestParser_TwoConsecutiveProvisionCycles(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()

	// Cycle 1
	require.Len(t, p.Parse(`Committing config session due to diffs detected: + interface Tunnel500`), 1)
	require.Len(t, p.Parse(`Configuration session finalized with command 'configure session foo commit'`), 1)
	assert.Empty(t, p.Pending())

	// Cycle 2
	require.Len(t, p.Parse(`Committing config session due to diffs detected: + interface Tunnel501`), 1)
	applied := p.Parse(`Configuration session finalized with command 'configure session bar commit'`)
	require.Len(t, applied, 1)
	assert.Equal(t, uint16(501), applied[0].TunnelID, "cycle 2 must not replay tunnel 500")
}

func TestParser_UnrelatedLinesIgnored(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	for _, line := range []string{
		``,
		`Received 42 lines of configuration from controller`,
		`forced unlock of configuration lock (xyz)`,
		`some random log noise`,
	} {
		assert.Empty(t, p.Parse(line), "line=%q", line)
	}
}

func TestParser_RejectsOversizedTunnelID(t *testing.T) {
	t.Parallel()

	// uint16 max is 65535; 70000 should be silently skipped, not panic.
	p := agent.NewParser()
	events := p.Parse(`Committing config session due to diffs detected: + interface Tunnel70000 + interface Tunnel500`)
	require.Len(t, events, 1)
	assert.Equal(t, uint16(500), events[0].TunnelID)
}

func TestParser_DoesNotConfuseInterfaceNamePrefixes(t *testing.T) {
	t.Parallel()

	// "Tunnel5000" must not match a regex that's been fooled by "Tunnel500"
	// being a prefix. Use a `\b` boundary in the regex.
	p := agent.NewParser()
	events := p.Parse(`Committing config session due to diffs detected: + interface Tunnel5000`)
	require.Len(t, events, 1)
	assert.Equal(t, uint16(5000), events[0].TunnelID)
}
