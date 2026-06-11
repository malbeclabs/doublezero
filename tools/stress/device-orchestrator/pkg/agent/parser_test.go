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
	// Two events: EventCommit (activity signal, always fires) and EventApplied
	// (per pending tunnel).
	require.Len(t, events, 2)
	assert.Equal(t, agent.EventCommit, events[0].Kind, "EventCommit fires first as the activity signal")
	assert.Equal(t, uint16(0), events[0].TunnelID, "EventCommit carries no tunnel id")
	assert.Equal(t, agent.EventApplied, events[1].Kind)
	assert.Equal(t, uint16(500), events[1].TunnelID)
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
	// One EventCommit + one EventApplied per pending tunnel.
	require.Len(t, applied, 4)
	assert.Equal(t, agent.EventCommit, applied[0].Kind)
	assert.Equal(t, []uint16{500, 501, 502}, []uint16{applied[1].TunnelID, applied[2].TunnelID, applied[3].TunnelID})
	for _, e := range applied[1:] {
		assert.Equal(t, agent.EventApplied, e.Kind)
	}
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
	// EventCommit + EventApplied for tunnel 600.
	require.Len(t, p.Parse(`Configuration session finalized with command 'configure session foo commit'`), 2)
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

// TestParser_DeprovisionCommitEmitsCommitOnly proves that a successful
// commit whose diff was pure-removal (no `+ interface Tunnel<ID>` lines)
// still emits EventCommit. The post-deprovision quiescence wait in
// sweep.Run depends on this — without an EventCommit signal during
// deprovision the wait would see the agent as silent through the entire
// teardown and skip the wait, leaving deprovisioned tunnels stranded on
// the device.
func TestParser_DeprovisionCommitEmitsCommitOnly(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	assert.Empty(t, p.Parse(`Committing config session due to diffs detected: - interface Tunnel500 - interface Tunnel501`))

	events := p.Parse(`Configuration session finalized with command 'configure session foo commit'`)
	require.Len(t, events, 1, "deprovision commit must still emit one EventCommit even with no pending Applied tunnels")
	assert.Equal(t, agent.EventCommit, events[0].Kind)
	assert.Equal(t, uint16(0), events[0].TunnelID)
}

func TestParser_AbortClearsBufferWithoutAppliedEvents(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	events := p.Parse(`Committing config session due to diffs detected: + interface Tunnel500`)
	require.Len(t, events, 1)
	require.Equal(t, []uint16{500}, p.Pending())

	// Abort emits exactly one EventCommitAborted (so the quiescence
	// tracker can clear its pending-commit flag) and drops the
	// per-tunnel Applieds.
	events = p.Parse(`Configuration session finalized with command 'configure session foo abort'`)
	require.Len(t, events, 1)
	assert.Equal(t, agent.EventCommitAborted, events[0].Kind)
	assert.Equal(t, uint16(0), events[0].TunnelID)
	assert.Empty(t, p.Pending(), "abort still clears pending")
}

func TestParser_CommitWithoutPendingDiffEmitsCommitOnly(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	events := p.Parse(`Configuration session finalized with command 'configure session foo commit'`)
	// Every successful commit emits EventCommit even if no diff block was
	// open (e.g. a configure-session that committed an empty changeset).
	require.Len(t, events, 1)
	assert.Equal(t, agent.EventCommit, events[0].Kind)
	assert.Equal(t, uint16(0), events[0].TunnelID)
}

