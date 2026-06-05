package analyze

import (
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-reporter/pkg/parser"
)

func sample(secOffset int, cpu float64) parser.ProcessTopSample {
	return parser.ProcessTopSample{
		At:         time.Date(2024, 1, 1, 0, 0, secOffset, 0, time.UTC),
		CPUPercent: cpu,
	}
}

func TestCPUHotWindows_DetectsSustainedAtThreshold(t *testing.T) {
	// 8 samples at 10-s cadence. The first seven are ≥ 80 % (60 s
	// span: t=0..t=60, just meeting the minimum window). The eighth
	// drops below. Expectation: one detected window, peak = 85,
	// duration = 60 s.
	samples := []parser.ProcessTopSample{
		sample(0, 80),
		sample(10, 82),
		sample(20, 85),
		sample(30, 81),
		sample(40, 80),
		sample(50, 80),
		sample(60, 80),
		sample(70, 70), // breaks the stretch
	}
	wins := cpuHotWindows(samples)
	if len(wins) != 1 {
		t.Fatalf("want 1 window, got %d (%+v)", len(wins), wins)
	}
	if wins[0].PeakPct != 85 {
		t.Errorf("peak: want 85, got %v", wins[0].PeakPct)
	}
	if d := wins[0].Duration(); d != 60*time.Second {
		t.Errorf("window duration: want 60s, got %s", d)
	}
}

func TestCPUHotWindows_RequiresAtLeastSixtySecondSpan(t *testing.T) {
	// Sub-minimum stretch: 5 samples at 10-s cadence (40 s span) all ≥ 80%.
	// Expectation: no window (below 60 s).
	samples := []parser.ProcessTopSample{
		sample(0, 90),
		sample(10, 90),
		sample(20, 90),
		sample(30, 90),
		sample(40, 90),
		sample(50, 50), // break
	}
	if got := cpuHotWindows(samples); len(got) != 0 {
		t.Errorf("want 0 windows for 40 s stretch, got %d: %+v", len(got), got)
	}
}

func TestCPUHotWindows_LongStretchAcceptedAndPeakComputed(t *testing.T) {
	// 8 samples at 10-s cadence (70 s span) — well over the 60 s
	// minimum. Confirms detection at the typical case.
	samples := []parser.ProcessTopSample{
		sample(0, 81),
		sample(10, 95), // peak
		sample(20, 82),
		sample(30, 83),
		sample(40, 84),
		sample(50, 85),
		sample(60, 86),
		sample(70, 87),
	}
	wins := cpuHotWindows(samples)
	if len(wins) != 1 {
		t.Fatalf("want 1 window, got %d", len(wins))
	}
	if wins[0].PeakPct != 95 {
		t.Errorf("peak: want 95, got %v", wins[0].PeakPct)
	}
	if d := wins[0].Duration(); d != 70*time.Second {
		t.Errorf("duration: want 70s, got %s", d)
	}
}

func TestCPUHotWindows_SeparatedByCoolSampleStaySeparate(t *testing.T) {
	// Two sustained stretches with one sub-threshold sample between
	// them. They should NOT coalesce — the observer's check breaks on
	// any single cooler sample, and we match that shape.
	samples := []parser.ProcessTopSample{
		// First stretch, 70 s
		sample(0, 90), sample(10, 90), sample(20, 90), sample(30, 90),
		sample(40, 90), sample(50, 90), sample(60, 90), sample(70, 90),
		// Cool sample
		sample(80, 50),
		// Second stretch, 70 s
		sample(90, 90), sample(100, 90), sample(110, 90), sample(120, 90),
		sample(130, 90), sample(140, 90), sample(150, 90), sample(160, 90),
	}
	wins := cpuHotWindows(samples)
	if len(wins) != 2 {
		t.Fatalf("want 2 windows, got %d (%+v)", len(wins), wins)
	}
}

func runWith(samples []parser.ProcessTopSample, rss []parser.AgentMetricSample) *parser.Run {
	return &parser.Run{ProcessTopSamples: samples, AgentRSSSamples: rss}
}

