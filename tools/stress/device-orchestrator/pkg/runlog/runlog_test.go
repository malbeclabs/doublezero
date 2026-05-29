package runlog_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/malbeclabs/doublezero/tools/stress/device-orchestrator/pkg/runlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriter_RoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.jsonl")
	w, err := runlog.Open(path)
	require.NoError(t, err)

	rows := []runlog.Row{
		{RunID: "run-1", UserIndex: 0, UserPubkey: "pk0", TunnelID: 500, Event: runlog.EventSubmit, TNs: 1000, NAfterEvent: 0},
		{RunID: "run-1", UserIndex: 0, UserPubkey: "pk0", TunnelID: 500, Event: runlog.EventConfirm, TNs: 2000, NAfterEvent: 0},
		{RunID: "run-1", UserIndex: 0, UserPubkey: "pk0", TunnelID: 500, Event: runlog.EventActivate, TNs: 3000, NAfterEvent: 1},
		{RunID: "run-1", UserIndex: 0, UserPubkey: "pk0", TunnelID: 500, Event: runlog.EventDeprovisionActivate, TNs: 4000, NAfterEvent: 0},
	}
	for _, r := range rows {
		require.NoError(t, w.Append(r))
	}
	require.NoError(t, w.Close())

	// File ends with a newline; one row per line.
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var read []runlog.Row
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r runlog.Row
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &r))
		read = append(read, r)
	}
	require.NoError(t, scanner.Err())

	assert.Equal(t, rows, read)
}

func TestWriter_FillsMissingTimestamp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.jsonl")
	w, err := runlog.Open(path)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Append(runlog.Row{RunID: "r", UserIndex: 0, UserPubkey: "pk", Event: runlog.EventSubmit}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var r runlog.Row
	require.NoError(t, json.Unmarshal(data[:len(data)-1], &r))
	assert.NotZero(t, r.TNs, "Append should fill t_ns when zero")
}

func TestWriter_RejectsAfterClose(t *testing.T) {
	t.Parallel()

	w, err := runlog.Open(filepath.Join(t.TempDir(), "orchestrator-runlog.jsonl"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	err = w.Append(runlog.Row{RunID: "r", Event: runlog.EventSubmit})
	require.Error(t, err)
}

func TestWriter_Truncates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "orchestrator-runlog.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("stale\n"), 0o644))

	w, err := runlog.Open(path)
	require.NoError(t, err)
	require.NoError(t, w.Append(runlog.Row{RunID: "r", Event: runlog.EventSubmit, TNs: 1}))
	require.NoError(t, w.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "stale", "Open(path) should truncate existing content")
}
