package analyze

import (
	"sort"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

// CPUHotThresholdPct mirrors the observer's `cpuPercentThresh` — the
// per-sample CPU value (sum of non-idle %Cpu(s) fields) at-or-above
// which a sample counts as "hot". Sustained at this level over
// CPUHotMinWindow fires the observer's cpu_sustained abort.
const CPUHotThresholdPct = 80.0

// CPUHotMinWindow mirrors the observer's `cpuSustainedWindow`.
const CPUHotMinWindow = 60 * time.Second

// CPUHotWindow is a contiguous stretch of samples each at or above
// CPUHotThresholdPct whose Start..End span is at least CPUHotMinWindow.
type CPUHotWindow struct {
	Start   time.Time
	End     time.Time
	PeakPct float64
}

func (w CPUHotWindow) Duration() time.Duration { return w.End.Sub(w.Start) }

// ResourceStats is the rolled-up view of device CPU/memory and the
// agent's resident memory across a run. Empty (zero counts) when the
// observer was disabled.
type ResourceStats struct {
	CPUSampleCount int
	CPUPeakPct     float64
	CPUP95Pct      float64
	HotWindows     []CPUHotWindow

	MemSampleCount int
	MemPeakFreeKB  uint64
	MemPeakUsedKB  uint64
	// MemFloorKB is the configured floor (0 = check disabled).
	MemFloorKB          uint64
	MemFloorViolations  int
	MemFirstViolationAt time.Time

	RSSSampleCount int
	RSSPeakBytes   float64
	RSSEndBytes    float64
	// RSSSlopeBytesPerSec is the OLS slope of the RSS series against
	// wall-clock seconds. Positive = growing across the run. Zero when
	// fewer than two samples exist.
	RSSSlopeBytesPerSec float64
}

func resourceStats(samples []parser.ProcessTopSample, rss []parser.AgentMetricSample, memFloorKB uint64) ResourceStats {
	out := ResourceStats{
		CPUSampleCount: len(samples),
		MemSampleCount: len(samples),
		MemFloorKB:     memFloorKB,
		RSSSampleCount: len(rss),
	}

	if len(samples) > 0 {
		cpus := make([]float64, len(samples))
		for i, s := range samples {
			cpus[i] = s.CPUPercent
			if s.CPUPercent > out.CPUPeakPct {
				out.CPUPeakPct = s.CPUPercent
			}
			if s.MemFreeKB > out.MemPeakFreeKB {
				out.MemPeakFreeKB = s.MemFreeKB
			}
			if s.MemUsedKB > out.MemPeakUsedKB {
				out.MemPeakUsedKB = s.MemUsedKB
			}
			if memFloorKB > 0 && s.MemFreeKB < memFloorKB {
				out.MemFloorViolations++
				if out.MemFirstViolationAt.IsZero() {
					out.MemFirstViolationAt = s.At
				}
			}
		}
		sort.Float64s(cpus)
		out.CPUP95Pct = Percentile(cpus, 0.95)
		out.HotWindows = cpuHotWindows(samples)
	}

	if len(rss) > 0 {
		for _, m := range rss {
			if m.Value > out.RSSPeakBytes {
				out.RSSPeakBytes = m.Value
			}
		}
		out.RSSEndBytes = rss[len(rss)-1].Value
		if len(rss) >= 2 {
			t0 := rss[0].TNS
			xs := make([]float64, len(rss))
			ys := make([]float64, len(rss))
			for i, m := range rss {
				xs[i] = float64(m.TNS-t0) / float64(time.Second)
				ys[i] = m.Value
			}
			out.RSSSlopeBytesPerSec = LinearLeastSquares(xs, ys).Slope
		}
	}

	return out
}

// cpuHotWindows scans the time-sorted sample slice and returns one
// CPUHotWindow per contiguous stretch where every sample is ≥
// CPUHotThresholdPct AND the stretch's wall-clock span is ≥
// CPUHotMinWindow. A single sub-threshold sample breaks a window:
// matches the observer's abort decider, which also breaks on any
// cooler sample inside the 60-s ring.
func cpuHotWindows(samples []parser.ProcessTopSample) []CPUHotWindow {
	var out []CPUHotWindow
	startIdx := -1
	flush := func(endIdx int) {
		if startIdx < 0 {
			return
		}
		start := samples[startIdx].At
		end := samples[endIdx].At
		if end.Sub(start) >= CPUHotMinWindow {
			peak := samples[startIdx].CPUPercent
			for i := startIdx + 1; i <= endIdx; i++ {
				if samples[i].CPUPercent > peak {
					peak = samples[i].CPUPercent
				}
			}
			out = append(out, CPUHotWindow{Start: start, End: end, PeakPct: peak})
		}
		startIdx = -1
	}
	for i, s := range samples {
		if s.CPUPercent >= CPUHotThresholdPct {
			if startIdx < 0 {
				startIdx = i
			}
			continue
		}
		flush(i - 1)
	}
	flush(len(samples) - 1)
	return out
}
