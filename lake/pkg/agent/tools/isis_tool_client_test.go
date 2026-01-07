package tools

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/isis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockISISEnricher implements ISISEnricher for testing.
type MockISISEnricher struct {
	EnrichFromReaderFunc func(ctx context.Context, r io.Reader, timestamp string) (*isis.Result, error)
	EnrichFromFileFunc   func(ctx context.Context, path string) (*isis.Result, error)
}

func (m *MockISISEnricher) EnrichFromReader(ctx context.Context, r io.Reader, timestamp string) (*isis.Result, error) {
	if m.EnrichFromReaderFunc != nil {
		return m.EnrichFromReaderFunc(ctx, r, timestamp)
	}
	return nil, nil
}

func (m *MockISISEnricher) EnrichFromFile(ctx context.Context, path string) (*isis.Result, error) {
	if m.EnrichFromFileFunc != nil {
		return m.EnrichFromFileFunc(ctx, path)
	}
	return nil, nil
}

// MockISISS3Fetcher implements ISISS3Fetcher for testing.
type MockISISS3Fetcher struct {
	FetchLatestFunc func(ctx context.Context) (*isis.FetchLatestResult, error)
}

func (m *MockISISS3Fetcher) FetchLatest(ctx context.Context) (*isis.FetchLatestResult, error) {
	if m.FetchLatestFunc != nil {
		return m.FetchLatestFunc(ctx)
	}
	return nil, nil
}

func mockResult() *isis.Result {
	return &isis.Result{
		Markdown: "# ISIS Network Topology",
		Routers: map[string]isis.Router{
			"DZ-NY5-SW01": {
				Hostname:     "DZ-NY5-SW01",
				RouterID:     "10.0.0.1",
				Location:     "NYC",
				RouterType:   "L2",
				IsOverloaded: false,
				Neighbors: []isis.Neighbor{
					{Hostname: "DZ-NY5-SW02", Metric: 100},
					{Hostname: "DZ-LON-SW01", Metric: 200},
				},
			},
			"DZ-NY5-SW02": {
				Hostname:     "DZ-NY5-SW02",
				RouterID:     "10.0.0.2",
				Location:     "NYC",
				RouterType:   "L2",
				IsOverloaded: false,
				Neighbors: []isis.Neighbor{
					{Hostname: "DZ-NY5-SW01", Metric: 100},
				},
			},
			"DZ-LON-SW01": {
				Hostname:     "DZ-LON-SW01",
				RouterID:     "10.0.0.3",
				Location:     "London",
				RouterType:   "L1L2",
				IsOverloaded: true,
				Neighbors: []isis.Neighbor{
					{Hostname: "DZ-NY5-SW01", Metric: 200},
				},
			},
		},
		Stats: isis.NetworkStats{
			TotalRouters:      3,
			TotalLinks:        4,
			HealthyRouters:    2,
			OverloadedRouters: 1,
			SREnabledRouters:  2,
			HealthyPercent:    66.67,
			SRPercent:         66.67,
			AvgNeighbors:      1.33,
		},
	}
}

func TestISISToolClient_ListTools(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 5)

	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}

	assert.Contains(t, toolNames, "isis_refresh")
	assert.Contains(t, toolNames, "isis_get_summary")
	assert.Contains(t, toolNames, "isis_list_routers")
	assert.Contains(t, toolNames, "isis_get_router")
	assert.Contains(t, toolNames, "isis_get_adjacencies")
}

func TestISISToolClient_Refresh_FromS3(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "2026-01-07T12-00-00Z_upload_data.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{"test": "data"}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)

	// Parse JSON response
	var response map[string]any
	err = json.Unmarshal([]byte(result), &response)
	require.NoError(t, err)

	assert.Equal(t, "2026-01-07 12:00:00 UTC", response["timestamp"])
	assert.Equal(t, float64(3), response["router_count"])
	assert.Equal(t, float64(4), response["link_count"])
	assert.Equal(t, 66.67, response["healthy_percent"])
}

func TestISISToolClient_Refresh_FromFile(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromFileFunc: func(_ context.Context, path string) (*isis.Result, error) {
			assert.Equal(t, "/path/to/data.json", path)
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{
		"source": "/path/to/data.json",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var response map[string]any
	err = json.Unmarshal([]byte(result), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(3), response["router_count"])
}

func TestISISToolClient_Refresh_WithLevel(t *testing.T) {
	enricherCalls := 0
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			enricherCalls++
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	_, isError, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{
		"level": float64(1),
	})

	require.NoError(t, err)
	assert.False(t, isError)
	assert.Equal(t, 1, enricherCalls)
}

func TestISISToolClient_GetSummary_WithCache(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Then get summary
	result, isError, err := client.CallToolText(context.Background(), "isis_get_summary", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)

	var stats isis.NetworkStats
	err = json.Unmarshal([]byte(result), &stats)
	require.NoError(t, err)

	assert.Equal(t, 3, stats.TotalRouters)
	assert.Equal(t, 4, stats.TotalLinks)
	assert.Equal(t, 2, stats.HealthyRouters)
	assert.Equal(t, 66.67, stats.HealthyPercent)
}

func TestISISToolClient_GetSummary_NoCache(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_get_summary", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "no cached ISIS data")
}

func TestISISToolClient_Refresh_S3Error(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return nil, assert.AnError
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "failed to fetch")
}

