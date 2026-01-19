package graph

import (
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/isis"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

func TestStore_SyncISIS(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Clear any existing data
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create test contributors
	contributors := []dzsvc.Contributor{
		{PK: "contrib1", Code: "test1", Name: "Test Contributor 1"},
	}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	// Create test metros
	metros := []dzsvc.Metro{
		{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128},
	}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	// Create test devices
	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "DZ-NY7-SW01", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "DZ-DC1-SW01", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	// Create test link with /31 tunnel_net (required for ISIS correlation)
	// tunnel_net "172.16.0.116/31" contains IPs 172.16.0.116 (side_a) and 172.16.0.117 (side_z)
	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "172.16.0.116/31", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000, ISISDelayOverrideNs: 0},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	// Create graph store and sync initial data
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Now sync ISIS data
	// LSP from device1 shows neighbor with IP 172.16.0.117 (which is device2's IP on this link)
	lsps := []isis.LSP{
		{
			SystemID: "ac10.0001.0000.00-00",
			Hostname: "DZ-NY7-SW01",
			RouterID: "172.16.0.1",
			Neighbors: []isis.Neighbor{
				{
					SystemID:     "ac10.0002.0000",
					Metric:       1000, // 1000 microseconds = 1ms
					NeighborAddr: "172.16.0.117",
					AdjSIDs:      []uint32{100001, 100002},
				},
			},
		},
	}

	err = graphStore.SyncISIS(ctx, lsps)
	require.NoError(t, err)

	// Verify ISIS data was synced
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Check that Link was updated with ISIS metric
	res, err := session.Run(ctx, "MATCH (l:Link {pk: 'link1'}) RETURN l.isis_metric AS metric, l.isis_adj_sids AS adj_sids", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	metric, _ := record.Get("metric")
	adjSids, _ := record.Get("adj_sids")
	require.Equal(t, int64(1000), metric, "expected ISIS metric to be 1000")
	require.NotNil(t, adjSids, "expected adj_sids to be set")

	// Check that Device was updated with ISIS properties
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'}) RETURN d.isis_system_id AS system_id, d.isis_router_id AS router_id", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	systemID, _ := record.Get("system_id")
	routerID, _ := record.Get("router_id")
	require.Equal(t, "ac10.0001.0000.00-00", systemID, "expected ISIS system_id")
	require.Equal(t, "172.16.0.1", routerID, "expected ISIS router_id")

	// Check that ISIS_ADJACENT relationship was created
	res, err = session.Run(ctx, "MATCH (d1:Device {pk: 'device1'})-[r:ISIS_ADJACENT]->(d2:Device {pk: 'device2'}) RETURN r.metric AS metric, r.neighbor_addr AS neighbor_addr", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	metric, _ = record.Get("metric")
	neighborAddr, _ := record.Get("neighbor_addr")
	require.Equal(t, int64(1000), metric, "expected ISIS_ADJACENT metric to be 1000")
	require.Equal(t, "172.16.0.117", neighborAddr, "expected neighbor_addr to be 172.16.0.117")
}

func TestStore_SyncISIS_NoMatchingLink(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup test data with links that won't match the ISIS neighbor addr
	setupTestData(t, chClient)

	// Create graph store and sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Sync ISIS data with neighbor_addr that doesn't match any tunnel_net
	lsps := []isis.LSP{
		{
			SystemID: "ac10.0001.0000.00-00",
			Hostname: "DZ-NY7-SW01",
			RouterID: "172.16.0.1",
			Neighbors: []isis.Neighbor{
				{
					SystemID:     "ac10.0002.0000",
					Metric:       1000,
					NeighborAddr: "192.168.99.99", // This IP won't match any tunnel_net
					AdjSIDs:      []uint32{100001},
				},
			},
		},
	}

	// Should not error, just log unmatched neighbors
	err = graphStore.SyncISIS(ctx, lsps)
	require.NoError(t, err)

	// Verify no ISIS_ADJACENT relationships were created
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	res, err := session.Run(ctx, "MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(0), count, "expected no ISIS_ADJACENT relationships for unmatched neighbor")
}

func TestStore_SyncISIS_EmptyLSPs(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup test data
	setupTestData(t, chClient)

	// Create graph store and sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Sync empty LSPs
	err = graphStore.SyncISIS(ctx, nil)
	require.NoError(t, err)

	err = graphStore.SyncISIS(ctx, []isis.LSP{})
	require.NoError(t, err)
}

// TestStore_SyncWithISIS tests that graph and ISIS data are synced atomically.
// This ensures there is never a moment where the graph has nodes but no ISIS relationships.
func TestStore_SyncWithISIS(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Clear any existing data
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create test contributors
	contributors := []dzsvc.Contributor{
		{PK: "contrib1", Code: "test1", Name: "Test Contributor 1"},
	}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	// Create test metros
	metros := []dzsvc.Metro{
		{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128},
		{PK: "metro2", Code: "DC", Name: "Washington DC", Longitude: -77.0369, Latitude: 38.9072},
	}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	// Create test devices
	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "DZ-NY7-SW01", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "DZ-DC1-SW01", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro2", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	// Create test link with /31 tunnel_net
	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "172.16.0.116/31", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	// Create graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create ISIS LSPs
	lsps := []isis.LSP{
		{
			SystemID: "ac10.0001.0000.00-00",
			Hostname: "DZ-NY7-SW01",
			RouterID: "172.16.0.1",
			Neighbors: []isis.Neighbor{
				{
					SystemID:     "ac10.0002.0000",
					Metric:       1000,
					NeighborAddr: "172.16.0.117",
					AdjSIDs:      []uint32{100001},
				},
			},
		},
	}

	// Sync graph with ISIS atomically
	err = graphStore.SyncWithISIS(ctx, lsps)
	require.NoError(t, err)

	// Verify everything was synced together
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Check devices exist
	res, err := session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(2), count, "expected 2 devices")

	// Check ISIS_ADJACENT relationship exists
	res, err = session.Run(ctx, "MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(1), count, "expected 1 ISIS_ADJACENT relationship")

	// Check Device has ISIS properties
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'}) RETURN d.isis_system_id AS system_id", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	systemID, _ := record.Get("system_id")
	require.Equal(t, "ac10.0001.0000.00-00", systemID, "expected ISIS system_id")

	// Check Link has ISIS metric
	res, err = session.Run(ctx, "MATCH (l:Link {pk: 'link1'}) RETURN l.isis_metric AS metric", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	metric, _ := record.Get("metric")
	require.Equal(t, int64(1000), metric, "expected ISIS metric")
}

// TestStore_SyncWithISIS_EmptyLSPs tests that SyncWithISIS works with no ISIS data.
func TestStore_SyncWithISIS_EmptyLSPs(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup test data
	setupTestData(t, chClient)

	// Create graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Sync with empty LSPs - should still sync base graph
	err = graphStore.SyncWithISIS(ctx, []isis.LSP{})
	require.NoError(t, err)

	// Verify base graph was synced
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	res, err := session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Greater(t, count.(int64), int64(0), "expected devices to be synced")
}

// TestStore_SyncWithISIS_ReplacesExistingData tests that SyncWithISIS replaces existing data atomically.
func TestStore_SyncWithISIS_ReplacesExistingData(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Clear any existing data
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create initial test data
	contributors := []dzsvc.Contributor{
		{PK: "contrib1", Code: "test1", Name: "Test Contributor 1"},
	}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	metros := []dzsvc.Metro{
		{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128},
	}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "DZ-NY7-SW01", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "DZ-DC1-SW01", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "172.16.0.116/31", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	// Create graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// First sync with ISIS data
	lsps1 := []isis.LSP{
		{
			SystemID: "ac10.0001.0000.00-00",
			Hostname: "DZ-NY7-SW01",
			RouterID: "172.16.0.1",
			Neighbors: []isis.Neighbor{
				{SystemID: "ac10.0002.0000", Metric: 1000, NeighborAddr: "172.16.0.117", AdjSIDs: []uint32{100001}},
			},
		},
	}
	err = graphStore.SyncWithISIS(ctx, lsps1)
	require.NoError(t, err)

	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Verify first sync
	res, err := session.Run(ctx, "MATCH (l:Link {pk: 'link1'}) RETURN l.isis_metric AS metric", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	metric, _ := record.Get("metric")
	require.Equal(t, int64(1000), metric, "expected ISIS metric 1000 after first sync")

	// Second sync with different ISIS data
	lsps2 := []isis.LSP{
		{
			SystemID: "ac10.0001.0000.00-00",
			Hostname: "DZ-NY7-SW01",
			RouterID: "172.16.0.1",
			Neighbors: []isis.Neighbor{
				{SystemID: "ac10.0002.0000", Metric: 2000, NeighborAddr: "172.16.0.117", AdjSIDs: []uint32{100001}}, // Different metric
			},
		},
	}
	err = graphStore.SyncWithISIS(ctx, lsps2)
	require.NoError(t, err)

	// Verify second sync replaced the data
	res, err = session.Run(ctx, "MATCH (l:Link {pk: 'link1'}) RETURN l.isis_metric AS metric", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	metric, _ = record.Get("metric")
	require.Equal(t, int64(2000), metric, "expected ISIS metric 2000 after second sync")

	// Verify ISIS_ADJACENT still exists
	res, err = session.Run(ctx, "MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(1), count, "expected 1 ISIS_ADJACENT relationship after resync")
}

func TestParseTunnelNet31(t *testing.T) {
	t.Run("valid /31", func(t *testing.T) {
		ip1, ip2, err := parseTunnelNet31("172.16.0.116/31")
		require.NoError(t, err)
		require.Equal(t, "172.16.0.116", ip1)
		require.Equal(t, "172.16.0.117", ip2)
	})

	t.Run("valid /31 at boundary", func(t *testing.T) {
		ip1, ip2, err := parseTunnelNet31("10.0.0.0/31")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.0", ip1)
		require.Equal(t, "10.0.0.1", ip2)
	})

	t.Run("invalid CIDR", func(t *testing.T) {
		_, _, err := parseTunnelNet31("not-a-cidr")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid CIDR")
	})

	t.Run("wrong prefix length /30", func(t *testing.T) {
		_, _, err := parseTunnelNet31("172.16.0.116/30")
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected /31")
	})

	t.Run("wrong prefix length /32", func(t *testing.T) {
		_, _, err := parseTunnelNet31("172.16.0.116/32")
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected /31")
	})
}