func TestParser_TwoConsecutiveProvisionCycles(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()

	// Cycle 1: pre_commit (tunnel 500) → EventCommit + Applied(500).
	require.Len(t, p.Parse(`Committing config session due to diffs detected: + interface Tunnel500`), 1)
	require.Len(t, p.Parse(`Configuration session finalized with command 'configure session foo commit'`), 2)
	assert.Empty(t, p.Pending())

	// Cycle 2 must not replay tunnel 500.
	require.Len(t, p.Parse(`Committing config session due to diffs detected: + interface Tunnel501`), 1)
	applied := p.Parse(`Configuration session finalized with command 'configure session bar commit'`)
	require.Len(t, applied, 2)
	assert.Equal(t, agent.EventCommit, applied[0].Kind)
	assert.Equal(t, agent.EventApplied, applied[1].Kind)
	assert.Equal(t, uint16(501), applied[1].TunnelID, "cycle 2 must not replay tunnel 500")
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

// TestParser_ReceivedBytesEmitsConfigReceived covers the activity-signal
// shim the orchestrator's quiescence tracker uses to keep itself awake
// during the multi-second diff-check window that follows a fresh config
// pull. Without this, the agent can be silent for tens of seconds while
// analyzing a >1 MB deprovision diff and the tracker will mistake the
// silence for "agent quiesced" and tear down the SSH session.
func TestParser_ReceivedBytesEmitsConfigReceived(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	p := agent.NewParser(agent.WithClock(fixedClock(now)))

	// The "lines" sibling should not produce an event (we anchor on
	// "bytes" so exactly one EventConfigReceived fires per poll cycle).
	assert.Empty(t, p.Parse(`2026/06/04 22:00:00.000000 eapi.go:217: Received 51553 lines of configuration from controller`))

	events := p.Parse(`2026/06/04 22:00:00.000010 eapi.go:218: Received 1622130 bytes of configuration from controller`)
	require.Len(t, events, 1)
	assert.Equal(t, agent.EventConfigReceived, events[0].Kind)
	assert.Equal(t, uint16(0), events[0].TunnelID)
	assert.Equal(t, now, events[0].At)
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

// TestParser_RealEOSDiffFormat covers the shape real Arista EOS hardware
// emits when diffing a session against running-config: the
// "interface TunnelN" section header is a context line (no prefix on the
// first section, space-prefixed on subsequent sections) and the additions
// land on the property lines below it. cEOS containers don't do this —
// they prefix the entire section, header included, with "+". The parser
// must promote a section into pending the first time it sees a `+   ...`
// property line inside it, and only once per section.
func TestParser_RealEOSDiffFormat(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	lines := []string{
		`2026/06/04 13:40:10 Committing config session due to diffs detected: --- system:/running-config`,
		`+++ session:/doublezero-agent-1780580398-session-config`,
		`interface Tunnel500`,
		`+   description USER-UCAST-500`,
		`+   mtu 9216`,
		`+   vrf vrf1`,
		`+   ip address 169.254.0.0/31`,
		` !`,
		` interface Tunnel501`,
		`+   description USER-UCAST-501`,
		`+   mtu 9216`,
		` !`,
		` interface Tunnel502`,
		`+   description USER-UCAST-502`,
		` !`,
	}
	var events []agent.Event
	for _, l := range lines {
		events = append(events, p.Parse(l)...)
	}
	require.Len(t, events, 3, "one EventPreCommitLog per real-EOS section, not per + line")
	for i, want := range []uint16{500, 501, 502} {
		assert.Equal(t, agent.EventPreCommitLog, events[i].Kind)
		assert.Equal(t, want, events[i].TunnelID)
	}
	assert.Equal(t, []uint16{500, 501, 502}, p.Pending())

	applied := p.Parse(`Configuration session finalized with command 'configure session doublezero-agent-1780580398 commit'`)
	// One EventCommit + one EventApplied per pending tunnel.
	require.Len(t, applied, 4)
	assert.Equal(t, agent.EventCommit, applied[0].Kind)
	assert.Equal(t, []uint16{500, 501, 502}, []uint16{applied[1].TunnelID, applied[2].TunnelID, applied[3].TunnelID})
}

// TestParser_RealEOSPureRemovalSectionEmitsNothing proves that a real-EOS
// section that contains only "-   ..." lines (deprovision) does not get
// promoted into pending — only `+` lines flip the section.
func TestParser_RealEOSPureRemovalSectionEmitsNothing(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	lines := []string{
		`Committing config session due to diffs detected: --- system:/running-config`,
		`+++ session:/foo-config`,
		` interface Tunnel500`,
		`-   description USER-UCAST-500`,
		`-   mtu 9216`,
		` !`,
	}
	for _, l := range lines {
		assert.Empty(t, p.Parse(l), "line=%q", l)
	}
	assert.Empty(t, p.Pending(), "pure-removal section must not pre-commit")

	events := p.Parse(`Configuration session finalized with command 'configure session foo commit'`)
	require.Len(t, events, 1, "commit fires EventCommit even with no pending")
	assert.Equal(t, agent.EventCommit, events[0].Kind)
}

// TestParser_RealEOSEmitsOncePerSectionEvenWithManyAdditions enforces that
// a section with many `+   ...` property lines produces exactly one
// EventPreCommitLog (and exactly one entry in pending).
func TestParser_RealEOSEmitsOncePerSectionEvenWithManyAdditions(t *testing.T) {
	t.Parallel()

	p := agent.NewParser()
	require.Empty(t, p.Parse(`Committing config session due to diffs detected: --- system:/running-config`))
	require.Empty(t, p.Parse(`+++ session:/foo-config`))
	require.Empty(t, p.Parse(`interface Tunnel900`))

	first := p.Parse(`+   description USER-UCAST-900`)
	require.Len(t, first, 1, "first + line promotes the section")
	assert.Equal(t, uint16(900), first[0].TunnelID)

	for _, prop := range []string{
		`+   mtu 9216`,
		`+   vrf vrf1`,
		`+   ip address 169.254.0.0/31`,
		`+   tunnel mode gre`,
	} {
		assert.Empty(t, p.Parse(prop), "subsequent + lines in the same section must not re-emit")
	}
	assert.Equal(t, []uint16{900}, p.Pending())
}
