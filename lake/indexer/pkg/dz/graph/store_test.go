package graph

import (
	"net"
	"testing"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	clickhousetesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/testing"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	neo4jtesting "github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j/testing"
	laketesting "github.com/malbeclabs/doublezero/lake/utils/pkg/testing"
	"github.com/stretchr/testify/require"
)

// clearTestData removes all data from the serviceability dimension tables using direct SQL
func clearTestData(t *testing.T, chClient clickhouse.Client) {
	ctx := t.Context()
	// Use sync context to ensure truncations are visible before returning
	syncCtx := clickhouse.ContextWithSyncInsert(ctx)

	conn, err := chClient.Conn(syncCtx)
	require.NoError(t, err)
	defer conn.Close()

	// Truncate staging and history tables to clear data between tests
	// Table names follow the DimensionType2Dataset naming convention:
	// - Base table: dim_<schema_name>
	// - Staging: stg_<base>_snapshot = stg_dim_<schema_name>_snapshot
	// - History: <base>_history = dim_<schema_name>_history
	tables := []string{
		// Staging tables
		"stg_dim_dz_users_snapshot",
		"stg_dim_dz_links_snapshot",
		"stg_dim_dz_devices_snapshot",
		"stg_dim_dz_metros_snapshot",
		"stg_dim_dz_contributors_snapshot",
		// History tables
		"dim_dz_users_history",
		"dim_dz_links_history",
		"dim_dz_devices_history",
		"dim_dz_metros_history",
		"dim_dz_contributors_history",
	}

	for _, table := range tables {
		err := conn.Exec(syncCtx, "TRUNCATE TABLE IF EXISTS "+table)
		require.NoError(t, err, "failed to truncate %s", table)
	}
}

func setupTestData(t *testing.T, chClient clickhouse.Client) {
	ctx := t.Context()
	log := laketesting.NewLogger()

	// First clear any existing data from previous tests
	clearTestData(t, chClient)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create test contributors
	contributors := []dzsvc.Contributor{
		{PK: "contrib1", Code: "test1", Name: "Test Contributor 1"},
		{PK: "contrib2", Code: "test2", Name: "Test Contributor 2"},
	}
	err = store.ReplaceContributors(ctx, contributors)
	require.NoError(t, err)

	// Create test metros
	metros := []dzsvc.Metro{
		{PK: "metro1", Code: "NYC", Name: "New York", Longitude: -74.006, Latitude: 40.7128},
		{PK: "metro2", Code: "LAX", Name: "Los Angeles", Longitude: -118.2437, Latitude: 34.0522},
	}
	err = store.ReplaceMetros(ctx, metros)
	require.NoError(t, err)

	// Create test devices
	devices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "dev1", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "dev3", PublicIP: "1.2.3.6", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, devices)
	require.NoError(t, err)

	// Create test links connecting devices
	links := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "10.0.0.0/30", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000, ISISDelayOverrideNs: 0},
		{PK: "link2", Status: "active", Code: "link2", TunnelNet: "10.0.0.4/30", ContributorPK: "contrib1", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 2000000, CommittedJitterNs: 200000, Bandwidth: 10000000000, ISISDelayOverrideNs: 0},
	}
	err = store.ReplaceLinks(ctx, links)
	require.NoError(t, err)

	// Create test users
	users := []dzsvc.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "active", Kind: "client", ClientIP: net.ParseIP("192.168.1.1"), DZIP: net.ParseIP("10.0.1.1"), DevicePK: "device1", TunnelID: 1},
	}
	err = store.ReplaceUsers(ctx, users)
	require.NoError(t, err)
}

