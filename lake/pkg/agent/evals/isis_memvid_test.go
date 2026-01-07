//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/malbeclabs/doublezero/lake/pkg/agent"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/prompts"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/agent/tools"
	"github.com/malbeclabs/doublezero/lake/pkg/isis"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ISIS-Memvid Evals: Test agent's ability to query network topology
// =============================================================================

func TestLake_Agent_Evals_Anthropic_ISISNetworkSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISNetworkSummary(t)
}

func TestLake_Agent_Evals_Anthropic_ISISRouterQuery(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISRouterQuery(t)
}

func TestLake_Agent_Evals_Anthropic_ISISLocationFilter(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISLocationFilter(t)
}

func TestLake_Agent_Evals_Anthropic_ISISAdjacencies(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISAdjacencies(t)
}

// =============================================================================
// Full Workflow Evals: ISIS → Memvid storage → retrieval
// =============================================================================

func TestLake_Agent_Evals_Anthropic_ISISMemvidFullWorkflow(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISMemvidFullWorkflow(t)
}

func TestLake_Agent_Evals_Anthropic_ISISMemvidStoreAndRetrieve(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}
	runTest_ISISMemvidStoreAndRetrieve(t)
}

// =============================================================================
// Test Implementations
// =============================================================================

func runTest_ISISNetworkSummary(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Set up agent with ISIS tool client (mocked)
	agentInstance := setupISISAgent(t, ctx, debug, debugLevel)

	var output bytes.Buffer
	question := "What is the current state of the ISIS network? How many routers are there and what is the health status?"

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	validateISISNetworkSummaryResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

func runTest_ISISRouterQuery(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	agentInstance := setupISISAgent(t, ctx, debug, debugLevel)

	var output bytes.Buffer
	question := "Tell me about the router DZ-NY5-SW01. What are its neighbors and what is its segment routing configuration?"

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	validateISISRouterQueryResponse(t, response)

	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

func runTest_ISISLocationFilter(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	agentInstance := setupISISAgent(t, ctx, debug, debugLevel)

	var output bytes.Buffer
	question := "Which routers are located in NYC? List them with their basic information."

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	validateISISLocationFilterResponse(t, response)

	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

func runTest_ISISAdjacencies(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	agentInstance := setupISISAgent(t, ctx, debug, debugLevel)

	var output bytes.Buffer
	question := "Show me the network adjacencies. Which routers are connected to each other and what are the link metrics?"

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	validateISISAdjacenciesResponse(t, response)

	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

// runTest_ISISMemvidFullWorkflow tests the complete flow:
// 1. Agent fetches ISIS data
// 2. Agent saves router info to memvid
// 3. Validates that memory_save was called with correct data
func runTest_ISISMemvidFullWorkflow(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Create mock memvid that captures calls
	memvidMock := newMockMemvidRunner()

	// Set up agent with both ISIS and Memvid tools
	agentInstance := setupISISMemvidAgent(t, ctx, debug, debugLevel, memvidMock)

	var output bytes.Buffer
	question := "Get the ISIS network data. Then save the router DZ-NY5-SW01 to memory with the tag 'isis-snapshot' so we can reference it later."

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	// Validate the workflow
	validateISISMemvidWorkflowResponse(t, response, memvidMock)

	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

// runTest_ISISMemvidStoreAndRetrieve tests:
// 1. Store ISIS data in memvid
// 2. Later retrieve it via memory_search
func runTest_ISISMemvidStoreAndRetrieve(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Create mock memvid with pre-stored data that can be "found"
	memvidMock := newMockMemvidRunnerWithStoredData()

	agentInstance := setupISISMemvidAgent(t, ctx, debug, debugLevel, memvidMock)

	var output bytes.Buffer
	question := "Search memory for any stored ISIS router data. What routers do we have saved?"

	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}

	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	response := result.FinalText

	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	// Validate retrieval
	validateISISMemvidRetrievalResponse(t, response, memvidMock)

	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err)
	require.True(t, isCorrect, "Ollama evaluation indicates incorrect response. Reason: %s", reason)
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateISISNetworkSummaryResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention routers
	hasRouters := strings.Contains(responseLower, "router")
	require.True(t, hasRouters, "Response should mention routers: %s", truncateForError(response, 300))

	// Should have some numbers (router counts, percentages)
	hasNumbers := strings.ContainsAny(response, "0123456789")
	require.True(t, hasNumbers, "Response should include numeric data: %s", truncateForError(response, 300))

	// Should mention health or status
	hasHealth := strings.Contains(responseLower, "health") ||
		strings.Contains(responseLower, "healthy") ||
		strings.Contains(responseLower, "status")
	require.True(t, hasHealth, "Response should mention health status: %s", truncateForError(response, 300))
}

func validateISISRouterQueryResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention the specific router
	hasRouter := strings.Contains(responseLower, "dz-ny5-sw01") ||
		strings.Contains(responseLower, "ny5")
	require.True(t, hasRouter, "Response should mention DZ-NY5-SW01: %s", truncateForError(response, 300))

	// Should mention neighbors
	hasNeighbors := strings.Contains(responseLower, "neighbor")
	require.True(t, hasNeighbors, "Response should mention neighbors: %s", truncateForError(response, 300))

	// Should mention segment routing or SR
	hasSR := strings.Contains(responseLower, "segment") ||
		strings.Contains(responseLower, "sr") ||
		strings.Contains(responseLower, "sid") ||
		strings.Contains(responseLower, "srgb")
	require.True(t, hasSR, "Response should mention segment routing: %s", truncateForError(response, 300))
}

func validateISISLocationFilterResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention NYC
	hasNYC := strings.Contains(responseLower, "nyc") ||
		strings.Contains(responseLower, "new york")
	require.True(t, hasNYC, "Response should mention NYC: %s", truncateForError(response, 300))

	// Should list routers
	hasRouters := strings.Contains(responseLower, "router") ||
		strings.Contains(responseLower, "dz-")
	require.True(t, hasRouters, "Response should list routers: %s", truncateForError(response, 300))
}

func validateISISAdjacenciesResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention connections or adjacencies
	hasConnections := strings.Contains(responseLower, "connect") ||
		strings.Contains(responseLower, "adjacenc") ||
		strings.Contains(responseLower, "neighbor") ||
		strings.Contains(responseLower, "link")
	require.True(t, hasConnections, "Response should mention connections: %s", truncateForError(response, 300))

	// Should mention metrics
	hasMetrics := strings.Contains(responseLower, "metric") ||
		strings.Contains(responseLower, "cost")
	require.True(t, hasMetrics, "Response should mention metrics: %s", truncateForError(response, 300))
}

