package querier

import (
	dzsvc "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/latency"
	dztelemusage "github.com/malbeclabs/doublezero/lake/pkg/indexer/dz/telemetry/usage"
	mcpgeoip "github.com/malbeclabs/doublezero/lake/pkg/indexer/geoip"
	sol "github.com/malbeclabs/doublezero/lake/pkg/indexer/sol"
)

var Datasets = Merge(
	dzsvc.Datasets,
	dztelemlatency.Datasets,
	dztelemusage.Datasets,
	sol.Datasets,
	mcpgeoip.Datasets,
)

func Merge[T any](xs ...[]T) []T {
	n := 0
	for _, s := range xs {
		n += len(s)
	}
	out := make([]T, 0, n)
	for _, s := range xs {
		out = append(out, s...)
	}
	return out
}