func TestStore_Sync(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()

	// Setup test data in ClickHouse
	setupTestData(t, chClient)

	// Create graph store
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Sync to Neo4j
	ctx := t.Context()
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify nodes were created
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Check device count
	res, err := session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(3), count)

	// Check link count
	res, err = session.Run(ctx, "MATCH (l:Link) RETURN count(l) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count)

	// Check metro count
	res, err = session.Run(ctx, "MATCH (m:Metro) RETURN count(m) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count)

	// Check user count
	res, err = session.Run(ctx, "MATCH (u:User) RETURN count(u) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(1), count)

	// Check contributor count
	res, err = session.Run(ctx, "MATCH (c:Contributor) RETURN count(c) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count)
}

func TestStore_Sync_Idempotent(t *testing.T) {
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

	// Sync twice
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify counts are still correct after second sync
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// Check device count
	res, err := session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(3), count, "expected 3 devices after idempotent sync")

	// Check link count
	res, err = session.Run(ctx, "MATCH (l:Link) RETURN count(l) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count, "expected 2 links after idempotent sync")
}

func TestStore_Sync_UpdatesProperties(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup initial test data
	setupTestData(t, chClient)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial device status
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	res, err := session.Run(ctx, "MATCH (d:Device {pk: 'device1'}) RETURN d.status AS status, d.code AS code", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	status, _ := record.Get("status")
	code, _ := record.Get("code")
	require.Equal(t, "active", status)
	require.Equal(t, "dev1", code)

	// Update device in ClickHouse - change status and code
	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	updatedDevices := []dzsvc.Device{
		{PK: "device1", Status: "inactive", DeviceType: "router", Code: "dev1-updated", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 200},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "dev3", PublicIP: "1.2.3.6", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, updatedDevices)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify device properties were updated
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'}) RETURN d.status AS status, d.code AS code, d.max_users AS max_users", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	status, _ = record.Get("status")
	code, _ = record.Get("code")
	maxUsers, _ := record.Get("max_users")
	require.Equal(t, "inactive", status, "expected status to be updated to 'inactive'")
	require.Equal(t, "dev1-updated", code, "expected code to be updated")
	require.Equal(t, int64(200), maxUsers, "expected max_users to be updated to 200")
}

func TestStore_Sync_AddsNewNodes(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup initial test data
	setupTestData(t, chClient)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial counts
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	res, err := session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(3), count)

	// Add new device and link in ClickHouse
	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Add a new device
	newDevices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "dev1", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "dev3", PublicIP: "1.2.3.6", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 100},
		{PK: "device4", Status: "active", DeviceType: "switch", Code: "dev4", PublicIP: "1.2.3.7", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 50},
	}
	err = store.ReplaceDevices(ctx, newDevices)
	require.NoError(t, err)

	// Add a new link connecting device3 to device4
	newLinks := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "10.0.0.0/30", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
		{PK: "link2", Status: "active", Code: "link2", TunnelNet: "10.0.0.4/30", ContributorPK: "contrib1", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 2000000, CommittedJitterNs: 200000, Bandwidth: 10000000000},
		{PK: "link3", Status: "active", Code: "link3", TunnelNet: "10.0.0.8/30", ContributorPK: "contrib2", SideAPK: "device3", SideZPK: "device4", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 500000, CommittedJitterNs: 50000, Bandwidth: 1000000000},
	}
	err = store.ReplaceLinks(ctx, newLinks)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify new device was added
	res, err = session.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(4), count, "expected 4 devices after adding new one")

	// Verify new link was added
	res, err = session.Run(ctx, "MATCH (l:Link) RETURN count(l) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(3), count, "expected 3 links after adding new one")

	// Verify new device has correct properties and relationships
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device4'})-[:LOCATED_IN]->(m:Metro) RETURN d.code AS code, d.device_type AS device_type, m.code AS metro_code", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	code, _ := record.Get("code")
	deviceType, _ := record.Get("device_type")
	metroCode, _ := record.Get("metro_code")
	require.Equal(t, "dev4", code)
	require.Equal(t, "switch", deviceType)
	require.Equal(t, "LAX", metroCode)

	// Verify new link connects correct devices
	res, err = session.Run(ctx, "MATCH (l:Link {pk: 'link3'})-[:CONNECTS]->(d:Device) RETURN d.pk AS pk ORDER BY d.pk", nil)
	require.NoError(t, err)
	var connectedDevices []string
	for res.Next(ctx) {
		record := res.Record()
		pk, _ := record.Get("pk")
		connectedDevices = append(connectedDevices, pk.(string))
	}
	require.NoError(t, res.Err())
	require.ElementsMatch(t, []string{"device3", "device4"}, connectedDevices, "expected link3 to connect device3 and device4")
}

