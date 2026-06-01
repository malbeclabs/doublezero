// Package promscrape scrapes the doublezero-agent's Prometheus metrics
// endpoint on every tick and appends one NDJSON row per metric sample to
// observer.agent_metrics.json in the working directory. It also exposes a
// thread-safe Snapshot of the latest counter values.
package promscrape

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	// Matches e2e/internal/prometheus/metrics.go for uniform behavior.
	requestTimeout = 5 * time.Second

	// Bounds memory from a misbehaving or compromised endpoint. The agent
	// emits a few kB in practice.
	maxBodyBytes = 16 << 20

	outputFilename = "observer.agent_metrics.json"
)

// Scraper polls a Prometheus metrics endpoint, persists samples as NDJSON,
// and tracks the latest counter values for downstream consumers.
type Scraper struct {
	metricsURL string
	outPath    string
	interval   time.Duration
	logger     *slog.Logger
	client     *http.Client

	mu       sync.RWMutex
	counters map[string]float64

	now func() time.Time
}

func New(metricsURL, workingDir string, interval time.Duration, logger *slog.Logger) *Scraper {
	return &Scraper{
		metricsURL: metricsURL,
		outPath:    filepath.Join(workingDir, outputFilename),
		interval:   interval,
		logger:     logger,
		client:     &http.Client{},
		counters:   map[string]float64{},
		now:        time.Now,
	}
}

// Run scrapes the metrics endpoint immediately and then on every tick of
// interval, until ctx is canceled. Per-tick errors are logged at WARN and
// do not abort the loop.
func (s *Scraper) Run(ctx context.Context) error {
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

// Snapshot returns a copy of the latest counter family totals. Returns an
// empty map before the first successful tick. Failed ticks do not clear the
// previous values, so callers can compute deltas without seeing spurious
// resets.
func (s *Scraper) Snapshot() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]float64, len(s.counters))
	for k, v := range s.counters {
		out[k] = v
	}
	return out
}

func (s *Scraper) tick(ctx context.Context) {
	families, err := s.fetch(ctx)
	if err != nil {
		s.logger.Warn("scrape fetch failed", "url", s.metricsURL, "err", err)
		return
	}
	// Treat an empty-but-2xx response as a soft failure so a transient
	// empty body cannot look like "counters reset to zero" to a caller.
	if len(families) == 0 {
		s.logger.Warn("scrape returned no metric families", "url", s.metricsURL)
		return
	}
	tNS := s.now().UTC().UnixNano()
	rows, counters := encodeFamilies(families, tNS)
	// A write failure freezes the snapshot so disk and the in-memory
	// snapshot never disagree.
	if err := s.appendRows(rows); err != nil {
		s.logger.Warn("scrape append failed", "path", s.outPath, "err", err)
		return
	}
	s.mu.Lock()
	s.counters = counters
	s.mu.Unlock()
}

func (s *Scraper) fetch(ctx context.Context) (map[string]*prom.MetricFamily, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.metricsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	return parser.TextToMetricFamilies(io.LimitReader(resp.Body, maxBodyBytes))
}

// appendRows writes the buffered NDJSON in a single Write. The observer is
// the only writer of the file, so concurrent interleaving is not a concern.
func (s *Scraper) appendRows(rows []byte) error {
	f, err := os.OpenFile(s.outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(rows); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

type metricRow struct {
	TNS        int64   `json:"t_ns"`
	MetricName string  `json:"metric_name"`
	Value      float64 `json:"value"`
	LabelsJSON string  `json:"labels_json"`
}

// encodeFamilies converts parsed metric families into NDJSON rows and a map
// of counter totals (family name → sum across all label series). Families
// are walked in sorted order so output is deterministic. Only counters
// contribute to the totals map; downstream consumers want counter deltas.
func encodeFamilies(families map[string]*prom.MetricFamily, tNS int64) ([]byte, map[string]float64) {
	counters := map[string]float64{}
	if len(families) == 0 {
		return nil, counters
	}
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	for _, name := range names {
		family := families[name]
		for _, m := range family.GetMetric() {
			labels := labelMap(m)
			switch family.GetType() {
			case prom.MetricType_COUNTER:
				v := m.GetCounter().GetValue()
				emitRow(&buf, tNS, name, v, labels)
				counters[name] += v
			case prom.MetricType_GAUGE:
				emitRow(&buf, tNS, name, m.GetGauge().GetValue(), labels)
			case prom.MetricType_UNTYPED:
				emitRow(&buf, tNS, name, m.GetUntyped().GetValue(), labels)
			case prom.MetricType_SUMMARY:
				sum := m.GetSummary()
				emitRow(&buf, tNS, name+"_sum", sum.GetSampleSum(), labels)
				emitRow(&buf, tNS, name+"_count", float64(sum.GetSampleCount()), labels)
				for _, q := range sum.GetQuantile() {
					ql := mergeLabel(labels, "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64))
					emitRow(&buf, tNS, name, q.GetValue(), ql)
				}
			case prom.MetricType_HISTOGRAM:
				h := m.GetHistogram()
				emitRow(&buf, tNS, name+"_sum", h.GetSampleSum(), labels)
				emitRow(&buf, tNS, name+"_count", float64(h.GetSampleCount()), labels)
				for _, b := range h.GetBucket() {
					bl := mergeLabel(labels, "le", strconv.FormatFloat(b.GetUpperBound(), 'g', -1, 64))
					emitRow(&buf, tNS, name+"_bucket", float64(b.GetCumulativeCount()), bl)
				}
			}
		}
	}
	return buf.Bytes(), counters
}

func emitRow(buf *bytes.Buffer, tNS int64, name string, value float64, labels map[string]string) {
	// Sanitize NaN/Inf so a pathological value cannot fail the whole tick.
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}
	labelBytes, err := json.Marshal(labels)
	if err != nil {
		labelBytes = []byte(`{}`)
	}
	row, err := json.Marshal(metricRow{
		TNS:        tNS,
		MetricName: name,
		Value:      value,
		LabelsJSON: string(labelBytes),
	})
	if err != nil {
		return
	}
	buf.Write(row)
	buf.WriteByte('\n')
}

func labelMap(m *prom.Metric) map[string]string {
	out := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}

func mergeLabel(base map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for kk, vv := range base {
		out[kk] = vv
	}
	out[k] = v
	return out
}

var _ collector.Collector = (*Scraper)(nil)
