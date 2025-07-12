package prometheus

import (
	"context"
	"net/http"

	prom "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	MetricNameGoMemstatsAllocBytes = "go_memstats_alloc_bytes"
	MetricNameGoGoroutines         = "go_goroutines"
)

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

func (m *MetricsClient) Fetch(ctx context.Context) error {
	resp, err := http.Get(m.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return err
	}

	m.families = families
	return nil
}

func (m *MetricsClient) GetCounter(name string) *prom.Counter {
	family, ok := m.families[name]
	if !ok {
		return nil
	}

	return family.Metric[0].Counter
}

func (m *MetricsClient) GetGauge(name string) *prom.Gauge {
	family, ok := m.families[name]
	if !ok {
		return nil
	}

	return family.Metric[0].Gauge
}
