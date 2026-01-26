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

func TestLake_Agent_Evals_Anthropic_NetworkPaths(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkPaths(t, newAnthropicLLMClient)
}

// TestLake_Agent_Evals_Anthropic_ShortestPath tests that "shortest path" prompts
// correctly route to Cypher (not SQL). This is a regression test for a bug where
// the model would use SQL latency comparison views instead of Cypher path finding.
func TestLake_Agent_Evals_Anthropic_ShortestPath(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ShortestPath(t, newAnthropicLLMClient)
}


func runTest_NetworkPaths(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data (for SQL queries)
	seedNetworkPathsData(t, ctx, conn)
	validateNetworkPathsQuery(t, ctx, conn)

	// Get Neo4j client and seed graph data if available
	neo4jClient := testNeo4jClient(t)
	if neo4jClient != nil {
		seedNetworkPathsGraphData(t, ctx, neo4jClient)
		validateGraphData(t, ctx, neo4jClient, 4, 4) // 4 devices, 4 links for multi-hop paths
	} else {
		t.Log("Neo4j not available, running without graph database")
	}

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Use workflow with Neo4j support if available
	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	question := "confirm for me the paths between SIN and TYO"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response identifies the path through Hong Kong",
			ExpectedValue: "Path via HKG mentioned: SIN -> HKG -> TYO (or sin-hkg-1 and hkg-tyo-1 links)",
			Rationale:     "Test data has a 2-hop path through Hong Kong",
		},
		{
			Description:   "Response identifies the path through Seoul",
			ExpectedValue: "Path via SEL mentioned: SIN -> SEL -> TYO (or sin-sel-1 and sel-tyo-1 links)",
			Rationale:     "Test data has a 2-hop path through Seoul",
		},
		{
			Description:   "Response confirms paths are available",
			ExpectedValue: "Indicates paths/links are activated or available",
			Rationale:     "User wants to confirm paths are working",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for network paths")
}

// seedNetworkPathsData creates network topology with multi-hop SIN-TYO paths
// No direct SIN-TYO link - must traverse through HKG or SEL:
// - Path 1: SIN -> HKG -> TYO (via Hong Kong)
// - Path 2: SIN -> SEL -> TYO (via Seoul)
func seedNetworkPathsData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "tyo", Name: "Tokyo"},
		{PK: "metro3", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro4", Code: "sel", Name: "Seoul"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "sel-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	// Multi-hop topology: SIN connects to HKG and SEL, both connect to TYO
	links := []serviceability.Link{
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "hkg-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link3", Code: "sin-sel-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device4", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link4", Code: "sel-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device4", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2", Bandwidth: 10000000000},
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

func validateNetworkPathsQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify there are NO direct SIN-TYO links
	directQuery := `
SELECT l.code
FROM dz_links_current l
JOIN dz_devices_current da ON l.side_a_pk = da.pk
JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
JOIN dz_metros_current ma ON da.metro_pk = ma.pk
JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE (ma.code = 'sin' AND mz.code = 'tyo') OR (ma.code = 'tyo' AND mz.code = 'sin')
`
	directResult, err := dataset.Query(ctx, conn, directQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 0, directResult.Count, "Should have NO direct links between SIN and TYO")

	// Verify the intermediate links exist
	allLinksQuery := `SELECT code FROM dz_links_current ORDER BY code`
	allLinksResult, err := dataset.Query(ctx, conn, allLinksQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 4, allLinksResult.Count, "Should have 4 links total")
	t.Logf("Database validation passed: 0 direct SIN-TYO links, 4 total links for multi-hop paths")
}

