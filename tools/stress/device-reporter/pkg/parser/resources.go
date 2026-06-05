package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProcessTopSample is one parsed `show processes top once` snapshot
// from the observer's per-tick capture. Memory fields are in kilobytes
// (the EOS schema unit).
type ProcessTopSample struct {
	At         time.Time
	CPUPercent float64
	MemFreeKB  uint64
	MemUsedKB  uint64
	MemTotalKB uint64
}

// AgentMetricSample is one observer.agent_metrics.json row filtered
// to a specific metric_name.
type AgentMetricSample struct {
	TNS   int64
	Value float64
}

const (
	processTopGlob       = "show-processes-top-once-*.json"
	agentMetricsFilename = "observer.agent_metrics.json"
)

// LoadProcessTopSamples reads every `show-processes-top-once-<ts>.json`
// in dir and returns the parsed samples sorted by sample-time ascending.
// `skipped` counts files that were present but unparseable, so the
// markdown writer can distinguish "observer disabled" (no files) from
// "files present but corrupt" — important for a forensics tool.
func LoadProcessTopSamples(dir string) (samples []ProcessTopSample, skipped int, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, processTopGlob))
	if err != nil {
		return nil, 0, err
	}
	samples = make([]ProcessTopSample, 0, len(paths))
	for _, p := range paths {
		buf, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, 0, fmt.Errorf("read %s: %w", p, err)
		}
		s, ok := parseProcessTopSample(buf, filepath.Base(p))
		if !ok {
			skipped++
			continue
		}
		samples = append(samples, s)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].At.Before(samples[j].At) })
	return samples, skipped, nil
}

// LoadAgentMetrics returns NDJSON rows from observer.agent_metrics.json
// whose metric_name equals `metric`, sorted by t_ns ascending.
// `skipped` counts non-empty rows that failed to unmarshal — surfaced
// so the writer can warn instead of silently producing an empty
// section. Missing file → empty slice + nil error.
func LoadAgentMetrics(dir, metric string) (rows []AgentMetricSample, skipped int, err error) {
	path := filepath.Join(dir, agentMetricsFilename)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Label-heavy histogram bucket rows can exceed the 64 KB default.
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var row struct {
			TNS        int64   `json:"t_ns"`
			MetricName string  `json:"metric_name"`
			Value      float64 `json:"value"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			skipped++
			continue
		}
		if row.MetricName != metric {
			continue
		}
		rows = append(rows, AgentMetricSample{TNS: row.TNS, Value: row.Value})
	}
	if err := sc.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan %s: %w", path, err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TNS < rows[j].TNS })
	return rows, skipped, nil
}

func parseProcessTopSample(buf []byte, basename string) (ProcessTopSample, bool) {
	var env struct {
		CPUInfo map[string]map[string]float64 `json:"cpuInfo"`
		MemInfo struct {
			PhysicalMem struct {
				MemFree  uint64 `json:"memFree"`
				MemUsed  uint64 `json:"memUsed"`
				MemTotal uint64 `json:"memTotal"`
			} `json:"physicalMem"`
		} `json:"memInfo"`
		TimeInfo struct {
			CurrentTime float64 `json:"currentTime"`
		} `json:"timeInfo"`
	}
	if err := json.Unmarshal(buf, &env); err != nil {
		return ProcessTopSample{}, false
	}
	cpu, ok := totalCPUFromMap(env.CPUInfo)
	if !ok {
		return ProcessTopSample{}, false
	}
	ts := timeFromCurrentTime(env.TimeInfo.CurrentTime)
	if ts.IsZero() {
		ts = timeFromFilename(basename)
	}
	if ts.IsZero() {
		return ProcessTopSample{}, false
	}
	return ProcessTopSample{
		At:         ts,
		CPUPercent: cpu,
		MemFreeKB:  env.MemInfo.PhysicalMem.MemFree,
		MemUsedKB:  env.MemInfo.PhysicalMem.MemUsed,
		MemTotalKB: env.MemInfo.PhysicalMem.MemTotal,
	}, true
}

// totalCPUFromMap sums every non-idle `%Cpu(s)` field, matching the
// observer's parseCPUPercent. Returns (0, false) if `idle` is absent —
// without it we can't be sure we have the right object.
func totalCPUFromMap(info map[string]map[string]float64) (float64, bool) {
	fields, ok := info["%Cpu(s)"]
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

func timeFromCurrentTime(currentTime float64) time.Time {
	if currentTime <= 0 {
		return time.Time{}
	}
	sec := int64(currentTime)
	frac := currentTime - float64(sec)
	return time.Unix(sec, int64(frac*1e9)).UTC()
}

// timeFromFilename parses the observer's filename suffix. The observer
// renders timestamps as `2006-01-02T15-04-05.000000000Z` (colons
// swapped to dashes for filesystem portability); we restore the two
// time-component colons after `T` to parse it back.
func timeFromFilename(name string) time.Time {
	const prefix = "show-processes-top-once-"
	const suffix = ".json"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	tIdx := strings.Index(stamp, "T")
	if tIdx < 0 {
		return time.Time{}
	}
	timePart := strings.Replace(stamp[tIdx+1:], "-", ":", 2)
	t, err := time.Parse("2006-01-02T15:04:05.000000000Z", stamp[:tIdx]+"T"+timePart)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
