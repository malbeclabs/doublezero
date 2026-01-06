package isis

import (
	"math"
	"sort"
)

// LinkInfo represents a link between two routers.
type LinkInfo struct {
	Source string
	Dest   string
	Metric int
}

// NetworkStats holds aggregated network statistics.
type NetworkStats struct {
	TotalRouters      int
	TotalLinks        int
	HealthyRouters    int
	OverloadedRouters int
	IsolatedRouters   int
	SREnabledRouters  int

	// Computed percentages
	HealthyPercent float64
	SRPercent      float64
	AvgNeighbors   float64

	// SID range
	MinSID int
	MaxSID int

	// SRGB info
	SRGBStart      int
	SRGBEnd        int
	SRGBConsistent bool
	SRGBVariants   int
	HasSRGB        bool

	// Metric statistics
	MinMetric     int
	MaxMetric     int
	AvgMetric     float64
	MetricCount   int
	MinMetricLink *LinkInfo
	MaxMetricLink *LinkInfo

	// High cost links (metric > 50000)
	HighCostLinks []LinkInfo

	// Computed
	BidirectionalLinkCount int
}

// computeStats calculates network-wide statistics from router data.
func computeStats(routers map[string]Router) NetworkStats {
	stats := NetworkStats{
		TotalRouters: len(routers),
		MinMetric:    math.MaxInt,
		MaxMetric:    0,
		MinSID:       math.MaxInt,
		MaxSID:       0,
	}

	if len(routers) == 0 {
		stats.MinMetric = 0
		stats.MinSID = 0
		return stats
	}

	// Track unique links and SRGB values
	linkSet := make(map[string]struct{})
	srgbBases := make(map[int]struct{})
	srgbEnds := make(map[int]struct{})

	var totalNeighbors int
	var totalMetric int

	for hostname, router := range routers {
		// Health tracking
		if router.IsOverloaded {
			stats.OverloadedRouters++
		} else {
			stats.HealthyRouters++
		}

		// Isolated check
		if len(router.Neighbors) == 0 {
			stats.IsolatedRouters++
		}

		totalNeighbors += len(router.Neighbors)

		// SR tracking
		if router.NodeSID != nil {
			stats.SREnabledRouters++
			if *router.NodeSID < stats.MinSID {
				stats.MinSID = *router.NodeSID
			}
			if *router.NodeSID > stats.MaxSID {
				stats.MaxSID = *router.NodeSID
			}
		}

		// SRGB tracking
		if router.SRGBBase != nil {
			srgbBases[*router.SRGBBase] = struct{}{}
			if router.SRGBEnd != nil {
				srgbEnds[*router.SRGBEnd] = struct{}{}
			}
		}

		// Link metrics
		for _, neighbor := range router.Neighbors {
			// Create canonical link key (sorted hostnames)
			var linkKey string
			if hostname < neighbor.Hostname {
				linkKey = hostname + "|" + neighbor.Hostname
			} else {
				linkKey = neighbor.Hostname + "|" + hostname
			}

			if _, exists := linkSet[linkKey]; !exists {
				linkSet[linkKey] = struct{}{}
				stats.TotalLinks++

				metric := neighbor.Metric
				totalMetric += metric
				stats.MetricCount++

				if metric < stats.MinMetric {
					stats.MinMetric = metric
					stats.MinMetricLink = &LinkInfo{
						Source: hostname,
						Dest:   neighbor.Hostname,
						Metric: metric,
					}
				}

				if metric > stats.MaxMetric {
					stats.MaxMetric = metric
					stats.MaxMetricLink = &LinkInfo{
						Source: hostname,
						Dest:   neighbor.Hostname,
						Metric: metric,
					}
				}

				// High cost links (metric > 50000)
				if metric > 50000 {
					stats.HighCostLinks = append(stats.HighCostLinks, LinkInfo{
						Source: hostname,
						Dest:   neighbor.Hostname,
						Metric: metric,
					})
				}
			}
		}
	}

	// Sort high cost links by metric descending
	sort.Slice(stats.HighCostLinks, func(i, j int) bool {
		return stats.HighCostLinks[i].Metric > stats.HighCostLinks[j].Metric
	})

	// Compute percentages and averages
	if stats.TotalRouters > 0 {
		stats.HealthyPercent = float64(stats.HealthyRouters) / float64(stats.TotalRouters) * 100
		stats.SRPercent = float64(stats.SREnabledRouters) / float64(stats.TotalRouters) * 100
		stats.AvgNeighbors = float64(totalNeighbors) / float64(stats.TotalRouters)
	}

	if stats.MetricCount > 0 {
		stats.AvgMetric = float64(totalMetric) / float64(stats.MetricCount)
	}

	// Handle edge case when no links/SIDs found
	if stats.MinMetric == math.MaxInt {
		stats.MinMetric = 0
	}
	if stats.MinSID == math.MaxInt {
		stats.MinSID = 0
	}

	// SRGB consistency
	stats.HasSRGB = len(srgbBases) > 0
	stats.SRGBVariants = len(srgbBases)
	stats.SRGBConsistent = len(srgbBases) == 1 && len(srgbEnds) == 1

	if stats.HasSRGB {
		// Find min base and max end
		stats.SRGBStart = math.MaxInt
		stats.SRGBEnd = 0
		for base := range srgbBases {
			if base < stats.SRGBStart {
				stats.SRGBStart = base
			}
		}
		for end := range srgbEnds {
			if end > stats.SRGBEnd {
				stats.SRGBEnd = end
			}
		}
		if stats.SRGBEnd == 0 {
			stats.SRGBEnd = stats.SRGBStart
		}
	}

	stats.BidirectionalLinkCount = stats.TotalLinks * 2

	return stats
}
