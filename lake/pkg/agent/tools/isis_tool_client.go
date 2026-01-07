package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/isis"
)

// ISISEnricher defines the interface for ISIS enrichment operations.
type ISISEnricher interface {
	EnrichFromReader(ctx context.Context, r io.Reader, timestamp string) (*isis.Result, error)
	EnrichFromFile(ctx context.Context, path string) (*isis.Result, error)
}

// ISISS3Fetcher defines the interface for fetching ISIS data from S3.
type ISISS3Fetcher interface {
	FetchLatest(ctx context.Context) (*isis.FetchLatestResult, error)
}

// ISISToolClientConfig holds configuration for the ISISToolClient.
type ISISToolClientConfig struct {
	// S3Bucket is the S3 bucket for ISIS data (optional, uses default).
	S3Bucket string
	// S3Region is the AWS region for the S3 bucket (optional, uses default).
	S3Region string
}

// ISISToolClient implements react.ToolClient for ISIS topology operations.
type ISISToolClient struct {
	enricher  ISISEnricher
	s3Fetcher ISISS3Fetcher

	// Cached state
	mu              sync.RWMutex
	cachedResult    *isis.Result
	cachedTimestamp string
}

// NewISISToolClient creates a new ISISToolClient with the given configuration.
func NewISISToolClient(cfg ISISToolClientConfig) (*ISISToolClient, error) {
	enricher, err := isis.NewEnricher(isis.EnricherConfig{Level: 2})
	if err != nil {
		return nil, fmt.Errorf("failed to create enricher: %w", err)
	}

	s3Fetcher := isis.NewS3Fetcher(isis.S3FetcherConfig{
		Bucket: cfg.S3Bucket,
		Region: cfg.S3Region,
	})

	return &ISISToolClient{
		enricher:  enricher,
		s3Fetcher: s3Fetcher,
	}, nil
}

// NewISISToolClientWithDeps creates a new ISISToolClient with injected dependencies (for testing).
func NewISISToolClientWithDeps(cfg ISISToolClientConfig, enricher ISISEnricher, fetcher ISISS3Fetcher) *ISISToolClient {
	return &ISISToolClient{
		enricher:  enricher,
		s3Fetcher: fetcher,
	}
}

// ListTools returns the available ISIS tools.
func (c *ISISToolClient) ListTools(_ context.Context) ([]react.Tool, error) {
	return []react.Tool{
		{
			Name:        "isis_refresh",
			Description: "Fetch latest ISIS topology from S3 and cache locally. Use this to get fresh network topology data.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": map[string]any{
						"type":        "integer",
						"description": "ISIS level to process (1 or 2). Default: 2",
						"enum":        []int{1, 2},
					},
					"source": map[string]any{
						"type":        "string",
						"description": "Data source: 's3' (default) to fetch from S3, or a local file path",
					},
				},
			},
		},
		{
			Name:        "isis_get_summary",
			Description: "Get network summary statistics from cached ISIS enrichment. Returns router counts, link counts, health percentages, and SR statistics.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "isis_list_routers",
			Description: "List all ISIS routers with basic info (hostname, location, router type). Use to identify device IDs before querying telemetry or to filter by location. Essential for device identity verification (DZD, router names).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "Filter by location (e.g., 'NYC', 'London'). Case-insensitive partial match.",
					},
				},
			},
		},
		{
			Name:        "isis_get_router",
			Description: "Get full details for a specific router including neighbors, reachabilities, and SR config.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hostname": map[string]any{
						"type":        "string",
						"description": "Exact hostname of the router to look up.",
					},
				},
				"required": []string{"hostname"},
			},
		},
		{
			Name:        "isis_get_adjacencies",
			Description: "Get network adjacency graph showing router connections and link metrics. Use this BEFORE querying telemetry to understand which links connect which routers. Essential for path analysis (e.g., 'path from X to Y'). Returns list of {source, dest, metric} tuples.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"router": map[string]any{
						"type":        "string",
						"description": "Filter to neighbors of specific router (optional). If not provided, returns all adjacencies.",
					},
				},
			},
		},
	}, nil
}

// CallToolText calls a tool and returns the result as text.
func (c *ISISToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	switch name {
	case "isis_refresh":
		return c.callRefresh(ctx, args)
	case "isis_get_summary":
		return c.callGetSummary(ctx)
	case "isis_list_routers":
		return c.callListRouters(ctx, args)
	case "isis_get_router":
		return c.callGetRouter(ctx, args)
	case "isis_get_adjacencies":
		return c.callGetAdjacencies(ctx, args)
	default:
		return "", true, fmt.Errorf("unknown tool: %s", name)
	}
}

// refreshResponse is the JSON response for isis_refresh.
type refreshResponse struct {
	Timestamp      string  `json:"timestamp"`
	RouterCount    int     `json:"router_count"`
	LinkCount      int     `json:"link_count"`
	HealthyPercent float64 `json:"healthy_percent"`
}

