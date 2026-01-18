//go:build evals

package evals_test

import (
	"context"
	"os"
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
