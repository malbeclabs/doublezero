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

func TestLake_Agent_Evals_Anthropic_Reachability(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_Reachability(t, newAnthropicLLMClient)
}

func runTest_Reachability(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data
	seedReachabilityData(t, ctx, conn)

	// Get Neo4j client and seed graph data if available
	neo4jClient := testNeo4jClient(t)
	if neo4jClient != nil {
		seedReachabilityGraphData(t, ctx, neo4jClient)
		validateGraphData(t, ctx, neo4jClient, 5, 5) // 5 devices, 5 links
	} else {
		t.Log("Neo4j not available, running without graph database")
	}

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	question := "what metros can I reach from Singapore, including through multi-hop paths?"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response identifies Hong Kong as reachable (direct)",
			ExpectedValue: "Hong Kong (HKG) is reachable from Singapore",
			Rationale:     "Test data has direct SIN-HKG link",
		},
		{
			Description:   "Response identifies Seoul as reachable (direct)",
			ExpectedValue: "Seoul (SEL) is reachable from Singapore",
			Rationale:     "Test data has direct SIN-SEL link",
		},
		{
			Description:   "Response identifies Tokyo as reachable (via intermediate)",
			ExpectedValue: "Tokyo (TYO) is reachable from Singapore (via HKG or SEL)",
			Rationale:     "Test data has 2-hop paths to Tokyo through HKG and SEL",
		},
		{
			Description:   "Response identifies Los Angeles as reachable (via intermediate)",
			ExpectedValue: "Los Angeles (LAX) is reachable from Singapore (via HKG)",
			Rationale:     "Test data has 2-hop path to LAX through HKG",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for reachability")
}

// seedReachabilityData creates a hub-and-spoke topology centered on Singapore:
//
//	LAX ── HKG ── SIN ── SEL ── TYO
//	              │
//	              └── (also connects to HKG and SEL which connect to TYO)
//
// From SIN, reachable metros are: HKG (1 hop), SEL (1 hop), TYO (2 hops via HKG or SEL), LAX (2 hops via HKG)
func seedReachabilityData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro3", Code: "sel", Name: "Seoul"},
		{PK: "metro4", Code: "tyo", Name: "Tokyo"},
		{PK: "metro5", Code: "lax", Name: "Los Angeles"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "sel-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device5", Code: "lax-dzd1", Status: "activated", MetroPK: "metro5", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		// SIN connects directly to HKG and SEL
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "sin-sel-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		// HKG connects to TYO and LAX
		{PK: "link3", Code: "hkg-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device4", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link4", Code: "hkg-lax-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device5", SideAIfaceName: "Ethernet3", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		// SEL connects to TYO
		{PK: "link5", Code: "sel-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device4", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
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

// seedReachabilityGraphData seeds the Neo4j graph with the same topology
func seedReachabilityGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro3", Code: "sel", Name: "Seoul"},
		{PK: "metro4", Code: "tyo", Name: "Tokyo"},
		{PK: "metro5", Code: "lax", Name: "Los Angeles"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", MetroCode: "sin"},
		{PK: "device2", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro2", MetroCode: "hkg"},
		{PK: "device3", Code: "sel-dzd1", Status: "activated", MetroPK: "metro3", MetroCode: "sel"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro4", MetroCode: "tyo"},
		{PK: "device5", Code: "lax-dzd1", Status: "activated", MetroPK: "metro5", MetroCode: "lax"},
	}
	links := []graphLink{
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", SideAPK: "device1", SideZPK: "device2"},
		{PK: "link2", Code: "sin-sel-1", Status: "activated", SideAPK: "device1", SideZPK: "device3"},
		{PK: "link3", Code: "hkg-tyo-1", Status: "activated", SideAPK: "device2", SideZPK: "device4"},
		{PK: "link4", Code: "hkg-lax-1", Status: "activated", SideAPK: "device2", SideZPK: "device5"},
		{PK: "link5", Code: "sel-tyo-1", Status: "activated", SideAPK: "device3", SideZPK: "device4"},
	}

	seedGraphData(t, ctx, client, metros, devices, links)
}