func validateISISMemvidWorkflowResponse(t *testing.T, response string, mock *mockMemvidRunner) {
	responseLower := strings.ToLower(response)

	// Should mention saving or storing
	hasSaved := strings.Contains(responseLower, "saved") ||
		strings.Contains(responseLower, "stored") ||
		strings.Contains(responseLower, "memory")
	require.True(t, hasSaved, "Response should mention saving to memory: %s", truncateForError(response, 300))

	// Should mention the router
	hasRouter := strings.Contains(responseLower, "dz-ny5-sw01") ||
		strings.Contains(responseLower, "ny5")
	require.True(t, hasRouter, "Response should mention the router: %s", truncateForError(response, 300))

	// Verify memory_save was actually called
	calls := mock.GetCalls()
	hasSaveCall := false
	for _, call := range calls {
		if strings.Contains(call.Command, "put") {
			hasSaveCall = true
			// Verify the content includes router data
			require.True(t, strings.Contains(call.Stdin, "DZ-NY5-SW01") ||
				strings.Contains(call.Stdin, "172.16.0.1"),
				"memory_save should include router data")
			break
		}
	}
	require.True(t, hasSaveCall, "memory_save (put) should have been called")
}

func validateISISMemvidRetrievalResponse(t *testing.T, response string, mock *mockMemvidRunner) {
	responseLower := strings.ToLower(response)

	// Should mention finding or retrieving data
	hasFound := strings.Contains(responseLower, "found") ||
		strings.Contains(responseLower, "stored") ||
		strings.Contains(responseLower, "saved") ||
		strings.Contains(responseLower, "router")
	require.True(t, hasFound, "Response should mention found data: %s", truncateForError(response, 300))

	// Verify memory_search was called
	calls := mock.GetCalls()
	hasSearchCall := false
	for _, call := range calls {
		if strings.Contains(call.Command, "find") || strings.Contains(call.Command, "search") {
			hasSearchCall = true
			break
		}
	}
	require.True(t, hasSearchCall, "memory_search (find) should have been called")
}

