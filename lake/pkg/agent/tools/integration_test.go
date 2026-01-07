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

// TestMultiToolClient_ISISAndMemvid_Integration verifies that MultiToolClient
// correctly combines ISISToolClient and MemvidToolClient, exposing all 9 tools
// and routing calls to the correct underlying client.
func TestMultiToolClient_ISISAndMemvid_Integration(t *testing.T) {
	// Create ISISToolClient with mock dependencies
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
	isisClient := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Create MemvidToolClient with mock CommandRunner
	mockRunner := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"status": "ok"}`, "", nil
		},
	}
	memvidClient := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mockRunner,
	})

	// Combine via MultiToolClient
	multiClient, err := NewMultiToolClient(isisClient, memvidClient)
	require.NoError(t, err)
	require.NotNil(t, multiClient)

	// Verify ListTools() returns all 9 tools (5 ISIS + 4 Memvid)
	tools, err := multiClient.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 9, "expected 9 tools total (5 ISIS + 4 Memvid)")

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	// Verify ISIS tools are present
	assert.True(t, toolNames["isis_refresh"], "missing isis_refresh")
	assert.True(t, toolNames["isis_get_summary"], "missing isis_get_summary")
	assert.True(t, toolNames["isis_list_routers"], "missing isis_list_routers")
	assert.True(t, toolNames["isis_get_router"], "missing isis_get_router")
	assert.True(t, toolNames["isis_get_adjacencies"], "missing isis_get_adjacencies")

	// Verify Memvid tools are present
	assert.True(t, toolNames["memory_save"], "missing memory_save")
	assert.True(t, toolNames["memory_search"], "missing memory_search")
	assert.True(t, toolNames["memory_ask"], "missing memory_ask")
	assert.True(t, toolNames["memory_stats"], "missing memory_stats")

	// Verify routing works for ISIS tools
	result, isError, err := multiClient.CallToolText(context.Background(), "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "timestamp")

	// Verify routing works for Memvid tools
	result, isError, err = multiClient.CallToolText(context.Background(), "memory_stats", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError)
	assert.Contains(t, result, "status")

	// Verify unknown tool returns error
	_, _, err = multiClient.CallToolText(context.Background(), "unknown_tool", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

// TestAgentWorkflow_ISISRefreshThenQuery simulates a typical agent workflow
// where the agent refreshes ISIS data and then queries it through multiple tools.
func TestAgentWorkflow_ISISRefreshThenQuery(t *testing.T) {
	// Track call counts for verification
	enrichCalls := 0
	fetchCalls := 0

	mockEnricher := &MockISISEnricher{
		EnrichFromReaderFunc: func(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
			enrichCalls++
			return mockResult(), nil
		},
	}
	mockFetcher := &MockISISS3Fetcher{
		FetchLatestFunc: func(_ context.Context) (*isis.FetchLatestResult, error) {
			fetchCalls++
			return &isis.FetchLatestResult{
				Key:       "2026-01-07T12-00-00Z_upload_data.json",
				Timestamp: "2026-01-07 12:00:00 UTC",
				Body:      io.NopCloser(strings.NewReader(`{"test": "data"}`)),
			}, nil
		},
	}

	isisClient := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	ctx := context.Background()

	// Step 1: Call isis_refresh to fetch and cache data
	result, isError, err := isisClient.CallToolText(ctx, "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_refresh should succeed")
	assert.Equal(t, 1, fetchCalls, "should have fetched from S3 once")
	assert.Equal(t, 1, enrichCalls, "should have enriched once")

	var refreshResp map[string]any
	err = json.Unmarshal([]byte(result), &refreshResp)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-07 12:00:00 UTC", refreshResp["timestamp"])
	assert.Equal(t, float64(3), refreshResp["router_count"])

	// Step 2: Call isis_get_summary (uses cached data)
	result, isError, err = isisClient.CallToolText(ctx, "isis_get_summary", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_get_summary should succeed")
	assert.Equal(t, 1, fetchCalls, "should NOT fetch from S3 again")

	var stats isis.NetworkStats
	err = json.Unmarshal([]byte(result), &stats)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.TotalRouters)
	assert.Equal(t, 4, stats.TotalLinks)
	assert.Equal(t, 2, stats.HealthyRouters)

	// Step 3: Call isis_list_routers (uses cached data)
	result, isError, err = isisClient.CallToolText(ctx, "isis_list_routers", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_list_routers should succeed")

	var routers []map[string]any
	err = json.Unmarshal([]byte(result), &routers)
	require.NoError(t, err)
	assert.Len(t, routers, 3)

	// Routers should be sorted alphabetically
	assert.Equal(t, "DZ-LON-SW01", routers[0]["hostname"])
	assert.Equal(t, "DZ-NY5-SW01", routers[1]["hostname"])
	assert.Equal(t, "DZ-NY5-SW02", routers[2]["hostname"])

	// Step 4: Call isis_get_router for specific router (uses cached data)
	result, isError, err = isisClient.CallToolText(ctx, "isis_get_router", map[string]any{
		"hostname": "DZ-NY5-SW01",
	})
	require.NoError(t, err)
	assert.False(t, isError, "isis_get_router should succeed")

	var router isis.Router
	err = json.Unmarshal([]byte(result), &router)
	require.NoError(t, err)
	assert.Equal(t, "DZ-NY5-SW01", router.Hostname)
	assert.Equal(t, "10.0.0.1", router.RouterID)
	assert.Equal(t, "NYC", router.Location)
	assert.Len(t, router.Neighbors, 2)

	// Verify no additional S3 fetches occurred
	assert.Equal(t, 1, fetchCalls, "data should come from cache after initial refresh")
}

// TestAgentWorkflow_ISISToMemvid simulates a cross-tool workflow where the agent
// retrieves ISIS topology data and stores it in memory via Memvid.
func TestAgentWorkflow_ISISToMemvid(t *testing.T) {
	// Setup ISIS mocks
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
	isisClient := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Setup Memvid mock - RunFunc returns success, content captured via Calls
	mockRunner := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"id": "frame-isis-router", "status": "ok"}`, "", nil
		},
	}
	memvidClient := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mockRunner,
	})

	// Create combined client
	multiClient, err := NewMultiToolClient(isisClient, memvidClient)
	require.NoError(t, err)

	ctx := context.Background()

	// Step 1: Call isis_refresh (mock S3)
	result, isError, err := multiClient.CallToolText(ctx, "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_refresh should succeed")
	assert.Contains(t, result, "timestamp")

	// Step 2: Call isis_get_router to get router details
	result, isError, err = multiClient.CallToolText(ctx, "isis_get_router", map[string]any{
		"hostname": "DZ-NY5-SW01",
	})
	require.NoError(t, err)
	assert.False(t, isError, "isis_get_router should succeed")

	// Parse router data that would be stored
	var router isis.Router
	err = json.Unmarshal([]byte(result), &router)
	require.NoError(t, err)
	assert.Equal(t, "DZ-NY5-SW01", router.Hostname)

	// Step 3: Call memory_save with router data (mock memvid CLI)
	routerJSON := result
	result, isError, err = multiClient.CallToolText(ctx, "memory_save", map[string]any{
		"content": routerJSON,
		"title":   "ISIS Router: DZ-NY5-SW01",
		"tags":    []any{"type=isis", "location=NYC"},
	})
	require.NoError(t, err)
	assert.False(t, isError, "memory_save should succeed")
	assert.Contains(t, result, "frame-isis-router")

	// Verify memory_save was called with expected content
	require.Len(t, mockRunner.Calls, 1, "memory_save should have been called once")
	call := mockRunner.Calls[0]
	assert.Equal(t, "/usr/bin/memvid", call.Name)
	assert.Contains(t, call.Args, "put")
	assert.Contains(t, call.Args, "--title")
	assert.Contains(t, call.Args, "ISIS Router: DZ-NY5-SW01")
	assert.Contains(t, call.Args, "--tag")
	assert.Contains(t, call.Args, "type=isis")
	assert.Contains(t, call.Args, "location=NYC")

	// Verify the content passed to memvid contains the router data
	// MockCommandRunner captures stdin content in call.Stdin
	assert.Contains(t, call.Stdin, "DZ-NY5-SW01")
	assert.Contains(t, call.Stdin, "10.0.0.1") // RouterID

	// Verify title was passed correctly in args
	titleIndex := -1
	for i, arg := range call.Args {
		if arg == "--title" && i+1 < len(call.Args) {
			titleIndex = i + 1
			break
		}
	}
	require.NotEqual(t, -1, titleIndex, "should have --title in args")
	assert.Equal(t, "ISIS Router: DZ-NY5-SW01", call.Args[titleIndex])
}