func TestStore_Sync_RemovesDeletedNodes(t *testing.T) {
	// Use dedicated containers for this test to avoid shared state issues
	log := laketesting.NewLogger()
	ctx := t.Context()

	dedicatedClickHouse, err := clickhousetesting.NewDB(ctx, log, nil)
	require.NoError(t, err)
	t.Cleanup(func() { dedicatedClickHouse.Close() })

	dedicatedNeo4j, err := neo4jtesting.NewDB(ctx, log, nil)
	require.NoError(t, err)
	t.Cleanup(func() { dedicatedNeo4j.Close() })

	chClient := laketesting.NewClient(t, dedicatedClickHouse)
	neo4jClient, err := neo4jtesting.NewTestClient(t, dedicatedNeo4j)
	require.NoError(t, err)

	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	// Create contributors and metros first
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

	// Create 4 devices initially
	initialDevices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "dev1", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "dev3", PublicIP: "1.2.3.6", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device4", Status: "active", DeviceType: "router", Code: "dev4", PublicIP: "1.2.3.7", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, initialDevices)
	require.NoError(t, err)

	// Create 3 links initially
	initialLinks := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "10.0.0.0/30", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
		{PK: "link2", Status: "active", Code: "link2", TunnelNet: "10.0.0.4/30", ContributorPK: "contrib1", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 2000000, CommittedJitterNs: 200000, Bandwidth: 10000000000},
		{PK: "link3", Status: "active", Code: "link3", TunnelNet: "10.0.0.8/30", ContributorPK: "contrib1", SideAPK: "device3", SideZPK: "device4", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 500000, CommittedJitterNs: 50000, Bandwidth: 1000000000},
	}
	err = store.ReplaceLinks(ctx, initialLinks)
	require.NoError(t, err)

	// Create 2 users initially
	initialUsers := []dzsvc.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "active", Kind: "client", ClientIP: net.ParseIP("192.168.1.1"), DZIP: net.ParseIP("10.0.1.1"), DevicePK: "device1", TunnelID: 1},
		{PK: "user2", OwnerPubkey: "owner2", Status: "active", Kind: "client", ClientIP: net.ParseIP("192.168.1.2"), DZIP: net.ParseIP("10.0.1.2"), DevicePK: "device2", TunnelID: 2},
	}
	err = store.ReplaceUsers(ctx, initialUsers)
	require.NoError(t, err)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial counts using fresh session
	session1, err := neo4jClient.Session(ctx)
	require.NoError(t, err)

	res, err := session1.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	count, _ := record.Get("count")
	require.Equal(t, int64(4), count, "expected 4 devices initially")

	res, err = session1.Run(ctx, "MATCH (l:Link) RETURN count(l) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(3), count, "expected 3 links initially")

	res, err = session1.Run(ctx, "MATCH (u:User) RETURN count(u) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count, "expected 2 users initially")
	session1.Close(ctx)

	// Now remove device3, device4, link2, link3, and user2 by replacing with reduced lists
	remainingDevices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "dev1", PublicIP: "1.2.3.4", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, remainingDevices)
	require.NoError(t, err)

	remainingLinks := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "10.0.0.0/30", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, remainingLinks)
	require.NoError(t, err)

	remainingUsers := []dzsvc.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "active", Kind: "client", ClientIP: net.ParseIP("192.168.1.1"), DZIP: net.ParseIP("10.0.1.1"), DevicePK: "device1", TunnelID: 1},
	}
	err = store.ReplaceUsers(ctx, remainingUsers)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify using fresh session after sync
	session2, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session2.Close(ctx)

	// Verify device count reduced from 4 to 2
	res, err = session2.Run(ctx, "MATCH (d:Device) RETURN count(d) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(2), count, "expected 2 devices after removing device3 and device4")

	// Verify specific devices are gone
	res, err = session2.Run(ctx, "MATCH (d:Device {pk: 'device3'}) RETURN d", nil)
	require.NoError(t, err)
	require.False(t, res.Next(ctx), "expected device3 to be removed")

	res, err = session2.Run(ctx, "MATCH (d:Device {pk: 'device4'}) RETURN d", nil)
	require.NoError(t, err)
	require.False(t, res.Next(ctx), "expected device4 to be removed")

	// Verify link count reduced from 3 to 1
	res, err = session2.Run(ctx, "MATCH (l:Link) RETURN count(l) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(1), count, "expected 1 link after removing link2 and link3")

	// Verify specific links are gone
	res, err = session2.Run(ctx, "MATCH (l:Link {pk: 'link2'}) RETURN l", nil)
	require.NoError(t, err)
	require.False(t, res.Next(ctx), "expected link2 to be removed")

	res, err = session2.Run(ctx, "MATCH (l:Link {pk: 'link3'}) RETURN l", nil)
	require.NoError(t, err)
	require.False(t, res.Next(ctx), "expected link3 to be removed")

	// Verify user count reduced from 2 to 1
	res, err = session2.Run(ctx, "MATCH (u:User) RETURN count(u) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	count, _ = record.Get("count")
	require.Equal(t, int64(1), count, "expected 1 user after removing user2")

	// Verify specific user is gone
	res, err = session2.Run(ctx, "MATCH (u:User {pk: 'user2'}) RETURN u", nil)
	require.NoError(t, err)
	require.False(t, res.Next(ctx), "expected user2 to be removed")
}

func TestStore_Sync_UpdatesRelationships(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup initial test data
	setupTestData(t, chClient)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial relationships
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// device1 should be in metro1 (NYC)
	res, err := session.Run(ctx, "MATCH (d:Device {pk: 'device1'})-[:LOCATED_IN]->(m:Metro) RETURN m.code AS metro_code", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	metroCode, _ := record.Get("metro_code")
	require.Equal(t, "NYC", metroCode)

	// device1 should be operated by contrib1
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'})-[:OPERATES]->(c:Contributor) RETURN c.code AS contrib_code", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	contribCode, _ := record.Get("contrib_code")
	require.Equal(t, "test1", contribCode)

	// Move device1 to metro2 and change contributor to contrib2
	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	updatedDevices := []dzsvc.Device{
		{PK: "device1", Status: "active", DeviceType: "router", Code: "dev1", PublicIP: "1.2.3.4", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 100}, // Changed metro and contributor
		{PK: "device2", Status: "active", DeviceType: "router", Code: "dev2", PublicIP: "1.2.3.5", ContributorPK: "contrib1", MetroPK: "metro1", MaxUsers: 100},
		{PK: "device3", Status: "active", DeviceType: "router", Code: "dev3", PublicIP: "1.2.3.6", ContributorPK: "contrib2", MetroPK: "metro2", MaxUsers: 100},
	}
	err = store.ReplaceDevices(ctx, updatedDevices)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify device1 is now in metro2 (LAX)
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'})-[:LOCATED_IN]->(m:Metro) RETURN m.code AS metro_code", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	metroCode, _ = record.Get("metro_code")
	require.Equal(t, "LAX", metroCode, "expected device1 to be moved to LAX")

	// Verify device1 is now operated by contrib2
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'})-[:OPERATES]->(c:Contributor) RETURN c.code AS contrib_code", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	contribCode, _ = record.Get("contrib_code")
	require.Equal(t, "test2", contribCode, "expected device1 to be operated by contrib2")

	// Verify there's only one LOCATED_IN relationship for device1 (no stale relationships)
	res, err = session.Run(ctx, "MATCH (d:Device {pk: 'device1'})-[r:LOCATED_IN]->(m:Metro) RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	relCount, _ := record.Get("count")
	require.Equal(t, int64(1), relCount, "expected only one LOCATED_IN relationship")
}

func TestStore_Sync_UpdatesLinkConnections(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup initial test data
	setupTestData(t, chClient)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial link connections
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// link1 should connect device1 and device2
	res, err := session.Run(ctx, "MATCH (l:Link {pk: 'link1'})-[:CONNECTS]->(d:Device) RETURN d.pk AS pk ORDER BY d.pk", nil)
	require.NoError(t, err)
	var connectedDevices []string
	for res.Next(ctx) {
		record := res.Record()
		pk, _ := record.Get("pk")
		connectedDevices = append(connectedDevices, pk.(string))
	}
	require.NoError(t, res.Err())
	require.ElementsMatch(t, []string{"device1", "device2"}, connectedDevices)

	// Change link1 to connect device1 and device3 instead
	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	updatedLinks := []dzsvc.Link{
		{PK: "link1", Status: "active", Code: "link1", TunnelNet: "10.0.0.0/30", ContributorPK: "contrib1", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "eth0", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 1000000, CommittedJitterNs: 100000, Bandwidth: 10000000000}, // Changed SideZPK
		{PK: "link2", Status: "active", Code: "link2", TunnelNet: "10.0.0.4/30", ContributorPK: "contrib1", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "eth1", SideZIfaceName: "eth0", LinkType: "direct", CommittedRTTNs: 2000000, CommittedJitterNs: 200000, Bandwidth: 10000000000},
	}
	err = store.ReplaceLinks(ctx, updatedLinks)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify link1 now connects device1 and device3
	res, err = session.Run(ctx, "MATCH (l:Link {pk: 'link1'})-[:CONNECTS]->(d:Device) RETURN d.pk AS pk ORDER BY d.pk", nil)
	require.NoError(t, err)
	connectedDevices = nil
	for res.Next(ctx) {
		record := res.Record()
		pk, _ := record.Get("pk")
		connectedDevices = append(connectedDevices, pk.(string))
	}
	require.NoError(t, res.Err())
	require.ElementsMatch(t, []string{"device1", "device3"}, connectedDevices, "expected link1 to connect device1 and device3")

	// Verify there are exactly 2 CONNECTS relationships for link1
	res, err = session.Run(ctx, "MATCH (l:Link {pk: 'link1'})-[r:CONNECTS]->(d:Device) RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	relCount, _ := record.Get("count")
	require.Equal(t, int64(2), relCount, "expected exactly 2 CONNECTS relationships for link1")
}

func TestStore_Sync_UserDeviceAssignment(t *testing.T) {
	chClient := testClickHouseClient(t)
	neo4jClient := testNeo4jClient(t)
	log := laketesting.NewLogger()
	ctx := t.Context()

	// Setup initial test data
	setupTestData(t, chClient)

	// Create graph store and initial sync
	graphStore, err := NewStore(StoreConfig{
		Logger:     log,
		Neo4j:      neo4jClient,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify initial user assignment
	session, err := neo4jClient.Session(ctx)
	require.NoError(t, err)
	defer session.Close(ctx)

	// user1 should be assigned to device1
	res, err := session.Run(ctx, "MATCH (u:User {pk: 'user1'})-[:ASSIGNED_TO]->(d:Device) RETURN d.pk AS device_pk", nil)
	require.NoError(t, err)
	record, err := res.Single(ctx)
	require.NoError(t, err)
	devicePK, _ := record.Get("device_pk")
	require.Equal(t, "device1", devicePK)

	// Reassign user1 to device2
	store, err := dzsvc.NewStore(dzsvc.StoreConfig{
		Logger:     log,
		ClickHouse: chClient,
	})
	require.NoError(t, err)

	updatedUsers := []dzsvc.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "active", Kind: "client", ClientIP: net.ParseIP("192.168.1.1"), DZIP: net.ParseIP("10.0.1.1"), DevicePK: "device2", TunnelID: 1}, // Changed DevicePK
	}
	err = store.ReplaceUsers(ctx, updatedUsers)
	require.NoError(t, err)

	// Sync again
	err = graphStore.Sync(ctx)
	require.NoError(t, err)

	// Verify user1 is now assigned to device2
	res, err = session.Run(ctx, "MATCH (u:User {pk: 'user1'})-[:ASSIGNED_TO]->(d:Device) RETURN d.pk AS device_pk", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	devicePK, _ = record.Get("device_pk")
	require.Equal(t, "device2", devicePK, "expected user1 to be assigned to device2")

	// Verify there's only one ASSIGNED_TO relationship
	res, err = session.Run(ctx, "MATCH (u:User {pk: 'user1'})-[r:ASSIGNED_TO]->(d:Device) RETURN count(r) AS count", nil)
	require.NoError(t, err)
	record, err = res.Single(ctx)
	require.NoError(t, err)
	relCount, _ := record.Get("count")
	require.Equal(t, int64(1), relCount, "expected only one ASSIGNED_TO relationship")
}