func TestResourceStats_MemFloorViolation(t *testing.T) {
	samples := []parser.ProcessTopSample{
		{At: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), CPUPercent: 10, MemFreeKB: 200_000, MemUsedKB: 100_000},
		{At: time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC), CPUPercent: 10, MemFreeKB: 50_000, MemUsedKB: 250_000}, // below
		{At: time.Date(2024, 1, 1, 0, 0, 20, 0, time.UTC), CPUPercent: 10, MemFreeKB: 40_000, MemUsedKB: 260_000}, // below
		{At: time.Date(2024, 1, 1, 0, 0, 30, 0, time.UTC), CPUPercent: 10, MemFreeKB: 250_000, MemUsedKB: 50_000},
	}
	got := resourceStats(runWith(samples, nil), 100_000)
	if got.MemFloorViolations != 2 {
		t.Errorf("violations: want 2, got %d", got.MemFloorViolations)
	}
	want := time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC)
	if !got.MemFirstViolationAt.Equal(want) {
		t.Errorf("first violation timestamp: want %v, got %v", want, got.MemFirstViolationAt)
	}
	if got.MemPeakFreeKB != 250_000 || got.MemPeakUsedKB != 260_000 {
		t.Errorf("peak mem wrong: %+v", got)
	}
}

func TestResourceStats_MemFloorDisabled(t *testing.T) {
	samples := []parser.ProcessTopSample{
		{At: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), MemFreeKB: 1},
	}
	got := resourceStats(runWith(samples, nil), 0)
	if got.MemFloorViolations != 0 {
		t.Errorf("violations with floor=0: want 0, got %d", got.MemFloorViolations)
	}
}

func TestResourceStats_RSSSlopePositiveForIncreasingSeries(t *testing.T) {
	rss := []parser.AgentMetricSample{
		{TNS: 0, Value: 100},
		{TNS: int64(time.Second), Value: 200},
		{TNS: int64(2 * time.Second), Value: 300},
		{TNS: int64(3 * time.Second), Value: 400},
	}
	got := resourceStats(runWith(nil, rss), 0)
	if got.RSSSlopeBytesPerSec <= 0 {
		t.Errorf("strictly increasing series: want positive slope, got %v", got.RSSSlopeBytesPerSec)
	}
	if got.RSSPeakBytes != 400 {
		t.Errorf("peak: want 400, got %v", got.RSSPeakBytes)
	}
	if got.RSSEndBytes != 400 {
		t.Errorf("end: want 400, got %v", got.RSSEndBytes)
	}
}

func TestResourceStats_RSSSlopeNegativeForDecreasingSeries(t *testing.T) {
	rss := []parser.AgentMetricSample{
		{TNS: 0, Value: 400},
		{TNS: int64(time.Second), Value: 300},
		{TNS: int64(2 * time.Second), Value: 200},
		{TNS: int64(3 * time.Second), Value: 100},
	}
	got := resourceStats(runWith(nil, rss), 0)
	if got.RSSSlopeBytesPerSec >= 0 {
		t.Errorf("decreasing series: want negative slope, got %v", got.RSSSlopeBytesPerSec)
	}
}

func TestResourceStats_CPUPeakAndP95(t *testing.T) {
	samples := []parser.ProcessTopSample{
		sample(0, 10), sample(10, 20), sample(20, 30), sample(30, 40),
		sample(40, 50), sample(50, 60), sample(60, 70), sample(70, 80),
		sample(80, 90), sample(90, 100),
	}
	got := resourceStats(runWith(samples, nil), 0)
	if got.CPUPeakPct != 100 {
		t.Errorf("peak: want 100, got %v", got.CPUPeakPct)
	}
	if got.CPUP95Pct < 90 || got.CPUP95Pct > 100 {
		t.Errorf("p95 should be in [90, 100], got %v", got.CPUP95Pct)
	}
	if got.CPUSampleCount != 10 {
		t.Errorf("sample count: want 10, got %d", got.CPUSampleCount)
	}
}

func TestResourceStats_EmptyInputs(t *testing.T) {
	got := resourceStats(runWith(nil, nil), 1024)
	if got.CPUSampleCount != 0 || got.RSSSampleCount != 0 {
		t.Errorf("want empty stats, got %+v", got)
	}
	if len(got.HotWindows) != 0 {
		t.Errorf("want no hot windows, got %+v", got.HotWindows)
	}
}

func TestResourceStats_PropagatesSkippedCounts(t *testing.T) {
	r := &parser.Run{ProcessTopSkipped: 2, AgentMetricsSkipped: 5}
	got := resourceStats(r, 0)
	if got.ProcessTopSkipped != 2 || got.AgentMetricsSkipped != 5 {
		t.Errorf("skipped counts not propagated: %+v", got)
	}
}
