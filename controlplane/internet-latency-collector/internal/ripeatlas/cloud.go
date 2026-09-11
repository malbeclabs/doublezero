package ripeatlas

import (
	"context"
	"log/slog"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

type CloudNode struct {
	Code          string
	Cloud         string
	Latitude      float64
	Longitude     float64
	AtlasProbeIDs []int
	PingTarget    string
}

func NewCloudCollector(logger *slog.Logger, exporter exporter.Exporter, env string, nodes []CloudNode) *Collector {
	byCode := make(map[string]CloudNode, len(nodes))
	for _, node := range nodes {
		byCode[node.Code] = node
	}

	return &Collector{
		client:          NewClient(logger),
		log:             logger,
		exporter:        exporter,
		env:             env,
		probeToLocation: make(map[int]string),
		cloudMode:       true,
		cloudNodes:      byCode,
		getLocationsFunc: func(_ context.Context) []collector.LocationMatch {
			locations := make([]collector.LocationMatch, 0, len(nodes))
			for _, node := range nodes {
				locations = append(locations, collector.LocationMatch{
					LocationCode: node.Code,
					Latitude:     node.Latitude,
					Longitude:    node.Longitude,
				})
			}
			return locations
		},
	}
}
