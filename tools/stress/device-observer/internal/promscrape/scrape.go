// Package promscrape will scrape the doublezero-agent's Prometheus metrics
// endpoint. Real implementation lands in PR #3794.
package promscrape

import "github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"

func New(metricsURL, workingDir string) collector.Collector {
	_, _ = metricsURL, workingDir
	return collector.Noop{}
}