func TestISISToolClient_Refresh_EnrichError(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return nil, assert.AnError
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "failed to enrich")
}

func TestISISToolClient_UnknownTool(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	_, _, err := client.CallToolText(context.Background(), "unknown_tool", map[string]any{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestISISToolClient_ToolDescriptions(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)

	for _, tool := range tools {
		assert.NotEmpty(t, tool.Description, "tool %s should have description", tool.Name)
		assert.NotNil(t, tool.InputSchema, "tool %s should have input schema", tool.Name)
	}
}

func TestISISToolClient_RefreshCachesResult(t *testing.T) {
	fetchCalls := 0
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			fetchCalls++
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First refresh
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 1, fetchCalls)

	// Get summary should use cache, not fetch again
	_, isError, err := client.CallToolText(context.Background(), "isis_get_summary", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError)
	assert.Equal(t, 1, fetchCalls) // Still 1, not 2

	// Second refresh should fetch again
	_, _, err = client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 2, fetchCalls)
}

func TestISISToolClient_GetAdjacencies_AllAdjacencies(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get all adjacencies
	result, isError, err := client.CallToolText(context.Background(), "isis_get_adjacencies", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)

	var adjacencies []map[string]any
	err = json.Unmarshal([]byte(result), &adjacencies)
	require.NoError(t, err)

	// We have 4 adjacencies total from mockResult
	assert.Len(t, adjacencies, 4)

	// Check structure of first adjacency
	assert.Contains(t, adjacencies[0], "source")
	assert.Contains(t, adjacencies[0], "dest")
	assert.Contains(t, adjacencies[0], "metric")

	// Verify sorting: should be sorted by source, then dest
	// DZ-LON-SW01 -> DZ-NY5-SW01 (alphabetically first source)
	assert.Equal(t, "DZ-LON-SW01", adjacencies[0]["source"])
	assert.Equal(t, "DZ-NY5-SW01", adjacencies[0]["dest"])
	assert.Equal(t, float64(200), adjacencies[0]["metric"])

	// DZ-NY5-SW01 -> DZ-LON-SW01
	assert.Equal(t, "DZ-NY5-SW01", adjacencies[1]["source"])
	assert.Equal(t, "DZ-LON-SW01", adjacencies[1]["dest"])
	assert.Equal(t, float64(200), adjacencies[1]["metric"])

	// DZ-NY5-SW01 -> DZ-NY5-SW02
	assert.Equal(t, "DZ-NY5-SW01", adjacencies[2]["source"])
	assert.Equal(t, "DZ-NY5-SW02", adjacencies[2]["dest"])
	assert.Equal(t, float64(100), adjacencies[2]["metric"])

	// DZ-NY5-SW02 -> DZ-NY5-SW01
	assert.Equal(t, "DZ-NY5-SW02", adjacencies[3]["source"])
	assert.Equal(t, "DZ-NY5-SW01", adjacencies[3]["dest"])
}

