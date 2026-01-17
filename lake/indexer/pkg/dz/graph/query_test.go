package graph

import (
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/isis"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

func TestStore_ReachableFromMetro(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()

	// Setup test data
	setupTestData(t, chClient)

	// Create and sync graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	ctx := t.Context()
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Query devices reachable from metro1
	devices, err := graphStore.ReachableFromMetro(ctx, "metro1", true)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(devices), 2, "expected at least 2 devices reachable from metro1")
}

func TestStore_ShortestPath(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()

	// Setup test data
	setupTestData(t, chClient)

	// Create and sync graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	ctx := t.Context()
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Find shortest path from device1 to device3
	path, err := graphStore.ShortestPath(ctx, "device1", "device3", PathWeightHops)
	require.NoError(t, err)
	require.NotEmpty(t, path, "expected non-empty path")

	// Path should include device1 -> link1 -> device2 -> link2 -> device3
	require.GreaterOrEqual(t, len(path), 3, "expected at least 3 segments in path")
}

func TestStore_ExplainRoute(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()

	// Setup test data
	setupTestData(t, chClient)

	// Create and sync graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	ctx := t.Context()
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Explain route from device1 to device3
	route, err := graphStore.ExplainRoute(ctx, "device1", "device3")
	require.NoError(t, err)
	require.NotEmpty(t, route, "expected non-empty route")

	// Verify route starts with device1
	require.Equal(t, "device", route[0].Type)
	require.Equal(t, "device1", route[0].PK)
}

func TestStore_NetworkAroundDevice(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()

	// Setup test data
	setupTestData(t, chClient)

	// Create and sync graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	ctx := t.Context()
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Get network around device2 (which is connected to both device1 and device3)
	subgraph, err := graphStore.NetworkAroundDevice(ctx, "device2", 1)
	require.NoError(t, err)
	require.NotNil(t, subgraph)

	// Should include device2 and its neighbors
	require.GreaterOrEqual(t, len(subgraph.Devices), 1, "expected at least 1 device")
}

