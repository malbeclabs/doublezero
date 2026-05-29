// Package runlog will tail the orchestrator-written run log. Real
// implementation lands in PR #3795.
package runlog

import "github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"

func New(workingDir string) collector.Collector {
	_ = workingDir
	return collector.Noop{}
}
