//go:build evals

package evals_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow/prompts"
	"github.com/stretchr/testify/require"
)

// TestLake_Agent_Evals_Anthropic_CypherGenerationLiteral tests that Cypher generation
// produces correct graph queries for various scenarios.
func TestLake_Agent_Evals_Anthropic_CypherGenerationLiteral(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_CypherGenerationLiteral(t)
}

// TestLake_Agent_Evals_Anthropic_CypherGenerationPreserveQuery tests that when modifying
// an existing Cypher query, the generator preserves the query structure.
func TestLake_Agent_Evals_Anthropic_CypherGenerationPreserveQuery(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_CypherGenerationPreserveQuery(t)
}

func runTest_CypherGenerationLiteral(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Build the system prompt for Cypher generation
	systemPrompt := buildTestCypherGeneratePrompt(t)

	// Create LLM client
	llmClient := workflow.NewAnthropicLLMClientWithName(
		anthropic.ModelClaude3_5Haiku20241022,
		1024,
		"cypher-gen-eval",
	)

	testCases := []struct {
		name           string
		prompt         string
		mustContain    []string // Cypher must contain these
		mustNotContain []string // Cypher must NOT contain these
	}{
		{
			name:   "shortest path between two devices",
			prompt: "find the shortest path between nyc-dzd1 and lon-dzd1",
			mustContain: []string{
				"MATCH",
				"shortestPath",
				"nyc-dzd1",
				"lon-dzd1",
				"CONNECTS",
			},
			mustNotContain: []string{
				"allShortestPaths", // Should use shortestPath for single path
			},
		},
		{
			name:   "find devices in a metro",
			prompt: "find all devices in the NYC metro",
			mustContain: []string{
				"MATCH",
				"Device",
				"Metro",
				"nyc",
				"LOCATED_IN",
			},
			mustNotContain: []string{
				"shortestPath", // Not a path query
				"CONNECTS",     // Not looking for links
			},
		},
		{
			name:   "find connected devices",
			prompt: "what devices are directly connected to nyc-dzd1",
			mustContain: []string{
				"MATCH",
				"Device",
				"nyc-dzd1",
				"CONNECTS",
			},
			mustNotContain: []string{
				"shortestPath", // Not asking for path, just direct connections
			},
		},
		{
			name:   "find all paths between metros",
			prompt: "find all paths between NYC and LON metros",
			mustContain: []string{
				"MATCH",
				"Metro",
				"nyc",
				"lon",
			},
			mustNotContain: []string{
				// Should have path traversal
			},
		},
		{
			name:   "shortest path between metros",
			prompt: "find the shortest path between NYC and LON metros",
			mustContain: []string{
				"MATCH",
				"Metro",
				"shortestPath",
				"ORDER BY",
				"LIMIT",
			},
			mustNotContain: []string{
				"allShortestPaths", // Should use shortestPath for single path
			},
		},
		{
			name:   "reachability from a device",
			prompt: "what devices are reachable from nyc-dzd1 within 3 hops",
			mustContain: []string{
				"MATCH",
				"Device",
				"nyc-dzd1",
				"*", // Variable length path
			},
			mustNotContain: []string{
				"shortestPath", // Not asking for shortest, asking for reachability
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if debug {
				t.Logf("=== Testing: %s ===", tc.name)
				t.Logf("Prompt: %s", tc.prompt)
			}

			response, err := llmClient.Complete(ctx, systemPrompt, tc.prompt)
			require.NoError(t, err)

			cypher := extractCypher(response)
			if debug {
				if debugLevel == 1 {
					t.Logf("Cypher: %s", truncate(cypher, 200))
				} else {
					t.Logf("Full response:\n%s", response)
					t.Logf("Extracted Cypher:\n%s", cypher)
				}
			}

			require.NotEmpty(t, cypher, "Should have extracted Cypher from response")

			cypherLower := strings.ToLower(cypher)

			// Check required content
			for _, must := range tc.mustContain {
				require.True(t, strings.Contains(cypherLower, strings.ToLower(must)),
					"Cypher should contain '%s' but got: %s", must, cypher)
			}

			// Check forbidden content
			for _, mustNot := range tc.mustNotContain {
				require.False(t, strings.Contains(cypherLower, strings.ToLower(mustNot)),
					"Cypher should NOT contain '%s' but got: %s", mustNot, cypher)
			}
		})
	}
}

