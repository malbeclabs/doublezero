// Package runlog appends per-event rows to the orchestrator runlog file
// (`orchestrator-runlog.json`). One row per line; line-delimited JSON so the
// file can be tailed and downstream tooling can parse incrementally.
//
// Row schema (per #3746):
//
//	{run_id, user_index, user_pubkey, tunnel_id, event, t_ns, n_after_event}
//
// `t_ns` is the unix epoch in nanoseconds. `n_after_event` is the size of the
// active user set immediately after the event applied — provisioning increments
// it on `activate`, deprovisioning decrements on `deprovision_activate`. Other
// events carry the count as-of-emission.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Event enumerates the recognized event names. Stringly-typed in the file so
// the schema can grow without consumers needing to track an enum.
type Event string

const (
	EventSubmit              Event = "submit"
	EventConfirm             Event = "confirm"
	EventActivate            Event = "activate"
	EventPreCommitLog        Event = "pre_commit_log" // emitted by part-3 agent runner
	EventApplied             Event = "applied"        // emitted by part-3 agent runner
	EventDeprovisionSubmit   Event = "deprovision_submit"
	EventDeprovisionConfirm  Event = "deprovision_confirm"
	EventDeprovisionActivate Event = "deprovision_activate"
)

// Row is one entry in the runlog file. Field names match #3746's schema.
type Row struct {
	RunID       string `json:"run_id"`
	UserIndex   int    `json:"user_index"`
	UserPubkey  string `json:"user_pubkey"`
	TunnelID    uint16 `json:"tunnel_id"`
	Event       Event  `json:"event"`
	TNs         int64  `json:"t_ns"`
	NAfterEvent int    `json:"n_after_event"`
}

// Writer appends rows to an open file in line-delimited JSON.
type Writer struct {
	mu   sync.Mutex
	w    io.WriteCloser
	path string
}

// Open creates or truncates the file at path for append-only writes.
func Open(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open runlog %s: %w", path, err)
	}
	return &Writer{w: f, path: path}, nil
}

// Path returns the file path the writer is appending to.
func (w *Writer) Path() string { return w.path }

// Append serializes row as JSON and writes a single line.
func (w *Writer) Append(row Row) error {
	if row.TNs == 0 {
		row.TNs = time.Now().UnixNano()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return errors.New("runlog writer closed")
	}
	buf, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("marshal runlog row: %w", err)
	}
	buf = append(buf, '\n')
	if _, err := w.w.Write(buf); err != nil {
		return fmt.Errorf("write runlog row: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return nil
	}
	err := w.w.Close()
	w.w = nil
	return err
}
