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
	"regexp"
	"strconv"
	"strings"
	"sync"
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

	mu        sync.RWMutex
	latestCPU float64
	cpuValid  bool
}

func NewSampler(client eapiRunner, workingDir string, interval time.Duration, logger *slog.Logger) *Sampler {
	return &Sampler{client: client, workingDir: workingDir, interval: interval, logger: logger, now: time.Now}
}

// LatestCPUPercent returns the most recently parsed total CPU usage (sum of
// non-idle fields from `show processes top once`). Returns (0, false) before
// the first successful parse.
func (s *Sampler) LatestCPUPercent() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestCPU, s.cpuValid
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
		if c.cmd == "show processes top once" {
			if pct, ok := parseCPUPercent(raw); ok {
				s.mu.Lock()
				s.latestCPU, s.cpuValid = pct, true
				s.mu.Unlock()
			} else {
				s.logger.Warn("could not parse CPU from show processes top once")
			}
		}
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

// topCPULineRE matches the procps/busybox `top` `%Cpu(s):` (or `Cpu(s):`)
// header line. The numeric fields are extracted and summed below.
var topCPULineRE = regexp.MustCompile(`(?m)^%?Cpu\(s\):\s*(.+)$`)

// cpuFieldRE captures one `<number> <label>` pair (e.g. `0.5 us`, `5,0 id`).
var cpuFieldRE = regexp.MustCompile(`([0-9]+(?:[.,][0-9]+)?)\s*([a-zA-Z]+)`)

// parseCPUPercent extracts the total non-idle CPU percentage from an Arista
// `show processes top once | json` envelope. The envelope wraps procps
// `top -bn1` output as {"output":"top -bn1\n%Cpu(s): ...\n..."}; the
// helper extracts the `%Cpu(s)` line, sums every numeric field except `id`
// (idle), and returns (pct, true) on a parsed line. Locale-decimal commas
// are tolerated.
func parseCPUPercent(raw json.RawMessage) (float64, bool) {
	var env struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Output == "" {
		return 0, false
	}
	m := topCPULineRE.FindStringSubmatch(env.Output)
	if len(m) < 2 {
		return 0, false
	}
	var total float64
	var found bool
	for _, pair := range cpuFieldRE.FindAllStringSubmatch(m[1], -1) {
		label := strings.ToLower(pair[2])
		if label == "id" {
			found = true
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(pair[1], ",", "."), 64)
		if err != nil {
			continue
		}
		total += v
		found = true
	}
	if !found {
		return 0, false
	}
	return total, true
}

// fileTimestamp renders t as ISO 8601 UTC with `:` replaced by `-` so the
// result is portable across filesystems that disallow `:`.
func fileTimestamp(t time.Time) string {
	return strings.ReplaceAll(t.UTC().Format("2006-01-02T15:04:05.000000000Z"), ":", "-")
}
