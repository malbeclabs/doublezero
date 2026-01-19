package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/isis"
	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

// StoreConfig holds configuration for the Store.
type StoreConfig struct {
	Logger     *slog.Logger
	Neo4j      neo4j.Client
	ClickHouse clickhouse.Client
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.Neo4j == nil {
		return errors.New("neo4j client is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse client is required")
	}
	return nil
}

// Store manages the Neo4j graph representation of the network topology.
// It syncs data from ClickHouse (source of truth) to Neo4j for graph algorithms.
type Store struct {
	log *slog.Logger
	cfg StoreConfig
}

// NewStore creates a new Store.
func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

// Sync reads current state from ClickHouse and replaces the Neo4j graph.
// This performs a full sync atomically within a single transaction.
// Readers see either the old state or the new state, never an empty/partial state.
func (s *Store) Sync(ctx context.Context) error {
	s.log.Debug("graph: starting sync")

	// Read current data from ClickHouse
	devices, err := dzsvc.QueryCurrentDevices(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query devices: %w", err)
	}

	links, err := dzsvc.QueryCurrentLinks(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query links: %w", err)
	}

	metros, err := dzsvc.QueryCurrentMetros(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query metros: %w", err)
	}

	users, err := dzsvc.QueryCurrentUsers(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query users: %w", err)
	}

	contributors, err := dzsvc.QueryCurrentContributors(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query contributors: %w", err)
	}

	s.log.Debug("graph: fetched data from ClickHouse",
		"devices", len(devices),
		"links", len(links),
		"metros", len(metros),
		"users", len(users),
		"contributors", len(contributors))

	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close(ctx)

	// Perform atomic sync within a single write transaction
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.Transaction) (any, error) {
		// Delete all existing nodes and relationships
		res, err := tx.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to clear graph: %w", err)
		}
		if _, err := res.Consume(ctx); err != nil {
			return nil, fmt.Errorf("failed to consume clear result: %w", err)
		}

		// Create all nodes and relationships using batched UNWIND queries
		if err := batchCreateContributors(ctx, tx, contributors); err != nil {
			return nil, fmt.Errorf("failed to create contributors: %w", err)
		}

		if err := batchCreateMetros(ctx, tx, metros); err != nil {
			return nil, fmt.Errorf("failed to create metros: %w", err)
		}

		if err := batchCreateDevices(ctx, tx, devices); err != nil {
			return nil, fmt.Errorf("failed to create devices: %w", err)
		}

		if err := batchCreateLinks(ctx, tx, links); err != nil {
			return nil, fmt.Errorf("failed to create links: %w", err)
		}

		if err := batchCreateUsers(ctx, tx, users); err != nil {
			return nil, fmt.Errorf("failed to create users: %w", err)
		}

		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("failed to sync graph: %w", err)
	}

	s.log.Info("graph: sync completed",
		"devices", len(devices),
		"links", len(links),
		"metros", len(metros),
		"users", len(users))

	return nil
}

