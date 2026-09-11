package ripeatlas

import (
	"context"
	"log/slog"
	"strings"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/exporter"
)

const (
	CloudTimestampFileName = "ripe_atlas_cloud_timestamps.json"

	exchangeDescriptionPrefix = "DoubleZero "
	cloudDescriptionPrefix    = "DoubleZero Cloud "

	cloudTagSuffix      = "-cloud"
	cloudMeasurementTag = "doublezero-cloud"
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

func (c *Collector) timestampFileName() string {
	if c.cloudMode {
		return CloudTimestampFileName
	}
	return TimestampFileName
}

func (c *Collector) descriptionPrefix() string {
	if c.cloudMode {
		return cloudDescriptionPrefix
	}
	return exchangeDescriptionPrefix
}

func (c *Collector) measurementTag() string {
	if c.cloudMode {
		return c.env + cloudTagSuffix
	}
	return c.env
}

// The cloud prefix extends the exchange prefix, so exchange mode has to reject it explicitly.
func (c *Collector) ownsDescription(description string) bool {
	if !strings.HasPrefix(description, c.descriptionPrefix()) {
		return false
	}
	return c.cloudMode || !strings.HasPrefix(description, cloudDescriptionPrefix)
}

// Exchange descriptions end "to <code> probe <id>", cloud descriptions "to <code> target <address>".
func (c *Collector) targetLocationFromDescription(description string) (string, bool) {
	parts := strings.Split(description, " to ")
	if len(parts) != 2 {
		return "", false
	}
	marker := " probe"
	if c.cloudMode {
		marker = " target"
	}
	idx := strings.Index(parts[1], marker)
	if idx == -1 {
		return "", false
	}
	return parts[1][:idx], true
}
