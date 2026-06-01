// Package abort evaluates abort signals and writes a sentinel file when
// any trigger fires. Real implementation lands in PR #3796.
package abort

import "github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"

func New(abortFile string) collector.Collector {
	_ = abortFile
	return collector.Noop{}
}
