package loggingtail

import "github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"

func NewAgent(workingDir string) collector.Collector {
	_ = workingDir
	return collector.Noop{}
}
