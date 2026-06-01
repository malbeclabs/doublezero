// Package loggingtail will follow agent and orchestrator logs. Real
// implementations land in PR #3795.
package loggingtail

import (
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/collector"
	"github.com/malbeclabs/doublezero/tools/stress/device-observer/internal/eapi"
)

func NewEOS(client *eapi.Client, workingDir string) collector.Collector {
	_, _ = client, workingDir
	return collector.Noop{}
}
