package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// AbortSentinel is the JSON the observer writes to the run dir's `abort`
// file when one of its triggers fires.
type AbortSentinel struct {
	Reason    string `json:"reason"`
	Detail    string `json:"detail"`
	FiredAtNs int64  `json:"fired_at_ns"`
	Trigger   string `json:"trigger"`
}

// FiredAt returns the abort wall-clock time.
func (a *AbortSentinel) FiredAt() time.Time { return time.Unix(0, a.FiredAtNs) }

func loadAbortSentinel(path string) (*AbortSentinel, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a AbortSentinel
	if err := json.Unmarshal(buf, &a); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &a, nil
}