// runTest_ShortestPath tests that "shortest path" questions route to Cypher, not SQL.
// This is a regression test - the model was incorrectly using dz_vs_internet_latency_comparison
// SQL view when asked for "shortest path" between metros.
func runTest_ShortestPath(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data (for SQL queries)
	seedNetworkPathsData(t, ctx, conn)

	// Get Neo4j client and seed graph data - REQUIRED for this test
	neo4jClient := testNeo4jClient(t)
	if neo4jClient == nil {
		t.Skip("Neo4j not available, skipping shortest path test (requires graph database)")
	}
	seedNetworkPathsGraphData(t, ctx, neo4jClient)
	validateGraphData(t, ctx, neo4jClient, 4, 4)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	// This exact phrasing was failing - it triggered SQL instead of Cypher
	question := "shortest path from sin to tyo"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// CRITICAL: Verify Cypher was used, not SQL
	// Check executed queries for Cypher syntax (MATCH) vs SQL syntax (SELECT)
	require.NotEmpty(t, result.ExecutedQueries, "Should have executed at least one query")

	foundCypher := false
	for _, eq := range result.ExecutedQueries {
		query := eq.Result.SQL // This field holds either SQL or Cypher
		t.Logf("Executed query: %s", query)

		// Check for SQL patterns that indicate wrong routing
		if strings.Contains(strings.ToUpper(query), "DZ_VS_INTERNET_LATENCY_COMPARISON") {
			t.Errorf("WRONG: Used SQL latency comparison view instead of Cypher for path finding")
		}
		if strings.Contains(strings.ToUpper(query), "SELECT") && !strings.Contains(strings.ToUpper(query), "MATCH") {
			t.Logf("WARNING: Query appears to be SQL, not Cypher: %s", query)
		}

		// Check for Cypher patterns
		if strings.Contains(strings.ToUpper(query), "MATCH") {
			foundCypher = true
		}
		if strings.Contains(strings.ToLower(query), "shortestpath") {
			foundCypher = true
		}
	}
	require.True(t, foundCypher, "Should have used Cypher (with MATCH or shortestPath) for path finding, but only SQL was found")

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	// Verify the response describes the actual path(s)
	expectations := []Expectation{
		{
			Description:   "Response identifies a path through an intermediate metro",
			ExpectedValue: "Mentions HKG (Hong Kong) or SEL (Seoul) as intermediate hop, or lists the links (sin-hkg-1, hkg-tyo-1, sin-sel-1, sel-tyo-1)",
			Rationale:     "There is no direct SIN-TYO link; path must go through HKG or SEL",
		},
		{
			Description:   "Response describes topology, not latency metrics",
			ExpectedValue: "Describes devices/links in the path, not RTT or latency comparison numbers",
			Rationale:     "This is a path-finding question, not a latency metrics question",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for shortest path")
}

// seedNetworkPathsGraphData seeds the Neo4j graph with the same multi-hop topology
// No direct SIN-TYO link - must traverse through HKG or SEL:
// - Path 1: SIN -> HKG -> TYO (via Hong Kong)
// - Path 2: SIN -> SEL -> TYO (via Seoul)
func seedNetworkPathsGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	// Use the helper function with matching data from ClickHouse seed
	metros := []graphMetro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "tyo", Name: "Tokyo"},
		{PK: "metro3", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro4", Code: "sel", Name: "Seoul"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", MetroCode: "sin"},
		{PK: "device2", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro2", MetroCode: "tyo"},
		{PK: "device3", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro3", MetroCode: "hkg"},
		{PK: "device4", Code: "sel-dzd1", Status: "activated", MetroPK: "metro4", MetroCode: "sel"},
	}
	// Multi-hop topology: SIN connects to HKG and SEL, both connect to TYO
	links := []graphLink{
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", SideAPK: "device1", SideZPK: "device3"},
		{PK: "link2", Code: "hkg-tyo-1", Status: "activated", SideAPK: "device3", SideZPK: "device2"},
		{PK: "link3", Code: "sin-sel-1", Status: "activated", SideAPK: "device1", SideZPK: "device4"},
		{PK: "link4", Code: "sel-tyo-1", Status: "activated", SideAPK: "device4", SideZPK: "device2"},
	}

	seedGraphData(t, ctx, client, metros, devices, links)
}

// TestLake_Agent_Evals_Anthropic_MetroToMetroShortestPath tests that "shortest path between
// metro A and metro B" queries correctly find the overall shortest path among all device pairs.
// This is a regression test for a bug where the agent would create a cartesian product of
// all devices in each metro without ordering by path length to find the true shortest.
func TestLake_Agent_Evals_Anthropic_MetroToMetroShortestPath(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_MetroToMetroShortestPath(t, newAnthropicLLMClient)
}

// runTest_MetroToMetroShortestPath tests metro-to-metro shortest path queries.
// The topology has multiple devices per metro with different path lengths:
// - NYC: nyc-dzd1, nyc-dzd2
// - LON: lon-dzd1, lon-dzd2
// - Paths: nyc-dzd1 -> lon-dzd1 (direct, 1 hop)
//          nyc-dzd2 -> fra-dzd1 -> lon-dzd2 (2 hops via Frankfurt)
// The query must find the shortest (nyc-dzd1 -> lon-dzd1), not an arbitrary path.
func runTest_MetroToMetroShortestPath(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data
	seedMetroToMetroShortestPathData(t, ctx, conn)

	// Get Neo4j client - REQUIRED for this test
	neo4jClient := testNeo4jClient(t)
	if neo4jClient == nil {
		t.Skip("Neo4j not available, skipping metro-to-metro shortest path test (requires graph database)")
	}
	seedMetroToMetroShortestPathGraphData(t, ctx, neo4jClient)
	validateGraphData(t, ctx, neo4jClient, 5, 3) // 5 devices, 3 links

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	// Use metro names, not device codes - this is the key difference from the device-to-device test
	question := "find the shortest path between NYC and LON"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	// CRITICAL: Verify Cypher query structure
	require.NotEmpty(t, result.ExecutedQueries, "Should have executed at least one query")

	foundCypher := false
	foundOrderBy := false
	foundLimit := false
	for _, eq := range result.ExecutedQueries {
		query := eq.Result.SQL // This field holds either SQL or Cypher
		queryUpper := strings.ToUpper(query)
		queryLower := strings.ToLower(query)

		if debug {
			if debugLevel == 1 {
				t.Logf("Executed query: %s", truncate(query, 200))
			} else {
				t.Logf("Executed query: %s", query)
			}
		}

		// Check for SQL patterns that indicate wrong routing
		if strings.Contains(queryUpper, "SELECT") && !strings.Contains(queryUpper, "MATCH") {
			t.Logf("WARNING: Query appears to be SQL, not Cypher")
		}

		// Check for Cypher patterns
		if strings.Contains(queryUpper, "MATCH") {
			foundCypher = true

			// For metro-to-metro queries, must have ORDER BY and LIMIT
			if strings.Contains(queryUpper, "ORDER BY") {
				foundOrderBy = true
			}
			if strings.Contains(queryUpper, "LIMIT") {
				foundLimit = true
			}

			// Check for problematic patterns that indicate the cartesian product bug
			if strings.Contains(queryLower, "collect(distinct") && !strings.Contains(queryUpper, "ORDER BY") {
				t.Errorf("WRONG: Using COLLECT(DISTINCT ...) without ORDER BY - this mixes results from multiple paths")
			}
		}
	}

	require.True(t, foundCypher, "Should have used Cypher for path finding")
	require.True(t, foundOrderBy, "Metro-to-metro shortest path query should ORDER BY path length to find the true shortest")
	require.True(t, foundLimit, "Metro-to-metro shortest path query should LIMIT 1 to return only the shortest path")

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	// Verify the response describes the actual shortest path
	expectations := []Expectation{
		{
			Description:   "Response identifies the direct NYC-LON path",
			ExpectedValue: "Mentions nyc-dzd1, lon-dzd1, or nyc-lon-direct link as the shortest path",
			Rationale:     "The direct path (1 hop) is shorter than the Frankfurt path (2 hops)",
		},
		{
			Description:   "Response describes a single shortest path, not multiple paths",
			ExpectedValue: "Describes one clear path, not a mix of segments from different paths",
			Rationale:     "Query asked for 'shortest path' (singular), should return the one shortest",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for metro-to-metro shortest path")
}

// seedMetroToMetroShortestPathData creates topology with multiple devices per metro
// and different path lengths between metros.
func seedMetroToMetroShortestPathData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "fra", Name: "Frankfurt"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Multiple devices per metro
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device3", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device4", Code: "lon-dzd2", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device5", Code: "fra-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	// Topology:
	// - Direct: nyc-dzd1 <-> lon-dzd1 (1 hop - shortest!)
	// - Via FRA: nyc-dzd2 <-> fra-dzd1 <-> lon-dzd2 (2 hops)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-direct", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "nyc-fra-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device5", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link3", Code: "fra-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device5", SideZPK: "device4", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
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

// seedMetroToMetroShortestPathGraphData seeds Neo4j with the same topology
func seedMetroToMetroShortestPathGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "fra", Name: "Frankfurt"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", MetroCode: "nyc"},
		{PK: "device2", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1", MetroCode: "nyc"},
		{PK: "device3", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", MetroCode: "lon"},
		{PK: "device4", Code: "lon-dzd2", Status: "activated", MetroPK: "metro2", MetroCode: "lon"},
		{PK: "device5", Code: "fra-dzd1", Status: "activated", MetroPK: "metro3", MetroCode: "fra"},
	}
	// Same topology as ClickHouse seed
	links := []graphLink{
		{PK: "link1", Code: "nyc-lon-direct", Status: "activated", SideAPK: "device1", SideZPK: "device3"},
		{PK: "link2", Code: "nyc-fra-1", Status: "activated", SideAPK: "device2", SideZPK: "device5"},
		{PK: "link3", Code: "fra-lon-1", Status: "activated", SideAPK: "device5", SideZPK: "device4"},
	}

	seedGraphData(t, ctx, client, metros, devices, links)
}
