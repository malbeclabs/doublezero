package ripeatlas

import (
	"context"
	"log/slog"
	"slices"
	"sort"
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

// A nil result means the check failed, which leaves every region on the probe it has enlisted.
func (c *Collector) cloudLiveProbes(ctx context.Context) map[int]bool {
	probeIDs := make([]int, 0, len(c.cloudNodes))
	for _, node := range c.cloudNodes {
		probeIDs = append(probeIDs, node.AtlasProbeIDs...)
	}
	sort.Ints(probeIDs)

	live, err := c.client.GetConnectedProbeIDs(ctx, probeIDs)
	if err != nil {
		c.log.Warn("Failed to check cloud probe liveness, keeping the enlisted probes",
			slog.String("error", err.Error()))
		return nil
	}

	c.log.Info("Checked cloud probe liveness",
		slog.Int("configured", len(probeIDs)),
		slog.Int("connected", len(live)))
	return live
}

// Each region gets one probe, taken from its own list in the node file and nowhere else.
func (c *Collector) selectCloudProbes(locationMatches []LocationProbeMatch, live map[int]bool, measurementState *MeasurementState) []LocationProbeMatch {
	enlisted := enlistedProbesByLocation(measurementState)

	result := make([]LocationProbeMatch, len(locationMatches))
	copy(result, locationMatches)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.probeToLocation = make(map[int]string)

	for i, match := range result {
		result[i].NearbyProbes = nil
		result[i].ProbeCount = 0

		node, ok := c.cloudNodes[match.LocationCode]
		if !ok {
			c.log.Warn("No node file entry for location",
				slog.String("location", match.LocationCode))
			continue
		}

		probeID := cloudSourceProbe(node.AtlasProbeIDs, enlisted[match.LocationCode], live, measurementState)
		if probeID == 0 {
			c.log.Warn("No live probe for region, it contributes no samples",
				slog.String("location", match.LocationCode),
				slog.Any("configured_probe_ids", node.AtlasProbeIDs))
			continue
		}

		result[i].NearbyProbes = []Probe{{ID: probeID, Latitude: node.Latitude, Longitude: node.Longitude}}
		result[i].ProbeCount = 1
		c.probeToLocation[probeID] = match.LocationCode
	}

	return result
}

// The enlisted probe keeps the seat while it is Connected and not marked unresponsive. A region
// whose every candidate is gone keeps its dead probe rather than dropping out.
func cloudSourceProbe(configured []int, enlisted int, live map[int]bool, measurementState *MeasurementState) int {
	if !slices.Contains(configured, enlisted) {
		enlisted = 0
	}

	usable := func(probeID int) bool {
		return live[probeID] && !measurementState.IsProbeUnresponsive(probeID)
	}

	if usable(enlisted) {
		return enlisted
	}
	for _, probeID := range configured {
		if usable(probeID) {
			return probeID
		}
	}
	return enlisted
}

// The enlisted probe is the choice earlier cycles made. RIPE measurement IDs rise over time, so
// the highest ID carries the most recent choice.
func enlistedProbesByLocation(measurementState *MeasurementState) map[string]int {
	metadata := measurementState.GetAllMetadata()
	measurementIDs := make([]int, 0, len(metadata))
	for measurementID := range metadata {
		measurementIDs = append(measurementIDs, measurementID)
	}
	sort.Ints(measurementIDs)

	enlisted := make(map[string]int)
	for _, measurementID := range measurementIDs {
		for _, source := range metadata[measurementID].Sources {
			enlisted[source.LocationCode] = source.ProbeID
		}
	}
	return enlisted
}
