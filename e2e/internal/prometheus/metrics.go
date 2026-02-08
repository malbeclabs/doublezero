package prometheus

import (
	"context"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	MetricNameGoMemstatsAllocBytes = "go_memstats_alloc_bytes"
	MetricNameGoGoroutines         = "go_goroutines"
)

type LabeledValue struct {
	Labels map[string]string
	Value  float64
}

type MetricsClient struct {
	url      string
	families map[string]*prom.MetricFamily
}

func NewMetricsClient(url string) *MetricsClient {
	return &MetricsClient{
		url:      url,
		families: make(map[string]*prom.MetricFamily),
	}
}

func (m *MetricsClient) WaitForReady(ctx context.Context, timeout time.Duration) error {
	return poll.Until(ctx, func() (bool, error) {
		err := m.Fetch(ctx)
		// Don't propagate transient errors - just keep polling
		return err == nil, nil
	}, timeout, 500*time.Millisecond)
}

func (m *MetricsClient) Fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return err
	}

	m.families = families
	return nil
}

func (m *MetricsClient) GetGaugeValues(name string) []LabeledValue {
	family, ok := m.families[name]
	if !ok {
		return nil
	}

	var values []LabeledValue
	for _, metric := range family.Metric {
		labels := make(map[string]string)
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		if metric.Gauge != nil {
			values = append(values, LabeledValue{
				Labels: labels,
				Value:  metric.Gauge.GetValue(),
			})
		}
	}
	return values
}

func (m *MetricsClient) GetCounterValues(name string) []LabeledValue {
	family, ok := m.families[name]
	if !ok {
		return nil
	}

	var values []LabeledValue
	for _, metric := range family.Metric {
		labels := make(map[string]string)
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		if metric.Counter != nil {
			values = append(values, LabeledValue{
				Labels: labels,
				Value:  metric.Counter.GetValue(),
			})
		}
	}
	return values
}