func runTest_CypherGenerationPreserveQuery(t *testing.T) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()

	// Build the system prompt
	systemPrompt := buildTestCypherGeneratePrompt(t)

	// Create LLM client
	llmClient := workflow.NewAnthropicLLMClientWithName(
		anthropic.ModelClaude3_5Haiku20241022,
		1024,
		"cypher-gen-eval",
	)

	testCases := []struct {
		name           string
		currentQuery   string
		prompt         string
		mustContain    []string // Must be preserved from original
		mustNotContain []string // Should not be changed/added
	}{
		{
			name:         "add status filter should preserve structure",
			currentQuery: "MATCH (d:Device) RETURN d.code, d.status",
			prompt:       "add a filter for activated devices only",
			mustContain: []string{
				"Device",
				"d.code",
				"d.status",
				"activated",
			},
			mustNotContain: []string{
				"Metro",   // Should not add nodes
				"Link",    // Should not add relationships
			},
		},
		{
			name:         "add limit should only add limit",
			currentQuery: "MATCH (d:Device)-[:LOCATED_IN]->(m:Metro) RETURN d.code, m.code",
			prompt:       "add LIMIT 10",
			mustContain: []string{
				"Device",
				"Metro",
				"LOCATED_IN",
				"d.code",
				"m.code",
				"LIMIT 10",
			},
			mustNotContain: []string{
				"Link",      // Should not add nodes
				"ORDER BY",  // Should not add ordering
			},
		},
		{
			name:         "change return columns should preserve match",
			currentQuery: "MATCH (d:Device {code: 'nyc-dzd1'})<-[:CONNECTS]-(l:Link) RETURN l.code",
			prompt:       "also return the link status",
			mustContain: []string{
				"Device",
				"nyc-dzd1",
				"CONNECTS",
				"Link",
				"l.code",
				"l.status",
			},
			mustNotContain: []string{
				"Metro", // Should not add unrelated nodes
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Format prompt like modify requests
			userPrompt := "Current query:\n" + tc.currentQuery + "\n\nUser request: " + tc.prompt

			if debug {
				t.Logf("=== Testing: %s ===", tc.name)
				t.Logf("Current query: %s", tc.currentQuery)
				t.Logf("Request: %s", tc.prompt)
			}

			response, err := llmClient.Complete(ctx, systemPrompt, userPrompt)
			require.NoError(t, err)

			cypher := extractCypher(response)
			if debug {
				if debugLevel == 1 {
					t.Logf("Cypher: %s", truncate(cypher, 200))
				} else {
					t.Logf("Full response:\n%s", response)
					t.Logf("Extracted Cypher:\n%s", cypher)
				}
			}

			require.NotEmpty(t, cypher, "Should have extracted Cypher from response")

			// Check required content (preserved + requested change)
			for _, must := range tc.mustContain {
				require.True(t, strings.Contains(cypher, must) || strings.Contains(strings.ToLower(cypher), strings.ToLower(must)),
					"Cypher should contain '%s' but got: %s", must, cypher)
			}

			// Check forbidden content
			for _, mustNot := range tc.mustNotContain {
				require.False(t, strings.Contains(cypher, mustNot) || strings.Contains(strings.ToLower(cypher), strings.ToLower(mustNot)),
					"Cypher should NOT contain '%s' but got: %s", mustNot, cypher)
			}
		})
	}
}