// =============================================================================
// Agent Setup with ISIS Tools
// =============================================================================

func setupISISAgent(t *testing.T, ctx context.Context, debug bool, debugLevel int) *agent.Agent {
	// Create logger
	var logger *slog.Logger
	if debug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = testLogger(t)
	}

	// Load prompts
	p, err := prompts.Load()
	require.NoError(t, err)

	// Build system prompt with ISIS context
	systemPrompt := p.BuildSystemPrompt() + `

You have access to ISIS network topology tools:
- isis_refresh: Fetch latest ISIS topology from S3 and cache locally
- isis_get_summary: Get network summary statistics
- isis_list_routers: List all routers with basic info
- isis_get_router: Get full details for a specific router
- isis_get_adjacencies: Get adjacency list (who connects to whom)

When asked about the network, first call isis_refresh to get fresh data, then use the other tools to answer questions.
`

	// Create LLM client
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	anthropicClient := anthropic.NewClient(option.WithAPIKey(apiKey))
	baseLLMClient := react.NewAnthropicAgent(
		anthropicClient,
		anthropic.ModelClaudeHaiku4_5_20251001,
		4000,
		systemPrompt,
	)

	var llmClient react.LLMClient = baseLLMClient
	if debug {
		llmClient = &debugLLMClient{
			LLMClient:  baseLLMClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create ISIS tool client with mock data
	mockEnricher := &mockISISEnricher{}
	mockFetcher := &mockISISS3Fetcher{}
	isisClient := tools.NewISISToolClientWithDeps(tools.ISISToolClientConfig{}, mockEnricher, mockFetcher)

	var toolClient react.ToolClient = isisClient
	if debug {
		toolClient = &debugToolClient{
			ToolClient: isisClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create react agent
	cfg := &react.Config{
		Logger:             logger,
		LLM:                llmClient,
		ToolClient:         toolClient,
		FinalizationPrompt: p.Finalization,
	}
	reactAgent, err := react.NewAgent(cfg)
	require.NoError(t, err)

	return agent.NewAgent(&agent.AgentConfig{
		ReactAgent: reactAgent,
	})
}

// =============================================================================
// Mock ISIS Data
// =============================================================================

type mockISISEnricher struct{}

func (m *mockISISEnricher) EnrichFromReader(_ context.Context, _ io.Reader, _ string) (*isis.Result, error) {
	return mockISISResult(), nil
}

func (m *mockISISEnricher) EnrichFromFile(_ context.Context, _ string) (*isis.Result, error) {
	return mockISISResult(), nil
}

type mockISISS3Fetcher struct{}

func (m *mockISISS3Fetcher) FetchLatest(_ context.Context) (*isis.FetchLatestResult, error) {
	return &isis.FetchLatestResult{
		Key:       "2026-01-07T12-00-00Z_upload_data.json",
		Timestamp: "2026-01-07 12:00:00 UTC",
		Body:      io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// =============================================================================
// Agent Setup with ISIS + Memvid Tools (Full Workflow)
// =============================================================================

func setupISISMemvidAgent(t *testing.T, ctx context.Context, debug bool, debugLevel int, memvidRunner *mockMemvidRunner) *agent.Agent {
	// Create logger
	var logger *slog.Logger
	if debug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = testLogger(t)
	}

	// Load prompts
	p, err := prompts.Load()
	require.NoError(t, err)

	// Build system prompt with both ISIS and Memvid context
	systemPrompt := p.BuildSystemPrompt() + `

You have access to ISIS network topology tools:
- isis_refresh: Fetch latest ISIS topology from S3 and cache locally
- isis_get_summary: Get network summary statistics
- isis_list_routers: List all routers with basic info
- isis_get_router: Get full details for a specific router
- isis_get_adjacencies: Get adjacency list (who connects to whom)

You also have access to memory tools for persistent storage:
- memory_save: Store content in persistent memory with title and tags
- memory_search: Search memory for previously stored content
- memory_ask: Ask questions about stored memory content
- memory_stats: Get memory statistics

Workflow for saving ISIS data:
1. First call isis_refresh to get fresh network data
2. Use isis_get_router to get specific router details
3. Use memory_save to store the router data with appropriate tags like "isis", "router", and the router hostname

When asked about the network, first call isis_refresh, then use the appropriate tools.
When asked to save data, use memory_save with descriptive title and tags.
When asked to find stored data, use memory_search.
`

	// Create LLM client
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	anthropicClient := anthropic.NewClient(option.WithAPIKey(apiKey))
	baseLLMClient := react.NewAnthropicAgent(
		anthropicClient,
		anthropic.ModelClaudeHaiku4_5_20251001,
		4000,
		systemPrompt,
	)

	var llmClient react.LLMClient = baseLLMClient
	if debug {
		llmClient = &debugLLMClient{
			LLMClient:  baseLLMClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create ISIS tool client with mock data
	mockEnricher := &mockISISEnricher{}
	mockFetcher := &mockISISS3Fetcher{}
	isisClient := tools.NewISISToolClientWithDeps(tools.ISISToolClientConfig{}, mockEnricher, mockFetcher)

	// Create Memvid tool client with mock runner
	memvidClient := tools.NewMemvidToolClient(tools.MemvidConfig{
		BinaryPath:    "memvid",
		BrainPath:     "/tmp/test-brain.mv2",
		CommandRunner: memvidRunner,
	})

	// Combine via MultiToolClient
	multiClient, err := tools.NewMultiToolClient(isisClient, memvidClient)
	require.NoError(t, err)

	var toolClient react.ToolClient = multiClient
	if debug {
		toolClient = &debugToolClient{
			ToolClient: multiClient,
			t:          t,
			debugLevel: debugLevel,
		}
	}

	// Create react agent
	cfg := &react.Config{
		Logger:             logger,
		LLM:                llmClient,
		ToolClient:         toolClient,
		FinalizationPrompt: p.Finalization,
	}
	reactAgent, err := react.NewAgent(cfg)
	require.NoError(t, err)

	return agent.NewAgent(&agent.AgentConfig{
		ReactAgent: reactAgent,
	})
}

// =============================================================================
// Mock Memvid Runner
// =============================================================================

// mockMemvidCall represents a captured memvid CLI call
type mockMemvidCall struct {
	Command string
	Args    []string
	Stdin   string
}

// mockMemvidRunner captures memvid CLI calls for verification
type mockMemvidRunner struct {
	mu           sync.Mutex
	calls        []mockMemvidCall
	searchResult string // Pre-configured search result
}

func newMockMemvidRunner() *mockMemvidRunner {
	return &mockMemvidRunner{
		calls: make([]mockMemvidCall, 0),
	}
}

func newMockMemvidRunnerWithStoredData() *mockMemvidRunner {
	// Return mock search results that look like stored ISIS data
	searchResult := `[
		{
			"title": "Router DZ-NY5-SW01 @ 2026-01-07",
			"content": "Router DZ-NY5-SW01 in NYC with neighbors DZ-NY7-SW01 and DZ-CHI-SW01",
			"tags": ["isis", "router", "NYC"],
			"score": 0.95
		}
	]`
	return &mockMemvidRunner{
		calls:        make([]mockMemvidCall, 0),
		searchResult: searchResult,
	}
}

func (m *mockMemvidRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader) (stdout, stderr string, err error) {
	// Capture the call
	var stdinContent string
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		stdinContent = string(data)
	}

	m.mu.Lock()
	m.calls = append(m.calls, mockMemvidCall{
		Command: name + " " + strings.Join(args, " "),
		Args:    args,
		Stdin:   stdinContent,
	})
	m.mu.Unlock()

	// Determine response based on command
	fullCmd := strings.Join(args, " ")

	if strings.Contains(fullCmd, "put") {
		// memory_save response
		return `{"status": "ok", "message": "Content saved successfully"}`, "", nil
	}

	if strings.Contains(fullCmd, "find") {
		// memory_search response
		if m.searchResult != "" {
			return m.searchResult, "", nil
		}
		return `[]`, "", nil
	}

	if strings.Contains(fullCmd, "ask") {
		// memory_ask response
		return `{"answer": "Based on stored data, DZ-NY5-SW01 is a router in NYC."}`, "", nil
	}

	if strings.Contains(fullCmd, "stats") {
		// memory_stats response
		return `{"total_frames": 10, "size_bytes": 1024}`, "", nil
	}

	return `{"status": "ok"}`, "", nil
}

func (m *mockMemvidRunner) GetCalls() []mockMemvidCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy
	result := make([]mockMemvidCall, len(m.calls))
	copy(result, m.calls)
	return result
}

// =============================================================================
// Mock ISIS Data
// =============================================================================

// mockISISResult creates realistic mock ISIS data for testing
func mockISISResult() *isis.Result {
	nodeSID101 := 101
	nodeSID102 := 102
	nodeSID103 := 103
	srgbBase := 900000
	srgbRange := 65536

	routers := map[string]isis.Router{
		"DZ-NY5-SW01": {
			Hostname:   "DZ-NY5-SW01",
			RouterID:   "172.16.0.1",
			SystemID:   "0000.0000.0001",
			RouterType: "L2",
			Area:       "49.0001",
			Location:   "NYC",
			Neighbors: []isis.Neighbor{
				{Hostname: "DZ-NY7-SW01", Metric: 100, NeighborAddr: "10.0.0.2"},
				{Hostname: "DZ-CHI-SW01", Metric: 200, NeighborAddr: "10.0.1.2"},
			},
			NodeSID:   &nodeSID101,
			SRGBBase:  &srgbBase,
			SRGBRange: &srgbRange,
		},
		"DZ-NY7-SW01": {
			Hostname:   "DZ-NY7-SW01",
			RouterID:   "172.16.0.2",
			SystemID:   "0000.0000.0002",
			RouterType: "L2",
			Area:       "49.0001",
			Location:   "NYC",
			Neighbors: []isis.Neighbor{
				{Hostname: "DZ-NY5-SW01", Metric: 100, NeighborAddr: "10.0.0.1"},
				{Hostname: "DZ-LON-SW01", Metric: 500, NeighborAddr: "10.0.2.2"},
			},
			NodeSID:   &nodeSID102,
			SRGBBase:  &srgbBase,
			SRGBRange: &srgbRange,
		},
		"DZ-CHI-SW01": {
			Hostname:   "DZ-CHI-SW01",
			RouterID:   "172.16.0.3",
			SystemID:   "0000.0000.0003",
			RouterType: "L2",
			Area:       "49.0001",
			Location:   "Chicago",
			Neighbors: []isis.Neighbor{
				{Hostname: "DZ-NY5-SW01", Metric: 200, NeighborAddr: "10.0.1.1"},
			},
			NodeSID:   &nodeSID103,
			SRGBBase:  &srgbBase,
			SRGBRange: &srgbRange,
		},
		"DZ-LON-SW01": {
			Hostname:     "DZ-LON-SW01",
			RouterID:     "172.16.0.4",
			SystemID:     "0000.0000.0004",
			RouterType:   "L2",
			Area:         "49.0001",
			Location:     "London",
			IsOverloaded: false,
			Neighbors: []isis.Neighbor{
				{Hostname: "DZ-NY7-SW01", Metric: 500, NeighborAddr: "10.0.2.1"},
			},
		},
	}

	return &isis.Result{
		Markdown: "# ISIS Network Summary\n\n4 routers, 4 links, 100% healthy",
		Routers:  routers,
		Stats: isis.NetworkStats{
			TotalRouters:     4,
			TotalLinks:       4,
			HealthyRouters:   4,
			SREnabledRouters: 3,
			HealthyPercent:   100.0,
			SRPercent:        75.0,
			AvgNeighbors:     1.5,
			MinSID:           101,
			MaxSID:           103,
			SRGBStart:        900000,
			SRGBEnd:          965535,
			SRGBConsistent:   true,
		},
	}
}
