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

// userTunnelDescriptionPrefix identifies user-tunnel interfaces by the
// description string the controller renders on each user's `interface
// TunnelN` block. Using the description rather than a numeric range on
// the tunnel ID lets the observer compare apples-to-apples against the
// orchestrator's user count regardless of where in the Tunnel<N>
// namespace the controller chooses to place them (legacy controllers
// started at 500; the gm/tunnel-id-start-1 fix starts at 1, which
// overlaps the inter-router routing-fabric range on physical EOS).
const userTunnelDescriptionPrefix = "USER-UCAST-"

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
	{"show interfaces description", "show-interfaces-description", true},
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
// tunnel interfaces on the device — i.e. interfaces whose description
// starts with "USER-UCAST-" (the prefix the controller renders on
// every per-user `interface TunnelN` block), counted from
// `show interfaces description`. Returns (0, false) before the first
// successful parse. The abort decider uses this to detect when the
// orchestrator's expected active-user count diverges from what the
// agent has actually applied — e.g. when the controller's per-device
// tunnel-slot cap silently truncates the rendered device config.
//
// Earlier iterations used `show gre tunnel static` (only lists
// statically-configured GRE-keyed routing-fabric tunnels) and
// `show ip interface brief` filtered on a Tunnel-index >= 500
// (worked while user tunnel IDs started at 500 but broke when the
// controller started allocating from 1, overlapping the routing
// fabric's low-numbered range). Filtering on the controller's
// rendered description string is the most stable discriminator:
// it tracks the controller's namespacing decisions regardless of
// where in the Tunnel<N> ID space user tunnels happen to land.
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
		if c.cmd == "show interfaces description" {
			if n, ok := parseTunnelCount(raw); ok {
				s.mu.Lock()
				s.latestTunnelCount, s.tunnelCountValid = n, true
				s.mu.Unlock()
			} else {
				s.logger.Warn("could not parse tunnel count from show interfaces description")
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

// parseTunnelCount returns the number of user-tunnel interfaces in the
// `show interfaces description | json` eAPI response, shaped as
// `{"interfaceDescriptions": {"<name>": {"description": "..."}, …}}`.
// An interface is a user tunnel when its description begins with
// `userTunnelDescriptionPrefix` — every per-user `interface TunnelN`
// block the controller renders carries `description USER-UCAST-<idx>`,
// so this filter is robust against changes in the Tunnel<N> id
// range (legacy: 500+; current: 1+).
//
// Returns (0, true) on an empty interfaces map and (N, true) on a
// populated one. Returns (0, false) only when the
// `interfaceDescriptions` key is missing or the JSON is malformed, so
// the abort decider can tell "zero confirmed" apart from "unknown" and
// suppress the sentinel in the latter case.
func parseTunnelCount(raw json.RawMessage) (int, bool) {
	type desc struct {
		Description string `json:"description"`
	}
	var env struct {
		InterfaceDescriptions map[string]desc `json:"interfaceDescriptions"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, false
	}
	if env.InterfaceDescriptions == nil {
		return 0, false
	}
	count := 0
	for _, d := range env.InterfaceDescriptions {
		if strings.HasPrefix(d.Description, userTunnelDescriptionPrefix) {
			count++
		}
	}
	return count, true
}

// fileTimestamp renders t as ISO 8601 UTC with `:` → `-` for filesystem
// portability.
func fileTimestamp(t time.Time) string {
	return strings.ReplaceAll(t.UTC().Format("2006-01-02T15:04:05.000000000Z"), ":", "-")
}