// buildTestCypherGeneratePrompt builds the system prompt for Cypher generation
func buildTestCypherGeneratePrompt(t *testing.T) string {
	// Load CYPHER_CONTEXT
	cypherContextData, err := prompts.PromptsFS.ReadFile("CYPHER_CONTEXT.md")
	require.NoError(t, err, "Failed to load CYPHER_CONTEXT.md")
	cypherContext := strings.TrimSpace(string(cypherContextData))

	// Load GENERATE_CYPHER.md and compose with CYPHER_CONTEXT
	generateData, err := prompts.PromptsFS.ReadFile("GENERATE_CYPHER.md")
	require.NoError(t, err, "Failed to load GENERATE_CYPHER.md")
	generatePrompt := strings.TrimSpace(string(generateData))
	generatePrompt = strings.ReplaceAll(generatePrompt, "{{CYPHER_CONTEXT}}", cypherContext)

	// Add editor-specific instructions
	editorInstructions := `

## FINAL INSTRUCTIONS (MUST FOLLOW)

1. Output ONLY a Cypher code block. No text before or after.
2. If modifying an existing query: change ONLY what was asked. Keep everything else identical.
3. Do NOT add nodes, relationships, or properties beyond what was explicitly requested.`

	// Add a minimal schema for testing (must match actual graph structure)
	schema := `
## Graph Schema

Node Labels:
- Device (pk, code, status, device_type, public_ip)
- Link (pk, code, status, bandwidth, isis_delay_override_ns)
- Metro (pk, code, name)
- Contributor (pk, code, name)

Relationships:
- (:Link)-[:CONNECTS {side, iface_name}]->(:Device) - Links point TO devices, use undirected for traversal
- (:Device)-[:LOCATED_IN]->(:Metro)
- (:Device)-[:OPERATES]->(:Contributor)
- (:Link)-[:OWNED_BY]->(:Contributor)
- (:Device)-[:ISIS_ADJACENT {metric}]->(:Device)
`

	return generatePrompt + "\n\n" + schema + editorInstructions
}

