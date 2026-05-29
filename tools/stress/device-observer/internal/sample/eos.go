// Package sample issues a fixed list of show commands on every tick and
// writes one file per command. Per-command failures are logged but do not
// stop the loop; the abort decider (PR #3796) owns repeated-failure policy.
package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// eapiRunner is the subset of *eapi.Client used by Sampler; tests
// substitute a fake.
type eapiRunner interface {
	RunShowJSON(cmd string) (json.RawMessage, error)
	RunShowText(cmd string) (string, error)
}

type commandSpec struct {
	cmd, slug string
	json      bool // false → text encoding
}

var commands = []commandSpec{
	{"show hardware capacity", "show-hardware-capacity", true},
	{"show gre tunnel static", "show-gre-tunnel-static", true},
	{"show processes top once", "show-processes-top-once", true},
	{"show logging errors", "show-logging-errors", false},
	{"show logging critical", "show-logging-critical", false},
}

type Sampler struct {
	client     eapiRunner
	workingDir string
	interval   time.Duration
	logger     *slog.Logger
	now        func() time.Time
}

func NewSampler(client eapiRunner, workingDir string, interval time.Duration, logger *slog.Logger) *Sampler {
	return &Sampler{client: client, workingDir: workingDir, interval: interval, logger: logger, now: time.Now}
}

// Run samples immediately, then on every tick of interval, until ctx is
// canceled. The immediate first sample avoids waiting a full interval for
// the first snapshot when interval is large.
func (s *Sampler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Sampler) tick(ctx context.Context) {
	ts := s.now().UTC()
	for _, c := range commands {
		if ctx.Err() != nil {
			return
		}
		// Run each command in a goroutine so ctx cancellation can
		// abandon the tick even though goeapi.RunCommands does not
		// accept a context. The leaked goroutine finishes whenever
		// the underlying HTTP call returns and does not block exit.
		done := make(chan error, 1)
		go func() { done <- s.runOne(c, ts) }()
		select {
		case <-ctx.Done():
			return
		case err := <-done:
			if err != nil {
				s.logger.Warn("sample command failed", "command", c.cmd, "err", err)
			}
		}
	}
}

func (s *Sampler) runOne(c commandSpec, ts time.Time) error {
	var body []byte
	ext := "log"
	if c.json {
		raw, err := s.client.RunShowJSON(c.cmd)
		if err != nil {
			return err
		}
		body, ext = raw, "json"
	} else {
		text, err := s.client.RunShowText(c.cmd)
		if err != nil {
			return err
		}
		body = []byte(text)
	}
	path := filepath.Join(s.workingDir, fmt.Sprintf("%s-%s.%s", c.slug, fileTimestamp(ts), ext))
	if err := os.WriteFile(path, body, 0o640); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// fileTimestamp renders t as ISO 8601 UTC with `:` replaced by `-` so the
// result is portable across filesystems that disallow `:`.
func fileTimestamp(t time.Time) string {
	return strings.ReplaceAll(t.Format("2006-01-02T15:04:05.000000000Z"), ":", "-")
}