// TestAgentWorkflow_ISISFilterAndSaveToMemvid tests filtering ISIS data before
// storing a subset in memory.
func TestAgentWorkflow_ISISFilterAndSaveToMemvid(t *testing.T) {
	// Setup ISIS mocks
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
	isisClient := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Setup memvid mock - RunFunc returns success, content captured via Calls
	mockRunner := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, _ []string, _ io.Reader) (string, string, error) {
			return `{"status": "ok"}`, "", nil
		},
	}
	memvidClient := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mockRunner,
	})

	multiClient, err := NewMultiToolClient(isisClient, memvidClient)
	require.NoError(t, err)

	ctx := context.Background()

	// Step 1: Refresh ISIS data
	_, _, err = multiClient.CallToolText(ctx, "isis_refresh", map[string]any{})
	require.NoError(t, err)

	// Step 2: List routers filtered by location (NYC)
	result, isError, err := multiClient.CallToolText(ctx, "isis_list_routers", map[string]any{
		"location": "NYC",
	})
	require.NoError(t, err)
	assert.False(t, isError)

	var nycRouters []map[string]any
	err = json.Unmarshal([]byte(result), &nycRouters)
	require.NoError(t, err)
	assert.Len(t, nycRouters, 2, "should have 2 NYC routers")

	// Step 3: Get adjacencies for one of the NYC routers
	result, isError, err = multiClient.CallToolText(ctx, "isis_get_adjacencies", map[string]any{
		"router": "DZ-NY5-SW01",
	})
	require.NoError(t, err)
	assert.False(t, isError)

	var adjacencies []map[string]any
	err = json.Unmarshal([]byte(result), &adjacencies)
	require.NoError(t, err)
	assert.Len(t, adjacencies, 2, "DZ-NY5-SW01 has 2 neighbors")

	// Step 4: Save the adjacency data to memory
	_, isError, err = multiClient.CallToolText(ctx, "memory_save", map[string]any{
		"content": result,
		"title":   "DZ-NY5-SW01 Adjacencies",
		"tags":    []any{"type=adjacency", "router=DZ-NY5-SW01"},
	})
	require.NoError(t, err)
	assert.False(t, isError)

	// Verify content was saved - MockCommandRunner captures stdin in Calls
	require.Len(t, mockRunner.Calls, 1, "memory_save should have been called once")
	savedContent := mockRunner.Calls[0].Stdin
	assert.Contains(t, savedContent, "DZ-NY5-SW01")
	assert.Contains(t, savedContent, "DZ-NY5-SW02")
	assert.Contains(t, savedContent, "DZ-LON-SW01")
}