// extractCypher extracts Cypher from a response that may contain markdown code blocks
func extractCypher(response string) string {
	response = strings.TrimSpace(response)

	// Try to extract from ```cypher block
	cypherBlockRe := regexp.MustCompile("(?s)```cypher\\s*\\n?(.*?)\\n?```")
	if matches := cypherBlockRe.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Try generic ``` block
	genericBlockRe := regexp.MustCompile("(?s)```\\s*\\n?(.*?)\\n?```")
	if matches := genericBlockRe.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no code block, return the whole response (might be raw Cypher)
	return response
}

// TestLake_Agent_Evals_Anthropic_CypherExecutionIntegration tests that generated Cypher
// queries actually execute correctly against a real Neo4j database.
// This catches issues where queries look syntactically correct but fail due to schema mismatches.
func TestLake_Agent_Evals_Anthropic_CypherExecutionIntegration(t *testing.T) {
	t.Parallel()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	neo4jClient := testNeo4jClient(t)
	if neo4jClient == nil {
		t.Skip("Neo4j not available, skipping integration test")
	}

	ctx := t.Context()
	debugLevel, debug := getDebugLevel()

	// Seed the graph with a known topology:
	// NYC metro: nyc-dzd1, nyc-dzd2
	// LON metro: lon-dzd1
	// Links: nyc-dzd1 <-> nyc-dzd2, nyc-dzd2 <-> lon-dzd1
	metros := []graphMetro{
		{PK: "metro-nyc", Code: "nyc", Name: "New York"},
		{PK: "metro-lon", Code: "lon", Name: "London"},
	}
	devices := []graphDevice{
		{PK: "dev-nyc1", Code: "nyc-dzd1", Status: "active", MetroPK: "metro-nyc"},
		{PK: "dev-nyc2", Code: "nyc-dzd2", Status: "active", MetroPK: "metro-nyc"},
		{PK: "dev-lon1", Code: "lon-dzd1", Status: "active", MetroPK: "metro-lon"},
	}
	links := []graphLink{
		{PK: "link-1", Code: "nyc-internal-1", Status: "activated", SideAPK: "dev-nyc1", SideZPK: "dev-nyc2"},
		{PK: "link-2", Code: "nyc-lon-1", Status: "activated", SideAPK: "dev-nyc2", SideZPK: "dev-lon1"},
	}

	seedGraphData(t, ctx, neo4jClient, metros, devices, links)
	validateGraphData(t, ctx, neo4jClient, 3, 2)

	// Build system prompt
	systemPrompt := buildTestCypherGeneratePrompt(t)

	// Create LLM client
	llmClient := workflow.NewAnthropicLLMClientWithName(
		anthropic.ModelClaude3_5Haiku20241022,
		1024,
		"cypher-exec-eval",
	)

	// Create querier for executing generated Cypher
	querier := NewNeo4jQuerier(neo4jClient)

	testCases := []struct {
		name           string
		prompt         string
		expectRows     int  // Expected minimum row count (-1 to skip check)
		expectNonEmpty bool // Expect at least one row
	}{
		{
			name:           "find devices in NYC metro",
			prompt:         "find all devices in the nyc metro",
			expectRows:     2,
			expectNonEmpty: true,
		},
		{
			name:           "find devices connected to nyc-dzd1",
			prompt:         "what devices are directly connected to nyc-dzd1",
			expectRows:     1, // nyc-dzd2
			expectNonEmpty: true,
		},
		{
			name:           "shortest path between NYC and LON devices",
			prompt:         "find the shortest path between nyc-dzd1 and lon-dzd1",
			expectRows:     -1, // Path query returns structured data
			expectNonEmpty: true,
		},
		{
			name:           "all metros",
			prompt:         "list all metros",
			expectRows:     2,
			expectNonEmpty: true,
		},
		{
			name:           "devices reachable from nyc-dzd1",
			prompt:         "what devices are reachable from nyc-dzd1 within 4 hops",
			expectRows:     -1, // Variable
			expectNonEmpty: true,
		},
		{
			name:           "shortest path between metros",
			prompt:         "find the shortest path between nyc and lon metros",
			expectRows:     -1, // Path query returns structured data
			expectNonEmpty: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if debug {
				t.Logf("=== Testing: %s ===", tc.name)
				t.Logf("Prompt: %s", tc.prompt)
			}

			// Generate Cypher
			response, err := llmClient.Complete(ctx, systemPrompt, tc.prompt)
			require.NoError(t, err)

			cypher := extractCypher(response)
			if debug {
				if debugLevel == 1 {
					t.Logf("Generated Cypher: %s", truncate(cypher, 200))
				} else {
					t.Logf("Generated Cypher:\n%s", cypher)
				}
			}
			require.NotEmpty(t, cypher, "Should have generated Cypher")

			// Execute the generated Cypher against Neo4j
			result, err := querier.Query(ctx, cypher)
			require.NoError(t, err, "Query execution should not error")

			if debug {
				t.Logf("Result: %d rows, error: %q", result.Count, result.Error)
				if debugLevel >= 2 && len(result.Rows) > 0 {
					t.Logf("First row: %v", result.Rows[0])
				}
			}

			// Verify no query error
			require.Empty(t, result.Error, "Query should not return error: %s\nCypher: %s", result.Error, cypher)

			// Verify expected results
			if tc.expectNonEmpty {
				require.Greater(t, result.Count, 0, "Query should return results\nCypher: %s", cypher)
			}
			if tc.expectRows >= 0 {
				require.GreaterOrEqual(t, result.Count, tc.expectRows,
					"Query should return at least %d rows, got %d\nCypher: %s", tc.expectRows, result.Count, cypher)
			}
		})
	}
}
