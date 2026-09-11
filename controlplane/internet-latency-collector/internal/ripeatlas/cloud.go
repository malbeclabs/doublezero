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
	clouds := make(map[string]string, len(nodes))
	for _, node := range nodes {
		byCode[node.Code] = node
		clouds[node.Code] = node.Cloud
	}

	return &Collector{
		client:          NewClient(logger),
		log:             logger,
		exporter:        exporter,
		env:             env,
		probeToLocation: make(map[int]string),
		cloudMode:       true,
		cloudNodes:      byCode,
		cloudByLocation: clouds,
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

// SetCloudExport turns on cloud export, mapping each location code to its cloud ("us-east-1" to
// "aws"). Records then carry cloud identity, and a result that got no reply is exported at zero RTT.
func (c *Collector) SetCloudExport(clouds map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cloudByLocation = clouds
}

func (c *Collector) cloudExportEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cloudByLocation) > 0
}

// originCode is the location that was pinged, targetCode the location it was pinged from.
func (c *Collector) cloudInfoFor(originCode, targetCode string, probeID, sent, received int) (*exporter.CloudInfo, bool) {
	c.mu.RLock()
	originCloud := c.cloudByLocation[originCode]
	targetCloud := c.cloudByLocation[targetCode]
	c.mu.RUnlock()

	if originCloud == "" || targetCloud == "" || probeID <= 0 {
		c.log.Warn("Incomplete cloud identity, skipping record",
			slog.String("origin_code", originCode),
			slog.String("target_code", targetCode),
			slog.Int("probe_id", probeID))
		return nil, false
	}

	return &exporter.CloudInfo{
		SourceCloud:     originCloud,
		TargetCloud:     targetCloud,
		ProbeID:         uint32(probeID),
		PacketsSent:     clampPacketCount(sent),
		PacketsReceived: clampPacketCount(received),
	}, true
}

// RIPE reports the counts as "sent" and "rcvd"; without them, an entry carrying an rtt is a reply.
func parsePacketCountsFromResult(result any) (int, int) {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return 0, 0
	}

	if sent, ok := resultMap["sent"].(float64); ok && sent > 0 {
		received, _ := resultMap["rcvd"].(float64)
		return int(sent), int(received)
	}

	sent := 0
	received := 0
	if pings, ok := resultMap["result"].([]any); ok {
		for _, ping := range pings {
			pingMap, ok := ping.(map[string]any)
			if !ok {
				continue
			}
			sent++
			if rtt, ok := pingMap["rtt"].(float64); ok && rtt > 0 {
				received++
			}
		}
	}

	return sent, received
}

func clampPacketCount(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
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
