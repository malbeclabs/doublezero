// Package loggingtail contains the device-observer's EOS-syslog poller and
// orchestrator-agent log tailer. Both append one NDJSON row per line to a
// well-known file in the working directory.
package loggingtail

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
)

const (
	eosOutputFilename = "observer.eos_logging.json"
	eosMinLookback    = 30 * time.Second // floor so consecutive ticks always overlap
)

type eosLogRunner interface {
	RunShowText(cmd string) (string, error)
}

// eosLine is the parsed shape of one syslog row. Severity and facility are
// empty when the line does not match the default Arista format; the full
// line still lands in Message so unparseable lines are not silently lost.
type eosLine struct {
	TNS      int64  `json:"t_ns"`
	Time     string `json:"time"`
	Severity string `json:"severity"`
	Facility string `json:"facility"`
	Message  string `json:"message"`
	raw      string // full source line; used as dedupe key so distinct events at the same second don't collapse
}

// EOSPoller queries `show logging last <window>` on every tick, dedupes
// across the rolling window, and appends NDJSON rows to
// observer.eos_logging.json.
type EOSPoller struct {
	client   eosLogRunner
	outPath  string
	interval time.Duration
	lookback time.Duration
	logger   *slog.Logger
	now      func() time.Time
	mu       sync.Mutex
	prev     map[string]struct{}
}

// NewEOS returns an EOSPoller. lookback is clamped to max(2*interval, 30s)
// so consecutive ticks overlap and dedupe is effective.
func NewEOS(client eosLogRunner, workingDir string, interval, lookback time.Duration, logger *slog.Logger) *EOSPoller {
	lookback = max(lookback, 2*interval, eosMinLookback)
	return &EOSPoller{
		client:   client,
		outPath:  filepath.Join(workingDir, eosOutputFilename),
		interval: interval,
		lookback: lookback,
		logger:   logger,
		now:      time.Now,
		prev:     map[string]struct{}{},
	}
}

// Run polls immediately and then on every tick of interval, until ctx is
// canceled. Per-tick failures are logged at WARN and the loop continues.
func (p *EOSPoller) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.tick()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.tick()
		}
	}
}

func (p *EOSPoller) tick() {
	cmd := fmt.Sprintf("show logging last %d seconds", int(p.lookback.Seconds()))
	out, err := p.client.RunShowText(cmd)
	if err != nil {
		p.logger.Warn("eos logging poll failed", "cmd", cmd, "err", err)
		return
	}
	tNS := p.now().UTC().UnixNano()
	parsed, scanErr := parseEOSLog(out, tNS)
	if scanErr != nil {
		p.logger.Warn("eos logging parse truncated", "err", scanErr)
	}
	p.mu.Lock()
	next := make(map[string]struct{}, len(parsed))
	var fresh []eosLine
	for _, line := range parsed {
		key := line.raw
		next[key] = struct{}{}
		if _, seen := p.prev[key]; seen {
			continue
		}
		fresh = append(fresh, line)
	}
	p.prev = next
	p.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	if err := appendNDJSON(p.outPath, fresh); err != nil {
		p.logger.Warn("eos logging append failed", "path", p.outPath, "err", err)
	}
}

// Arista's default syslog format is:
//
//	MMM dd HH:MM:SS hostname FACILITY-SEV-MNEMONIC: message
//
// The capture is intentionally permissive: anything that doesn't match the
// timestamp+tag prefix still becomes a row with empty severity/facility.
var eosLineRE = regexp.MustCompile(`^(\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2})\s+\S+\s+([A-Za-z0-9_]+)-(\d)-[A-Za-z0-9_]+:\s*(.*)$`)

func parseEOSLog(text string, tNS int64) ([]eosLine, error) {
	var out []eosLine
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := eosLineRE.FindStringSubmatch(line); m != nil {
			out = append(out, eosLine{
				TNS:      tNS,
				Time:     m[1],
				Facility: m[2],
				Severity: m[3],
				Message:  m[4],
				raw:      line,
			})
			continue
		}
		out = append(out, eosLine{TNS: tNS, Message: line, raw: line})
	}
	return out, sc.Err()
}

func appendNDJSON(path string, rows []eosLine) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range rows {
		buf, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf = append(buf, '\n')
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

var _ collector.Collector = (*EOSPoller)(nil)