// setupISISTestData creates test data with ISIS-compatible tunnel_net (/31) for correlation
func setupISISTestData(t *testing.T, chClient clickhouse.Client, graphStore *Store) {
	ctx := t.Context()
	log := laketesting.NewLogger()

	// Clear existing data
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create contributors
	contributors := []dzsvc.Contributor{
		{PK: "contrib1", Code: "test1", Name: "Test Contributor 1"},
	}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	// Create metros
	metros := []dzsvc.Metro{
		{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128},
		{PK: "metro2", Code: "DC", Name: "Washington DC", Longitude: -77.0369, Latitude: 38.9072},
	}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	// Create 3 devices forming a chain: device1 -- device2 -- device3
	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "DZ-NY1-SW01", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "DZ-NY2-SW01", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "DZ-DC1-SW01", PublicIP: "1.2.3.6", ContributorPK: "contrib1", MetroPK: "metro2", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	// Create links with /31 tunnel_net for ISIS correlation
	// link1: device1 (172.16.0.0) <-> device2 (172.16.0.1)
	// link2: device2 (172.16.0.2) <-> device3 (172.16.0.3)
	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "172.16.0.0/31", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
		{PK: "link2", Status: "active", Code: "link2", TunnelNet: "172.16.0.2/31", ContributorPK: "contrib1", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 2000000, CommittedJitterNs: 200000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	// Sync serviceability data to Neo4j
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Sync ISIS data
	// device1 sees device2 via 172.16.0.1
	// device2 sees device1 via 172.16.0.0 and device3 via 172.16.0.3
	// device3 sees device2 via 172.16.0.2
	lsps := []isis.LSP{
		{
			SystemID: "0000.0000.0001.00-00",
			Hostname: "DZ-NY1-SW01",
			RouterID: "10.0.0.1",
			Neighbors: []isis.Neighbor{
				{SystemID: "0000.0000.0002", Metric: 100, NeighborAddr: "172.16.0.1", AdjSIDs: []uint32{16001}},
			},
		},
		{
			SystemID: "0000.0000.0002.00-00",
			Hostname: "DZ-NY2-SW01",
			RouterID: "10.0.0.2",
			Neighbors: []isis.Neighbor{
				{SystemID: "0000.0000.0001", Metric: 100, NeighborAddr: "172.16.0.0", AdjSIDs: []uint32{16002}},
				{SystemID: "0000.0000.0003", Metric: 200, NeighborAddr: "172.16.0.3", AdjSIDs: []uint32{16003}},
			},
		},
		{
			SystemID: "0000.0000.0003.00-00",
			Hostname: "DZ-DC1-SW01",
			RouterID: "10.0.0.3",
			Neighbors: []isis.Neighbor{
				{SystemID: "0000.0000.0002", Metric: 200, NeighborAddr: "172.16.0.2", AdjSIDs: []uint32{16004}},
			},
		},
	}
	err = graphStore.SyncISIS(ctx, lsps)
	require.NoError(t, err)
}

func TestStore_ISISAdjacencies(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	setupISISTestData(t, chClient, graphStore)

	// Test getting adjacencies for device1 (should have 1 neighbor: device2)
	adjacencies, err := graphStore.ISISAdjacencies(ctx, "device1")
	require.NoError(t, err)
	require.Len(t, adjacencies, 1, "device1 should have 1 ISIS adjacency")
	require.Equal(t, "device1", adjacencies[0].FromDevicePK)
	require.Equal(t, "device2", adjacencies[0].ToDevicePK)
	require.Equal(t, uint32(100), adjacencies[0].Metric)
	require.Equal(t, "172.16.0.1", adjacencies[0].NeighborAddr)

	// Test getting adjacencies for device2 (should have 2 neighbors: device1 and device3)
	adjacencies, err = graphStore.ISISAdjacencies(ctx, "device2")
	require.NoError(t, err)
	require.Len(t, adjacencies, 2, "device2 should have 2 ISIS adjacencies")

	// Test getting adjacencies for device3 (should have 1 neighbor: device2)
	adjacencies, err = graphStore.ISISAdjacencies(ctx, "device3")
	require.NoError(t, err)
	require.Len(t, adjacencies, 1, "device3 should have 1 ISIS adjacency")
	require.Equal(t, "device3", adjacencies[0].FromDevicePK)
	require.Equal(t, "device2", adjacencies[0].ToDevicePK)
	require.Equal(t, uint32(200), adjacencies[0].Metric)
}

func TestStore_ISISTopology(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	setupISISTestData(t, chClient, graphStore)

	devices, adjacencies, err := graphStore.ISISTopology(ctx)
	require.NoError(t, err)

	// Should have 3 devices with ISIS data
	require.Len(t, devices, 3, "expected 3 devices with ISIS data")

	// Verify device properties
	deviceMap := make(map[string]ISISDevice)
	for _, d := range devices {
		deviceMap[d.PK] = d
	}
	require.Contains(t, deviceMap, "device1")
	require.Equal(t, "0000.0000.0001.00-00", deviceMap["device1"].SystemID)
	require.Equal(t, "10.0.0.1", deviceMap["device1"].RouterID)

	// Should have 4 adjacencies total (bidirectional links)
	// device1->device2, device2->device1, device2->device3, device3->device2
	require.Len(t, adjacencies, 4, "expected 4 ISIS adjacencies (bidirectional)")
}

func TestStore_ShortestPathByISISMetric(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	setupISISTestData(t, chClient, graphStore)

	// Find path from device1 to device3
	devices, totalMetric, err := graphStore.ShortestPathByISISMetric(ctx, "device1", "device3")
	require.NoError(t, err)

	// Path should be: device1 -> device2 -> device3
	require.Len(t, devices, 3, "expected 3 devices in path")
	require.Equal(t, "device1", devices[0].PK)
	require.Equal(t, "device2", devices[1].PK)
	require.Equal(t, "device3", devices[2].PK)

	// Total metric: device1->device2 (100) + device2->device3 (200) = 300
	require.Equal(t, uint32(300), totalMetric, "expected total metric of 300")

	// Verify ISIS properties are included
	require.Equal(t, "0000.0000.0001.00-00", devices[0].SystemID)
	require.Equal(t, "10.0.0.1", devices[0].RouterID)
}

func TestStore_LinksWithISISMetrics(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	setupISISTestData(t, chClient, graphStore)

	links, err := graphStore.LinksWithISISMetrics(ctx)
	require.NoError(t, err)

	// Should have 2 links with ISIS metrics
	require.Len(t, links, 2, "expected 2 links with ISIS metrics")

	// Create a map for easier verification
	linkMap := make(map[string]ISISLink)
	for _, l := range links {
		linkMap[l.PK] = l
	}

	// Verify link1 has ISIS metric
	require.Contains(t, linkMap, "link1")
	require.Equal(t, uint32(100), linkMap["link1"].ISISMetric)

	// Verify link2 has ISIS metric
	require.Contains(t, linkMap, "link2")
	require.Equal(t, uint32(200), linkMap["link2"].ISISMetric)
}

func TestStore_CompareTopology(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	setupISISTestData(t, chClient, graphStore)

	comparison, err := graphStore.CompareTopology(ctx)
	require.NoError(t, err)
	require.NotNil(t, comparison)

	// Should have 2 configured links
	require.Equal(t, 2, comparison.ConfiguredLinks, "expected 2 configured links")

	// Should have 4 ISIS adjacencies (bidirectional on 2 links)
	require.Equal(t, 4, comparison.ISISAdjacencies, "expected 4 ISIS adjacencies")

	// Should have 2 matched links (both links have ISIS adjacencies)
	require.Equal(t, 2, comparison.MatchedLinks, "expected 2 matched links")

	// Test data has metric mismatches:
	// - link1: configured RTT 1000µs, ISIS metric 100µs (10x difference)
	// - link2: configured RTT 2000µs, ISIS metric 200µs (10x difference)
	// Both exceed the 50% threshold, so expect 2 metric_mismatch discrepancies
	require.Len(t, comparison.Discrepancies, 2, "expected 2 metric mismatch discrepancies")
	for _, d := range comparison.Discrepancies {
		require.Equal(t, "metric_mismatch", d.Type)
	}
}

func TestStore_CompareTopology_MissingISIS(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Clear and setup base data
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create minimal data with a link but NO ISIS data
	contributors := []dzsvc.Contributor{{PK: "contrib1", Code: "test1", Name: "Test"}}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	metros := []dzsvc.Metro{{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128}}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "SW01", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "SW02", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	// Create an active link
	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "172.16.0.0/31", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Sync only serviceability data (no ISIS)
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Compare topology - should detect missing ISIS adjacency
	comparison, err := graphStore.CompareTopology(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, comparison.ConfiguredLinks, "expected 1 configured link")
	require.Equal(t, 0, comparison.ISISAdjacencies, "expected 0 ISIS adjacencies")
	require.Equal(t, 0, comparison.MatchedLinks, "expected 0 matched links")

	// Should have 1 discrepancy: missing ISIS on active link
	require.Len(t, comparison.Discrepancies, 1, "expected 1 discrepancy")
	require.Equal(t, "missing_isis", comparison.Discrepancies[0].Type)
	require.Equal(t, "link1", comparison.Discrepancies[0].LinkPK)
}