// SyncWithISIS reads current state from ClickHouse and IS-IS data, then replaces the Neo4j graph
// atomically within a single transaction. This ensures there is never a moment where the graph
// has base nodes but no ISIS relationships.
func (s *Store) SyncWithISIS(ctx context.Context, lsps []isis.LSP) error {
	s.log.Debug("graph: starting sync with ISIS", "lsps", len(lsps))

	// Read current data from ClickHouse
	devices, err := dzsvc.QueryCurrentDevices(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query devices: %w", err)
	}

	links, err := dzsvc.QueryCurrentLinks(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query links: %w", err)
	}

	metros, err := dzsvc.QueryCurrentMetros(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query metros: %w", err)
	}

	users, err := dzsvc.QueryCurrentUsers(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query users: %w", err)
	}

	contributors, err := dzsvc.QueryCurrentContributors(ctx, s.log, s.cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("failed to query contributors: %w", err)
	}

	s.log.Debug("graph: fetched data from ClickHouse",
		"devices", len(devices),
		"links", len(links),
		"metros", len(metros),
		"users", len(users),
		"contributors", len(contributors))

	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close(ctx)

	// Perform atomic sync within a single write transaction
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.Transaction) (any, error) {
		// Delete all existing nodes and relationships
		res, err := tx.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to clear graph: %w", err)
		}
		if _, err := res.Consume(ctx); err != nil {
			return nil, fmt.Errorf("failed to consume clear result: %w", err)
		}

		// Create all nodes and relationships using batched UNWIND queries
		if err := batchCreateContributors(ctx, tx, contributors); err != nil {
			return nil, fmt.Errorf("failed to create contributors: %w", err)
		}

		if err := batchCreateMetros(ctx, tx, metros); err != nil {
			return nil, fmt.Errorf("failed to create metros: %w", err)
		}

		if err := batchCreateDevices(ctx, tx, devices); err != nil {
			return nil, fmt.Errorf("failed to create devices: %w", err)
		}

		if err := batchCreateLinks(ctx, tx, links); err != nil {
			return nil, fmt.Errorf("failed to create links: %w", err)
		}

		if err := batchCreateUsers(ctx, tx, users); err != nil {
			return nil, fmt.Errorf("failed to create users: %w", err)
		}

		// Now create ISIS relationships within the same transaction
		if len(lsps) > 0 {
			if err := s.syncISISInTx(ctx, tx, lsps); err != nil {
				return nil, fmt.Errorf("failed to sync ISIS data: %w", err)
			}
		}

		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("failed to sync graph with ISIS: %w", err)
	}

	s.log.Info("graph: sync with ISIS completed",
		"devices", len(devices),
		"links", len(links),
		"metros", len(metros),
		"users", len(users),
		"lsps", len(lsps))

	return nil
}

// syncISISInTx creates ISIS relationships within an existing transaction.
func (s *Store) syncISISInTx(ctx context.Context, tx neo4j.Transaction, lsps []isis.LSP) error {
	// Build tunnel map from the newly created links
	tunnelMap, err := s.buildTunnelMapInTx(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to build tunnel map: %w", err)
	}
	s.log.Debug("graph: built tunnel map", "mappings", len(tunnelMap))

	now := time.Now()
	var adjacenciesCreated, linksUpdated, devicesUpdated, unmatchedNeighbors int

	for _, lsp := range lsps {
		for _, neighbor := range lsp.Neighbors {
			mapping, found := tunnelMap[neighbor.NeighborAddr]
			if !found {
				unmatchedNeighbors++
				continue
			}

			// Update Link with IS-IS metric
			if err := updateLinkISISInTx(ctx, tx, mapping.linkPK, neighbor, now); err != nil {
				s.log.Warn("graph: failed to update link ISIS data",
					"link_pk", mapping.linkPK,
					"error", err)
				continue
			}
			linksUpdated++

			// Update Device with IS-IS properties
			if err := updateDeviceISISInTx(ctx, tx, mapping.localPK, lsp, now); err != nil {
				s.log.Warn("graph: failed to update device ISIS data",
					"device_pk", mapping.localPK,
					"error", err)
			} else {
				devicesUpdated++
			}

			// Create ISIS_ADJACENT relationship
			if err := createISISAdjacentInTx(ctx, tx, mapping.localPK, mapping.neighborPK, neighbor, mapping.bandwidth, now); err != nil {
				s.log.Warn("graph: failed to create ISIS_ADJACENT",
					"from", mapping.localPK,
					"to", mapping.neighborPK,
					"error", err)
				continue
			}
			adjacenciesCreated++
		}
	}

	s.log.Debug("graph: ISIS sync in transaction completed",
		"adjacencies_created", adjacenciesCreated,
		"links_updated", linksUpdated,
		"devices_updated", devicesUpdated,
		"unmatched_neighbors", unmatchedNeighbors)

	return nil
}

