// Package sample issues a fixed list of show commands on every tick and
// writes one file per command. Per-command failures are logged but do not
// stop the loop; the abort decider owns repeated-failure policy.
package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type eapiRunner interface {
	RunShowJSON(cmd string) (json.RawMessage, error)
	RunShowText(cmd string) (string, error)
}

type commandSpec struct {
	cmd, slug string
	json      bool
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

	mu                sync.RWMutex
	latestCPU         float64
	cpuValid          bool
	latestTunnelCount int
	tunnelCountValid  bool
}

func NewSampler(client eapiRunner, workingDir string, interval time.Duration, logger *slog.Logger) *Sampler {
	return &Sampler{client: client, workingDir: workingDir, interval: interval, logger: logger, now: time.Now}
}

// LatestCPUPercent returns the most recently parsed total CPU usage (sum
// of non-idle fields). Returns (0, false) before the first successful
// parse.
func (s *Sampler) LatestCPUPercent() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestCPU, s.cpuValid
}

// LatestTunnelCount returns the most recently observed number of user
// tunnels on the device (`show gre tunnel static`'s greTunnels map
// length). Returns (0, false) before the first successful parse. The
// abort decider uses this to detect when the orchestrator's expected
// active-user count diverges from what the agent has actually applied —
// e.g. when the controller's per-device tunnel-slot cap silently
// truncates the rendered device config.
func (s *Sampler) LatestTunnelCount() (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestTunnelCount, s.tunnelCountValid
}

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
		// goeapi.RunCommands does not accept a context, so run the
		// command in a goroutine and race ctx.Done() against the
		// result. The leaked goroutine finishes whenever the HTTP
		// call returns and does not block exit.
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
		if c.cmd == "show gre tunnel static" {
			if n, ok := parseTunnelCount(raw); ok {
				s.mu.Lock()
				s.latestTunnelCount, s.tunnelCountValid = n, true
				s.mu.Unlock()
			} else {
				s.logger.Warn("could not parse tunnel count from show gre tunnel static")
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

// parseCPUPercent extracts the total non-idle CPU percentage from the
// `show processes top once | json` eAPI response, which has shape
// `{"cpuInfo":{"%Cpu(s)":{"idle":X,"user":Y,"system":Z,…}}, …}`. Every
// numeric sibling of `idle` is summed. The `%Cpu(s)` key embeds a
// percent sign so the inner object can only be addressed via map
// decoding.
func parseCPUPercent(raw json.RawMessage) (float64, bool) {
	var env struct {
		CPUInfo map[string]map[string]float64 `json:"cpuInfo"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || len(env.CPUInfo) == 0 {
		return 0, false
	}
	fields, ok := env.CPUInfo["%Cpu(s)"]
	if !ok || len(fields) == 0 {
		return 0, false
	}
	var total float64
	var sawIdle bool
	for k, v := range fields {
		if strings.EqualFold(k, "idle") {
			sawIdle = true
			continue
		}
		total += v
	}
	if !sawIdle {
		return 0, false
	}
	return total, true
}

// parseTunnelCount returns the number of entries in the `greTunnels` map
// of the `show gre tunnel static | json` eAPI response, shaped as
// `{"greTunnels": {"<idx>": {…}, …}}`. Returns (0, true) on an empty map
// — a healthy device with no user tunnels — and (0, false) only when
// `greTunnels` is missing entirely or the response is malformed, so the
// abort decider can distinguish "zero confirmed" from "unknown".
func parseTunnelCount(raw json.RawMessage) (int, bool) {
	var env struct {
		GreTunnels map[string]json.RawMessage `json:"greTunnels"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, false
	}
	if env.GreTunnels == nil {
		return 0, false
	}
	return len(env.GreTunnels), true
}

// fileTimestamp renders t as ISO 8601 UTC with `:` → `-` for filesystem
// portability.
func fileTimestamp(t time.Time) string {
	return strings.ReplaceAll(t.UTC().Format("2006-01-02T15:04:05.000000000Z"), ":", "-")
}