func (c *ISISToolClient) callRefresh(ctx context.Context, args map[string]any) (string, bool, error) {
	source := "s3"
	if s, ok := args["source"].(string); ok && s != "" {
		source = s
	}

	var result *isis.Result
	var timestamp string
	var err error

	if source == "s3" {
		// Fetch from S3
		fetchResult, fetchErr := c.s3Fetcher.FetchLatest(ctx)
		if fetchErr != nil {
			return fmt.Sprintf("failed to fetch from S3: %v", fetchErr), true, nil
		}
		defer fetchResult.Body.Close()

		timestamp = fetchResult.Timestamp
		result, err = c.enricher.EnrichFromReader(ctx, fetchResult.Body, timestamp)
		if err != nil {
			return fmt.Sprintf("failed to enrich data: %v", err), true, nil
		}
	} else {
		// Fetch from local file
		result, err = c.enricher.EnrichFromFile(ctx, source)
		if err != nil {
			return fmt.Sprintf("failed to enrich file %s: %v", source, err), true, nil
		}
		// Extract timestamp from filename would happen in EnrichFromFile
		timestamp = "local file"
	}

	// Cache the result
	c.mu.Lock()
	c.cachedResult = result
	c.cachedTimestamp = timestamp
	c.mu.Unlock()

	// Build response
	response := refreshResponse{
		Timestamp:      timestamp,
		RouterCount:    result.Stats.TotalRouters,
		LinkCount:      result.Stats.TotalLinks,
		HealthyPercent: result.Stats.HealthyPercent,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Sprintf("failed to marshal response: %v", err), true, nil
	}

	return string(jsonBytes), false, nil
}

func (c *ISISToolClient) callGetSummary(_ context.Context) (string, bool, error) {
	c.mu.RLock()
	result := c.cachedResult
	c.mu.RUnlock()

	if result == nil {
		return "no cached ISIS data available. Run isis_refresh first.", true, nil
	}

	jsonBytes, err := json.Marshal(result.Stats)
	if err != nil {
		return fmt.Sprintf("failed to marshal stats: %v", err), true, nil
	}

	return string(jsonBytes), false, nil
}

// routerSummary is the JSON response item for isis_list_routers.
type routerSummary struct {
	Hostname      string `json:"hostname"`
	Location      string `json:"location"`
	RouterType    string `json:"router_type"`
	IsHealthy     bool   `json:"is_healthy"`
	NeighborCount int    `json:"neighbor_count"`
}

func (c *ISISToolClient) callListRouters(_ context.Context, args map[string]any) (string, bool, error) {
	c.mu.RLock()
	result := c.cachedResult
	c.mu.RUnlock()

	if result == nil {
		return "no cached ISIS data available. Run isis_refresh first.", true, nil
	}

	// Extract optional location filter
	var locationFilter string
	if loc, ok := args["location"].(string); ok {
		locationFilter = strings.ToLower(strings.TrimSpace(loc))
	}

	// Build summaries from cached routers
	summaries := make([]routerSummary, 0, len(result.Routers))
	for _, router := range result.Routers {
		// Apply location filter if provided
		if locationFilter != "" {
			routerLoc := strings.ToLower(router.Location)
			if !strings.Contains(routerLoc, locationFilter) {
				continue
			}
		}

		summaries = append(summaries, routerSummary{
			Hostname:      router.Hostname,
			Location:      router.Location,
			RouterType:    router.RouterType,
			IsHealthy:     !router.IsOverloaded,
			NeighborCount: len(router.Neighbors),
		})
	}

	// Sort alphabetically by hostname for consistent output
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Hostname < summaries[j].Hostname
	})

	jsonBytes, err := json.Marshal(summaries)
	if err != nil {
		return fmt.Sprintf("failed to marshal router list: %v", err), true, nil
	}

	return string(jsonBytes), false, nil
}

func (c *ISISToolClient) callGetRouter(_ context.Context, args map[string]any) (string, bool, error) {
	// Extract hostname parameter (required)
	hostname, ok := args["hostname"].(string)
	if !ok || strings.TrimSpace(hostname) == "" {
		return "hostname parameter is required", true, nil
	}
	hostname = strings.TrimSpace(hostname)

	c.mu.RLock()
	result := c.cachedResult
	c.mu.RUnlock()

	if result == nil {
		return "no cached ISIS data available. Run isis_refresh first.", true, nil
	}

	// Look up router by hostname
	router, exists := result.Routers[hostname]
	if !exists {
		return fmt.Sprintf("router not found: %s", hostname), true, nil
	}

	jsonBytes, err := json.Marshal(router)
	if err != nil {
		return fmt.Sprintf("failed to marshal router: %v", err), true, nil
	}

	return string(jsonBytes), false, nil
}

// adjacency represents a single adjacency relationship.
type adjacency struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
	Metric int    `json:"metric"`
}

func (c *ISISToolClient) callGetAdjacencies(_ context.Context, args map[string]any) (string, bool, error) {
	c.mu.RLock()
	result := c.cachedResult
	c.mu.RUnlock()

	if result == nil {
		return "no cached ISIS data available. Run isis_refresh first.", true, nil
	}

	// Extract optional router filter
	var routerFilter string
	if r, ok := args["router"].(string); ok {
		routerFilter = strings.TrimSpace(r)
	}

	// If router filter provided, verify the router exists
	if routerFilter != "" {
		if _, exists := result.Routers[routerFilter]; !exists {
			return fmt.Sprintf("router '%s' not found", routerFilter), true, nil
		}
	}

	// Build adjacency list
	adjacencies := make([]adjacency, 0)
	for hostname, router := range result.Routers {
		// Skip if filter is set and doesn't match
		if routerFilter != "" && hostname != routerFilter {
			continue
		}

		for _, neighbor := range router.Neighbors {
			adjacencies = append(adjacencies, adjacency{
				Source: hostname,
				Dest:   neighbor.Hostname,
				Metric: neighbor.Metric,
			})
		}
	}

	// Sort by source, then dest for consistent output
	sort.Slice(adjacencies, func(i, j int) bool {
		if adjacencies[i].Source != adjacencies[j].Source {
			return adjacencies[i].Source < adjacencies[j].Source
		}
		return adjacencies[i].Dest < adjacencies[j].Dest
	})

	jsonBytes, err := json.Marshal(adjacencies)
	if err != nil {
		return fmt.Sprintf("failed to marshal adjacencies: %v", err), true, nil
	}

	return string(jsonBytes), false, nil
}