// buildTunnelMapInTx queries Links within a transaction.
func (s *Store) buildTunnelMapInTx(ctx context.Context, tx neo4j.Transaction) (map[string]tunnelMapping, error) {
	cypher := `
		MATCH (link:Link)
		WHERE link.tunnel_net IS NOT NULL AND link.tunnel_net <> ''
		MATCH (link)-[:CONNECTS {side: 'A'}]->(devA:Device)
		MATCH (link)-[:CONNECTS {side: 'Z'}]->(devZ:Device)
		RETURN link.pk AS pk, link.tunnel_net AS tunnel_net, devA.pk AS side_a_pk, devZ.pk AS side_z_pk,
		       coalesce(link.bandwidth, 0) AS bandwidth
	`
	result, err := tx.Run(ctx, cypher, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}

	tunnelMap := make(map[string]tunnelMapping)

	for result.Next(ctx) {
		record := result.Record()
		tunnelNet, _ := record.Get("tunnel_net")
		sideAPK, _ := record.Get("side_a_pk")
		sideZPK, _ := record.Get("side_z_pk")
		linkPK, _ := record.Get("pk")
		bandwidth, _ := record.Get("bandwidth")

		tunnelNetStr, ok := tunnelNet.(string)
		if !ok || tunnelNetStr == "" {
			continue
		}
		sideAPKStr, _ := sideAPK.(string)
		sideZPKStr, _ := sideZPK.(string)
		linkPKStr, _ := linkPK.(string)
		bandwidthInt, _ := bandwidth.(int64)

		ip1, ip2, err := parseTunnelNet31(tunnelNetStr)
		if err != nil {
			s.log.Debug("graph: failed to parse tunnel_net",
				"tunnel_net", tunnelNetStr,
				"error", err)
			continue
		}

		tunnelMap[ip1] = tunnelMapping{
			linkPK:     linkPKStr,
			neighborPK: sideAPKStr,
			localPK:    sideZPKStr,
			bandwidth:  bandwidthInt,
		}
		tunnelMap[ip2] = tunnelMapping{
			linkPK:     linkPKStr,
			neighborPK: sideZPKStr,
			localPK:    sideAPKStr,
			bandwidth:  bandwidthInt,
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return tunnelMap, nil
}

// updateLinkISISInTx updates a Link node with IS-IS metric data within a transaction.
func updateLinkISISInTx(ctx context.Context, tx neo4j.Transaction, linkPK string, neighbor isis.Neighbor, timestamp time.Time) error {
	cypher := `
		MATCH (link:Link {pk: $pk})
		SET link.isis_metric = $metric,
		    link.isis_adj_sids = $adj_sids,
		    link.isis_last_sync = $last_sync
	`
	res, err := tx.Run(ctx, cypher, map[string]any{
		"pk":        linkPK,
		"metric":    neighbor.Metric,
		"adj_sids":  neighbor.AdjSIDs,
		"last_sync": timestamp.Unix(),
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// updateDeviceISISInTx updates a Device node with IS-IS properties within a transaction.
func updateDeviceISISInTx(ctx context.Context, tx neo4j.Transaction, devicePK string, lsp isis.LSP, timestamp time.Time) error {
	cypher := `
		MATCH (d:Device {pk: $pk})
		SET d.isis_system_id = $system_id,
		    d.isis_router_id = $router_id,
		    d.isis_last_sync = $last_sync
	`
	res, err := tx.Run(ctx, cypher, map[string]any{
		"pk":        devicePK,
		"system_id": lsp.SystemID,
		"router_id": lsp.RouterID,
		"last_sync": timestamp.Unix(),
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// createISISAdjacentInTx creates or updates an ISIS_ADJACENT relationship within a transaction.
func createISISAdjacentInTx(ctx context.Context, tx neo4j.Transaction, fromPK, toPK string, neighbor isis.Neighbor, bandwidth int64, timestamp time.Time) error {
	cypher := `
		MATCH (d1:Device {pk: $from_pk})
		MATCH (d2:Device {pk: $to_pk})
		MERGE (d1)-[r:ISIS_ADJACENT]->(d2)
		SET r.metric = $metric,
		    r.neighbor_addr = $neighbor_addr,
		    r.adj_sids = $adj_sids,
		    r.last_seen = $last_seen,
		    r.bandwidth_bps = $bandwidth_bps
	`
	res, err := tx.Run(ctx, cypher, map[string]any{
		"from_pk":       fromPK,
		"to_pk":         toPK,
		"metric":        neighbor.Metric,
		"neighbor_addr": neighbor.NeighborAddr,
		"adj_sids":      neighbor.AdjSIDs,
		"last_seen":     timestamp.Unix(),
		"bandwidth_bps": bandwidth,
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// batchCreateContributors creates all Contributor nodes in a single batched query.
func batchCreateContributors(ctx context.Context, tx neo4j.Transaction, contributors []dzsvc.Contributor) error {
	if len(contributors) == 0 {
		return nil
	}

	items := make([]map[string]any, len(contributors))
	for i, c := range contributors {
		items[i] = map[string]any{
			"pk":   c.PK,
			"code": c.Code,
			"name": c.Name,
		}
	}

	cypher := `
		UNWIND $items AS item
		CREATE (c:Contributor {pk: item.pk, code: item.code, name: item.name})
	`
	res, err := tx.Run(ctx, cypher, map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// batchCreateMetros creates all Metro nodes in a single batched query.
func batchCreateMetros(ctx context.Context, tx neo4j.Transaction, metros []dzsvc.Metro) error {
	if len(metros) == 0 {
		return nil
	}

	items := make([]map[string]any, len(metros))
	for i, m := range metros {
		items[i] = map[string]any{
			"pk":        m.PK,
			"code":      m.Code,
			"name":      m.Name,
			"longitude": m.Longitude,
			"latitude":  m.Latitude,
		}
	}

	cypher := `
		UNWIND $items AS item
		CREATE (m:Metro {pk: item.pk, code: item.code, name: item.name, longitude: item.longitude, latitude: item.latitude})
	`
	res, err := tx.Run(ctx, cypher, map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// batchCreateDevices creates all Device nodes and their relationships in batched queries.
func batchCreateDevices(ctx context.Context, tx neo4j.Transaction, devices []dzsvc.Device) error {
	if len(devices) == 0 {
		return nil
	}

	items := make([]map[string]any, len(devices))
	for i, d := range devices {
		items[i] = map[string]any{
			"pk":             d.PK,
			"status":         d.Status,
			"device_type":    d.DeviceType,
			"code":           d.Code,
			"public_ip":      d.PublicIP,
			"max_users":      d.MaxUsers,
			"contributor_pk": d.ContributorPK,
			"metro_pk":       d.MetroPK,
		}
	}

	// Create device nodes
	cypherNodes := `
		UNWIND $items AS item
		CREATE (d:Device {
			pk: item.pk,
			status: item.status,
			device_type: item.device_type,
			code: item.code,
			public_ip: item.public_ip,
			max_users: item.max_users
		})
	`
	res, err := tx.Run(ctx, cypherNodes, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create OPERATES relationships to Contributors
	cypherOperates := `
		UNWIND $items AS item
		MATCH (d:Device {pk: item.pk})
		MATCH (c:Contributor {pk: item.contributor_pk})
		CREATE (d)-[:OPERATES]->(c)
	`
	res, err = tx.Run(ctx, cypherOperates, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create LOCATED_IN relationships to Metros
	cypherLocatedIn := `
		UNWIND $items AS item
		MATCH (d:Device {pk: item.pk})
		MATCH (m:Metro {pk: item.metro_pk})
		CREATE (d)-[:LOCATED_IN]->(m)
	`
	res, err = tx.Run(ctx, cypherLocatedIn, map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// batchCreateLinks creates all Link nodes and their relationships in batched queries.
func batchCreateLinks(ctx context.Context, tx neo4j.Transaction, links []dzsvc.Link) error {
	if len(links) == 0 {
		return nil
	}

	items := make([]map[string]any, len(links))
	for i, l := range links {
		items[i] = map[string]any{
			"pk":                     l.PK,
			"status":                 l.Status,
			"code":                   l.Code,
			"tunnel_net":             l.TunnelNet,
			"link_type":              l.LinkType,
			"committed_rtt_ns":       l.CommittedRTTNs,
			"committed_jitter_ns":    l.CommittedJitterNs,
			"bandwidth":              l.Bandwidth,
			"isis_delay_override_ns": l.ISISDelayOverrideNs,
			"contributor_pk":         l.ContributorPK,
			"side_a_pk":              l.SideAPK,
			"side_z_pk":              l.SideZPK,
			"side_a_iface_name":      l.SideAIfaceName,
			"side_z_iface_name":      l.SideZIfaceName,
		}
	}

	// Create link nodes
	cypherNodes := `
		UNWIND $items AS item
		CREATE (link:Link {
			pk: item.pk,
			status: item.status,
			code: item.code,
			tunnel_net: item.tunnel_net,
			link_type: item.link_type,
			committed_rtt_ns: item.committed_rtt_ns,
			committed_jitter_ns: item.committed_jitter_ns,
			bandwidth: item.bandwidth,
			isis_delay_override_ns: item.isis_delay_override_ns
		})
	`
	res, err := tx.Run(ctx, cypherNodes, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create OWNED_BY relationships to Contributors
	cypherOwnedBy := `
		UNWIND $items AS item
		MATCH (link:Link {pk: item.pk})
		MATCH (c:Contributor {pk: item.contributor_pk})
		CREATE (link)-[:OWNED_BY]->(c)
	`
	res, err = tx.Run(ctx, cypherOwnedBy, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create CONNECTS relationships to side A devices
	cypherConnectsA := `
		UNWIND $items AS item
		MATCH (link:Link {pk: item.pk})
		MATCH (devA:Device {pk: item.side_a_pk})
		CREATE (link)-[:CONNECTS {side: 'A', iface_name: item.side_a_iface_name}]->(devA)
	`
	res, err = tx.Run(ctx, cypherConnectsA, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create CONNECTS relationships to side Z devices
	cypherConnectsZ := `
		UNWIND $items AS item
		MATCH (link:Link {pk: item.pk})
		MATCH (devZ:Device {pk: item.side_z_pk})
		CREATE (link)-[:CONNECTS {side: 'Z', iface_name: item.side_z_iface_name}]->(devZ)
	`
	res, err = tx.Run(ctx, cypherConnectsZ, map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// batchCreateUsers creates all User nodes and their relationships in batched queries.
func batchCreateUsers(ctx context.Context, tx neo4j.Transaction, users []dzsvc.User) error {
	if len(users) == 0 {
		return nil
	}

	items := make([]map[string]any, len(users))
	for i, u := range users {
		var clientIP, dzIP string
		if u.ClientIP != nil {
			clientIP = u.ClientIP.String()
		}
		if u.DZIP != nil {
			dzIP = u.DZIP.String()
		}
		items[i] = map[string]any{
			"pk":           u.PK,
			"owner_pubkey": u.OwnerPubkey,
			"status":       u.Status,
			"kind":         u.Kind,
			"client_ip":    clientIP,
			"dz_ip":        dzIP,
			"tunnel_id":    u.TunnelID,
			"device_pk":    u.DevicePK,
		}
	}

	// Create user nodes
	cypherNodes := `
		UNWIND $items AS item
		CREATE (user:User {
			pk: item.pk,
			owner_pubkey: item.owner_pubkey,
			status: item.status,
			kind: item.kind,
			client_ip: item.client_ip,
			dz_ip: item.dz_ip,
			tunnel_id: item.tunnel_id
		})
	`
	res, err := tx.Run(ctx, cypherNodes, map[string]any{"items": items})
	if err != nil {
		return err
	}
	if _, err := res.Consume(ctx); err != nil {
		return err
	}

	// Create ASSIGNED_TO relationships to Devices
	cypherAssignedTo := `
		UNWIND $items AS item
		MATCH (user:User {pk: item.pk})
		MATCH (dev:Device {pk: item.device_pk})
		CREATE (user)-[:ASSIGNED_TO]->(dev)
	`
	res, err = tx.Run(ctx, cypherAssignedTo, map[string]any{"items": items})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// tunnelMapping maps a tunnel IP address to Link and Device information.
type tunnelMapping struct {
	linkPK     string // Link primary key
	neighborPK string // Device PK of the neighbor (device with this IP)
	localPK    string // Device PK of the other side
	bandwidth  int64  // Link bandwidth in bps
}

// SyncISIS updates the Neo4j graph with IS-IS adjacency data.
// It correlates IS-IS neighbors with existing Links via tunnel_net IP addresses,
// creates ISIS_ADJACENT relationships between Devices, and updates Link properties.
func (s *Store) SyncISIS(ctx context.Context, lsps []isis.LSP) error {
	s.log.Debug("graph: starting ISIS sync", "lsps", len(lsps))

	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close(ctx)

	// Step 1: Query all Links with their tunnel_net and side device PKs
	tunnelMap, err := s.buildTunnelMap(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to build tunnel map: %w", err)
	}
	s.log.Debug("graph: built tunnel map", "mappings", len(tunnelMap))

	now := time.Now()
	var adjacenciesCreated, linksUpdated, devicesUpdated, unmatchedNeighbors int

	// Step 2: Process each LSP
	for _, lsp := range lsps {
		for _, neighbor := range lsp.Neighbors {
			// Try to find the Link via neighbor_addr
			mapping, found := tunnelMap[neighbor.NeighborAddr]
			if !found {
				unmatchedNeighbors++
				s.log.Debug("graph: unmatched IS-IS neighbor",
					"neighbor_addr", neighbor.NeighborAddr,
					"neighbor_system_id", neighbor.SystemID)
				continue
			}

			// Update Link with IS-IS metric
			if err := s.updateLinkISIS(ctx, session, mapping.linkPK, neighbor, now); err != nil {
				s.log.Warn("graph: failed to update link ISIS data",
					"link_pk", mapping.linkPK,
					"error", err)
				continue
			}
			linksUpdated++

			// Update Device nodes with IS-IS properties
			// The local device is the one advertising this LSP
			if err := s.updateDeviceISIS(ctx, session, mapping.localPK, lsp, now); err != nil {
				s.log.Warn("graph: failed to update local device ISIS data",
					"device_pk", mapping.localPK,
					"error", err)
			} else {
				devicesUpdated++
			}

			// Create ISIS_ADJACENT relationship with bandwidth from the link
			if err := s.createISISAdjacent(ctx, session, mapping.localPK, mapping.neighborPK, neighbor, mapping.bandwidth, now); err != nil {
				s.log.Warn("graph: failed to create ISIS_ADJACENT",
					"from", mapping.localPK,
					"to", mapping.neighborPK,
					"error", err)
				continue
			}
			adjacenciesCreated++
		}
	}

	s.log.Info("graph: ISIS sync completed",
		"lsps", len(lsps),
		"adjacencies_created", adjacenciesCreated,
		"links_updated", linksUpdated,
		"devices_updated", devicesUpdated,
		"unmatched_neighbors", unmatchedNeighbors)

	return nil
}

// buildTunnelMap queries Links from Neo4j and builds a map from IP addresses to tunnel mappings.
// For each /31 tunnel_net, both IPs are mapped: one points to side_a as neighbor, one to side_z.
func (s *Store) buildTunnelMap(ctx context.Context, session neo4j.Session) (map[string]tunnelMapping, error) {
	cypher := `
		MATCH (link:Link)
		WHERE link.tunnel_net IS NOT NULL AND link.tunnel_net <> ''
		MATCH (link)-[:CONNECTS {side: 'A'}]->(devA:Device)
		MATCH (link)-[:CONNECTS {side: 'Z'}]->(devZ:Device)
		RETURN link.pk AS pk, link.tunnel_net AS tunnel_net, devA.pk AS side_a_pk, devZ.pk AS side_z_pk,
		       coalesce(link.bandwidth, 0) AS bandwidth
	`
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query links: %w", err)
	}

	tunnelMap := make(map[string]tunnelMapping)

	for result.Next(ctx) {
		record := result.Record()
		tunnelNet, _ := record.Get("tunnel_net")
		sideAPK, _ := record.Get("side_a_pk")
		sideZPK, _ := record.Get("side_z_pk")
		linkPK, _ := record.Get("pk")
		bandwidth, _ := record.Get("bandwidth")

		tunnelNetStr, ok := tunnelNet.(string)
		if !ok || tunnelNetStr == "" {
			continue
		}
		sideAPKStr, _ := sideAPK.(string)
		sideZPKStr, _ := sideZPK.(string)
		linkPKStr, _ := linkPK.(string)
		bandwidthInt, _ := bandwidth.(int64)

		// Parse the /31 CIDR to get both IPs
		ip1, ip2, err := parseTunnelNet31(tunnelNetStr)
		if err != nil {
			s.log.Debug("graph: failed to parse tunnel_net",
				"tunnel_net", tunnelNetStr,
				"error", err)
			continue
		}

		// Map each IP: if neighbor_addr is ip1, then the device at ip1 is the neighbor
		// For /31: lower IP typically assigned to side_a, higher to side_z
		tunnelMap[ip1] = tunnelMapping{
			linkPK:     linkPKStr,
			neighborPK: sideAPKStr, // Device at ip1 (lower) is side_a
			localPK:    sideZPKStr,
			bandwidth:  bandwidthInt,
		}
		tunnelMap[ip2] = tunnelMapping{
			linkPK:     linkPKStr,
			neighborPK: sideZPKStr, // Device at ip2 (higher) is side_z
			localPK:    sideAPKStr,
			bandwidth:  bandwidthInt,
		}
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return tunnelMap, nil
}

// updateLinkISIS updates a Link node with IS-IS metric data.
func (s *Store) updateLinkISIS(ctx context.Context, session neo4j.Session, linkPK string, neighbor isis.Neighbor, timestamp time.Time) error {
	cypher := `
		MATCH (link:Link {pk: $pk})
		SET link.isis_metric = $metric,
		    link.isis_adj_sids = $adj_sids,
		    link.isis_last_sync = $last_sync
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":        linkPK,
		"metric":    neighbor.Metric,
		"adj_sids":  neighbor.AdjSIDs,
		"last_sync": timestamp.Unix(),
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// updateDeviceISIS updates a Device node with IS-IS properties.
func (s *Store) updateDeviceISIS(ctx context.Context, session neo4j.Session, devicePK string, lsp isis.LSP, timestamp time.Time) error {
	cypher := `
		MATCH (d:Device {pk: $pk})
		SET d.isis_system_id = $system_id,
		    d.isis_router_id = $router_id,
		    d.isis_last_sync = $last_sync
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":        devicePK,
		"system_id": lsp.SystemID,
		"router_id": lsp.RouterID,
		"last_sync": timestamp.Unix(),
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// createISISAdjacent creates or updates an ISIS_ADJACENT relationship between two devices.
func (s *Store) createISISAdjacent(ctx context.Context, session neo4j.Session, fromPK, toPK string, neighbor isis.Neighbor, bandwidth int64, timestamp time.Time) error {
	cypher := `
		MATCH (d1:Device {pk: $from_pk})
		MATCH (d2:Device {pk: $to_pk})
		MERGE (d1)-[r:ISIS_ADJACENT]->(d2)
		SET r.metric = $metric,
		    r.neighbor_addr = $neighbor_addr,
		    r.adj_sids = $adj_sids,
		    r.last_seen = $last_seen,
		    r.bandwidth_bps = $bandwidth_bps
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"from_pk":       fromPK,
		"to_pk":         toPK,
		"metric":        neighbor.Metric,
		"neighbor_addr": neighbor.NeighborAddr,
		"adj_sids":      neighbor.AdjSIDs,
		"last_seen":     timestamp.Unix(),
		"bandwidth_bps": bandwidth,
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

// parseTunnelNet31 parses a /31 CIDR and returns both IP addresses.
func parseTunnelNet31(cidr string) (string, string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", fmt.Errorf("invalid CIDR: %w", err)
	}

	ones, bits := ipnet.Mask.Size()
	if ones != 31 || bits != 32 {
		return "", "", fmt.Errorf("expected /31, got /%d", ones)
	}

	// For a /31, the network address and broadcast address are the two usable IPs
	ip := ipnet.IP.To4()
	if ip == nil {
		return "", "", fmt.Errorf("not an IPv4 address")
	}

	// First IP (network address in /31 is usable)
	ip1 := make(net.IP, 4)
	copy(ip1, ip)

	// Second IP (broadcast address in /31 is usable)
	ip2 := make(net.IP, 4)
	copy(ip2, ip)
	ip2[3]++

	return ip1.String(), ip2.String(), nil
}