func TestISISToolClient_GetAdjacencies_FilterByRouter(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get adjacencies for DZ-NY5-SW01 only
	result, isError, err := client.CallToolText(context.Background(), "isis_get_adjacencies", map[string]any{
		"router": "DZ-NY5-SW01",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var adjacencies []map[string]any
	err = json.Unmarshal([]byte(result), &adjacencies)
	require.NoError(t, err)

	// DZ-NY5-SW01 has 2 neighbors
	assert.Len(t, adjacencies, 2)

	// All should have DZ-NY5-SW01 as source
	for _, adj := range adjacencies {
		assert.Equal(t, "DZ-NY5-SW01", adj["source"])
	}
}

func TestISISToolClient_GetAdjacencies_RouterNotFound(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Try to get adjacencies for non-existent router
	result, isError, err := client.CallToolText(context.Background(), "isis_get_adjacencies", map[string]any{
		"router": "nonexistent",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "router 'nonexistent' not found")
}

func TestISISToolClient_GetAdjacencies_NoCache(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_get_adjacencies", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "no cached ISIS data")
}

func TestISISToolClient_ListRouters_AllRouters(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get all routers
	result, isError, err := client.CallToolText(context.Background(), "isis_list_routers", map[string]any{})

	require.NoError(t, err)
	assert.False(t, isError)

	var routers []map[string]any
	err = json.Unmarshal([]byte(result), &routers)
	require.NoError(t, err)

	// We have 3 routers
	assert.Len(t, routers, 3)

	// Check structure of first router (sorted alphabetically by hostname)
	// DZ-LON-SW01 comes first alphabetically
	assert.Equal(t, "DZ-LON-SW01", routers[0]["hostname"])
	assert.Equal(t, "London", routers[0]["location"])
	assert.Equal(t, "L1L2", routers[0]["router_type"])
	assert.Equal(t, false, routers[0]["is_healthy"]) // IsOverloaded = true
	assert.Equal(t, float64(1), routers[0]["neighbor_count"])

	// DZ-NY5-SW01 comes second
	assert.Equal(t, "DZ-NY5-SW01", routers[1]["hostname"])
	assert.Equal(t, "NYC", routers[1]["location"])
	assert.Equal(t, "L2", routers[1]["router_type"])
	assert.Equal(t, true, routers[1]["is_healthy"])
	assert.Equal(t, float64(2), routers[1]["neighbor_count"])

	// DZ-NY5-SW02 comes third
	assert.Equal(t, "DZ-NY5-SW02", routers[2]["hostname"])
	assert.Equal(t, "NYC", routers[2]["location"])
}

func TestISISToolClient_ListRouters_FilterByLocation(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get routers in NYC only
	result, isError, err := client.CallToolText(context.Background(), "isis_list_routers", map[string]any{
		"location": "NYC",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var routers []map[string]any
	err = json.Unmarshal([]byte(result), &routers)
	require.NoError(t, err)

	// Should have 2 NYC routers
	assert.Len(t, routers, 2)

	// All should be in NYC
	for _, router := range routers {
		assert.Equal(t, "NYC", router["location"])
	}
}

func TestISISToolClient_ListRouters_FilterByLocation_CaseInsensitive(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get routers in london (lowercase)
	result, isError, err := client.CallToolText(context.Background(), "isis_list_routers", map[string]any{
		"location": "london",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var routers []map[string]any
	err = json.Unmarshal([]byte(result), &routers)
	require.NoError(t, err)

	// Should have 1 London router
	assert.Len(t, routers, 1)
	assert.Equal(t, "DZ-LON-SW01", routers[0]["hostname"])
}

func TestISISToolClient_ListRouters_FilterNoMatch(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get routers in non-existent location
	result, isError, err := client.CallToolText(context.Background(), "isis_list_routers", map[string]any{
		"location": "Tokyo",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var routers []map[string]any
	err = json.Unmarshal([]byte(result), &routers)
	require.NoError(t, err)

	// Should return empty array
	assert.Len(t, routers, 0)
}

func TestISISToolClient_ListRouters_NoCache(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	result, isError, err := client.CallToolText(context.Background(), "isis_list_routers", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "no cached ISIS data")
}

func TestISISToolClient_GetRouter_Success(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Get router details
	result, isError, err := client.CallToolText(context.Background(), "isis_get_router", map[string]any{
		"hostname": "DZ-NY5-SW01",
	})

	require.NoError(t, err)
	assert.False(t, isError)

	var router isis.Router
	err = json.Unmarshal([]byte(result), &router)
	require.NoError(t, err)

	assert.Equal(t, "DZ-NY5-SW01", router.Hostname)
	assert.Equal(t, "10.0.0.1", router.RouterID)
	assert.Equal(t, "NYC", router.Location)
	assert.Equal(t, "L2", router.RouterType)
	assert.False(t, router.IsOverloaded)
	assert.Len(t, router.Neighbors, 2)
}

func TestISISToolClient_GetRouter_NotFound(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Try to get non-existent router
	result, isError, err := client.CallToolText(context.Background(), "isis_get_router", map[string]any{
		"hostname": "nonexistent-router",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "router not found: nonexistent-router")
}

func TestISISToolClient_GetRouter_MissingHostname(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Try to get router without hostname parameter
	result, isError, err := client.CallToolText(context.Background(), "isis_get_router", map[string]any{})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "hostname parameter is required")
}

func TestISISToolClient_GetRouter_EmptyHostname(t *testing.T) {
	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			return &isis.FetchLatestResult{
				Key:       "test.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		},
	}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// First, refresh to populate cache
	_, _, err := client.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Try to get router with empty hostname
	result, isError, err := client.CallToolText(context.Background(), "isis_get_router", map[string]any{
		"hostname": "",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "hostname parameter is required")
}

func TestISISToolClient_GetRouter_NoCache(t *testing.T) {
	mockEnricher := &MockISISEnricher{}
	mockFetcher := &MockISISS3Fetcher{}

	client := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Try to get router without cache
	result, isError, err := client.CallToolText(context.Background(), "isis_get_router", map[string]any{
		"hostname": "DZ-NY5-SW01",
	})

	require.NoError(t, err)
	assert.True(t, isError)
	assert.Contains(t, result, "no cached ISIS data")
}
