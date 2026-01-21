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

func TestLake_Agent_Evals_Anthropic_TopologyDiscovery(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_TopologyDiscovery(t, newAnthropicLLMClient)
}

func runTest_TopologyDiscovery(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed ClickHouse data
	seedTopologyDiscoveryData(t, ctx, conn)

	// Get Neo4j client and seed graph data if available
	neo4jClient := testNeo4jClient(t)
	if neo4jClient != nil {
		seedTopologyDiscoveryGraphData(t, ctx, neo4jClient)
		validateGraphData(t, ctx, neo4jClient, 4, 5) // 4 devices, 5 links
	} else {
		t.Log("Neo4j not available, running without graph database")
	}

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	question := "show me all devices and links connected to the Singapore device"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response identifies the Singapore device",
			ExpectedValue: "Identifies sin-dzd1 as the Singapore device",
			Rationale:     "Test data has sin-dzd1 in Singapore metro",
		},
		{
			Description:   "Response lists all connected links",
			ExpectedValue: "Lists sin-hkg-1, sin-sel-1, sin-tyo-1 as connected links (3 total)",
			Rationale:     "Singapore device has 3 direct links to HKG, SEL, and TYO",
		},
		{
			Description:   "Response identifies the connected devices/metros",
			ExpectedValue: "Identifies connections to Hong Kong (hkg-dzd1), Seoul (sel-dzd1), and Tokyo (tyo-dzd1)",
			Rationale:     "These are the devices on the other end of the links",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for topology discovery")
}

// seedTopologyDiscoveryData creates a star topology with Singapore at the center:
//
//	    HKG
//	     │
//	SEL──SIN──TYO
//	     │
//	    (extra link between HKG-SEL for complexity)
//
// Singapore device (sin-dzd1) connects directly to: HKG, SEL, TYO
func seedTopologyDiscoveryData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro3", Code: "sel", Name: "Seoul"},
		{PK: "metro4", Code: "tyo", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "sel-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		// SIN connects to all three other metros directly
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "sin-sel-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link3", Code: "sin-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device4", SideAIfaceName: "Ethernet3", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		// Extra links not directly connected to SIN (should NOT appear in response)
		{PK: "link4", Code: "hkg-sel-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2", Bandwidth: 10000000000},
		{PK: "link5", Code: "sel-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device4", SideAIfaceName: "Ethernet3", SideZIfaceName: "Ethernet2", Bandwidth: 10000000000},
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

// seedTopologyDiscoveryGraphData seeds the Neo4j graph with the same topology
func seedTopologyDiscoveryGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "sin", Name: "Singapore"},
		{PK: "metro2", Code: "hkg", Name: "Hong Kong"},
		{PK: "metro3", Code: "sel", Name: "Seoul"},
		{PK: "metro4", Code: "tyo", Name: "Tokyo"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "sin-dzd1", Status: "activated", MetroPK: "metro1", MetroCode: "sin"},
		{PK: "device2", Code: "hkg-dzd1", Status: "activated", MetroPK: "metro2", MetroCode: "hkg"},
		{PK: "device3", Code: "sel-dzd1", Status: "activated", MetroPK: "metro3", MetroCode: "sel"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro4", MetroCode: "tyo"},
	}
	links := []graphLink{
		{PK: "link1", Code: "sin-hkg-1", Status: "activated", SideAPK: "device1", SideZPK: "device2"},
		{PK: "link2", Code: "sin-sel-1", Status: "activated", SideAPK: "device1", SideZPK: "device3"},
		{PK: "link3", Code: "sin-tyo-1", Status: "activated", SideAPK: "device1", SideZPK: "device4"},
		{PK: "link4", Code: "hkg-sel-1", Status: "activated", SideAPK: "device2", SideZPK: "device3"},
		{PK: "link5", Code: "sel-tyo-1", Status: "activated", SideAPK: "device3", SideZPK: "device4"},
	}

	seedGraphData(t, ctx, client, metros, devices, links)
}
