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
	"strconv"
	"strings"
	"sync"
	"time"
)

// userTunnelMinID is the lowest interface index treated as a "user
// tunnel" when counting interfaces from `show ip interface brief`. The
// stress harness's user tunnels start at Tunnel500 (the controller's
// rendered config begins its per-user GRE block at this index); lower
// Tunnel indices on the device — typically Tunnel1..Tunnel127 used for
// inter-router routing fabric on physical EOS — are explicitly excluded
// so the device_tunnel_gap sentinel compares apples to apples against
// the orchestrator's active-user count.
const userTunnelMinID = 500

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
	{"show ip interface brief", "show-ip-interface-brief", true},
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
// tunnel interfaces on the device — i.e. interfaces named TunnelN
// with N >= userTunnelMinID, parsed from `show ip interface brief`.
// Returns (0, false) before the first successful parse. The abort
// decider uses this to detect when the orchestrator's expected
// active-user count diverges from what the agent has actually applied —
// e.g. when the controller's per-device tunnel-slot cap silently
// truncates the rendered device config.
//
// An earlier version of this used `show gre tunnel static`, but that
// command only lists statically-configured GRE-keyed tunnels (the
// device's inter-router fabric, ~3 entries on physical EOS) — user
// tunnels are interface-mode GRE without a static next-hop and don't
// show up there. The result was a sentinel that always read zero on
// any device that did its tunnel via interface-mode GRE.
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
		if c.cmd == "show ip interface brief" {
			if n, ok := parseTunnelCount(raw); ok {
				s.mu.Lock()
				s.latestTunnelCount, s.tunnelCountValid = n, true
				s.mu.Unlock()
			} else {
				s.logger.Warn("could not parse tunnel count from show ip interface brief")
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
// `show ip interface brief | json` eAPI response, shaped as
// `{"interfaces": {"<name>": {…}, …}}`. An interface is a user tunnel
// when its name matches `Tunnel<N>` and N >= userTunnelMinID; lower
// indices and other interface types (Loopback, Ethernet, Management)
// are ignored.
//
// Returns (0, true) on an empty interfaces map and (N, true) on a
// populated one — including when N=0 because the only Tunnel interfaces
// present have low indices (the inter-router fabric). Returns (0, false)
// only when the `interfaces` key is missing entirely or the JSON is
// malformed, so the abort decider can tell "zero confirmed" apart from
// "unknown" and suppress the sentinel in the latter case.
func parseTunnelCount(raw json.RawMessage) (int, bool) {
	var env struct {
		Interfaces map[string]json.RawMessage `json:"interfaces"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, false
	}
	if env.Interfaces == nil {
		return 0, false
	}
	count := 0
	for name := range env.Interfaces {
		idx, ok := userTunnelIndex(name)
		if !ok || idx < userTunnelMinID {
			continue
		}
		count++
	}
	return count, true
}

// userTunnelIndex parses "Tunnel<N>" and returns N. Returns (0, false)
// for any other interface name.
func userTunnelIndex(name string) (int, bool) {
	const prefix = "Tunnel"
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	idx, err := strconv.Atoi(name[len(prefix):])
	if err != nil {
		return 0, false
	}
	return idx, true
}

// fileTimestamp renders t as ISO 8601 UTC with `:` → `-` for filesystem
// portability.
func fileTimestamp(t time.Time) string {
	return strings.ReplaceAll(t.UTC().Format("2006-01-02T15:04:05.000000000Z"), ":", "-")
}
