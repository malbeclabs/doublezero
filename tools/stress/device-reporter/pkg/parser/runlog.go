package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Event mirrors one row in orchestrator-runlog.jsonl. The orchestrator emits
// at most 8 distinct kinds (submit/confirm/activate × {provision, deprovision}
// plus pre_commit_log / applied for tunnels the agent finished applying).
type Event struct {
	UserIndex   int    `json:"user_index"`
	TunnelID    uint16 `json:"tunnel_id"`
	Event       string `json:"event"`
	TNs         int64  `json:"t_ns"`
	NAfterEvent int    `json:"n_after_event"`
}

// Time returns the event's wall-clock instant.
func (e Event) Time() time.Time { return time.Unix(0, e.TNs) }

// EventCounts groups events by .Event for headline figures.
type EventCounts map[string]int

// CountEvents returns a per-event-type tally.
func CountEvents(events []Event) EventCounts {
	out := make(EventCounts)
	for _, e := range events {
		out[e.Event]++
	}
	return out
}

func loadRunlog(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	s := bufio.NewScanner(f)
	// Some runs at high user counts produce >64 KB lines if pubkeys grow
	// (they don't today, but safer to allow). Cap at 1 MiB.
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for s.Scan() {
		line++
		if len(s.Bytes()) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, e)
	}
	if err := s.Err(); err != nil {
		return out, fmt.Errorf("scan: %w", err)
	}
	return out, nil
}