// TestMultiToolClient_ToolIsolation verifies that errors in one tool don't affect
// the other tools, and that cache state is properly maintained.
func TestMultiToolClient_ToolIsolation(t *testing.T) {
	// Setup ISIS with working mocks
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
	isisClient := NewISISToolClientWithDeps(ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Setup Memvid with failing mock for memory_search, working for others
	mockRunner := &MockCommandRunner{
		RunFunc: func(_ context.Context, _ string, args []string, _ io.Reader) (string, string, error) {
			// Simulate failure for memory_search
			if len(args) > 0 && args[0] == "find" {
				return "", "search index corrupted", assert.AnError
			}
			return `{"status": "ok"}`, "", nil
		},
	}
	memvidClient := NewMemvidToolClient(MemvidConfig{
		BinaryPath:    "/usr/bin/memvid",
		BrainPath:     "/path/to/brain.mv2",
		CommandRunner: mockRunner,
	})

	multiClient, err := NewMultiToolClient(isisClient, memvidClient)
	require.NoError(t, err)

	ctx := context.Background()

	// ISIS tools should work
	_, isError, err := multiClient.CallToolText(ctx, "isis_refresh", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_refresh should succeed")

	// memory_stats should work (not the failing one)
	_, isError, err = multiClient.CallToolText(ctx, "memory_stats", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "memory_stats should succeed")

	// memory_search should fail
	result, isError, err := multiClient.CallToolText(ctx, "memory_search", map[string]any{
		"query": "test",
	})
	require.NoError(t, err) // The error is in isError, not err
	assert.True(t, isError, "memory_search should return isError=true")
	assert.Contains(t, result, "search index corrupted")

	// ISIS tools should still work after memvid failure
	result, isError, err = multiClient.CallToolText(ctx, "isis_get_summary", map[string]any{})
	require.NoError(t, err)
	assert.False(t, isError, "isis_get_summary should still work after memvid failure")
	assert.Contains(t, result, "TotalRouters")
}
