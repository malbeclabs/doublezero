//go:build evals

package evals_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/stretchr/testify/require"
)

// TestLake_Agent_Evals_Anthropic_CypherPreference tests that the agent uses Cypher
// for path-finding queries when Neo4j is available.
func TestLake_Agent_Evals_Anthropic_CypherPreference(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_CypherPreference(t, newAnthropicLLMClient)
}

func runTest_CypherPreference(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data
	seedCypherPreferenceData(t, ctx, conn)

	// Get Neo4j client and seed graph data - this test requires Neo4j
	neo4jClient := testNeo4jClient(t)
	if neo4jClient == nil {
		t.Skip("Neo4j not available, skipping Cypher preference test")
	}
	seedCypherPreferenceGraphData(t, ctx, neo4jClient)
	validateGraphData(t, ctx, neo4jClient, 3, 2) // 3 devices, 2 links

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Create a workflow with query tracking
	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	// Test case: path finding should use Cypher, not SQL
	// This is a query that SQL cannot answer well - it requires graph traversal
	question := "what is the path from NYC to Tokyo?"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	// Check that the response contains the expected path
	expectations := []Expectation{
		{
			Description:   "Response describes the path through London",
			ExpectedValue: "Path goes through London: NYC -> LON -> TOK (or mentions nyc-lon-1 and lon-tok-1 links)",
			Rationale:     "Test data has NYC-LON-TOK path, no direct NYC-TOK link",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for cypher preference")

	// Verify that Cypher was used (check if any query contains MATCH which is Cypher syntax)
	cypherUsed := false
	for _, eq := range result.ExecutedQueries {
		sql := eq.GeneratedQuery.SQL
		// Cypher queries use MATCH, SQL queries use SELECT
		if strings.Contains(strings.ToUpper(sql), "MATCH") {
			cypherUsed = true
			t.Logf("Cypher query detected: %s", sql)
			break
		}
	}

	if !cypherUsed {
		t.Logf("Warning: Agent used SQL instead of Cypher for path-finding query")
		// Log all queries for debugging
		for i, eq := range result.ExecutedQueries {
			t.Logf("Query %d: %s", i+1, eq.GeneratedQuery.SQL)
		}
	}

	// For path finding queries, Cypher should be used
	require.True(t, cypherUsed, "Agent should use Cypher (MATCH) for path-finding queries, not SQL (SELECT)")
}

// seedCypherPreferenceData creates a simple 3-device network for testing
func seedCypherPreferenceData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tok", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "tok-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "lon-tok-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

// seedCypherPreferenceGraphData seeds the Neo4j graph with the same topology
func seedCypherPreferenceGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tok", Name: "Tokyo"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", MetroCode: "nyc"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", MetroCode: "lon"},
		{PK: "device3", Code: "tok-dzd1", Status: "activated", MetroPK: "metro3", MetroCode: "tok"},
	}
	links := []graphLink{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", SideAPK: "device1", SideZPK: "device2"},
		{PK: "link2", Code: "lon-tok-1", Status: "activated", SideAPK: "device2", SideZPK: "device3"},
	}

	seedGraphData(t, ctx, client, metros, devices, links)
}
