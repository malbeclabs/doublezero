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
// This performs a full sync, clearing and rebuilding the graph.
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

	// Clear and rebuild graph
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Neo4j session: %w", err)
	}
	defer session.Close(ctx)

	// Delete all existing nodes and relationships
	res, err := session.Run(ctx, "MATCH (n) DETACH DELETE n", nil)
	if err != nil {
		return fmt.Errorf("failed to clear graph: %w", err)
	}
	if _, err := res.Consume(ctx); err != nil {
		return fmt.Errorf("failed to consume clear result: %w", err)
	}

	// Create Contributor nodes
	for _, c := range contributors {
		if err := s.createContributorNode(ctx, session, c); err != nil {
			return fmt.Errorf("failed to create contributor node: %w", err)
		}
	}

	// Create Metro nodes
	for _, m := range metros {
		if err := s.createMetroNode(ctx, session, m); err != nil {
			return fmt.Errorf("failed to create metro node: %w", err)
		}
	}

	// Create Device nodes and relationships
	for _, d := range devices {
		if err := s.createDeviceNode(ctx, session, d); err != nil {
			return fmt.Errorf("failed to create device node: %w", err)
		}
	}

	// Create Link nodes and relationships
	for _, l := range links {
		if err := s.createLinkNode(ctx, session, l); err != nil {
			return fmt.Errorf("failed to create link node: %w", err)
		}
	}

	// Create User nodes and relationships
	for _, u := range users {
		if err := s.createUserNode(ctx, session, u); err != nil {
			return fmt.Errorf("failed to create user node: %w", err)
		}
	}

	s.log.Info("graph: sync completed",
		"devices", len(devices),
		"links", len(links),
		"metros", len(metros),
		"users", len(users))

	return nil
}

func (s *Store) createContributorNode(ctx context.Context, session neo4j.Session, c dzsvc.Contributor) error {
	cypher := `
		MERGE (cont:Contributor {pk: $pk})
		SET cont.code = $code,
		    cont.name = $name
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":   c.PK,
		"code": c.Code,
		"name": c.Name,
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

func (s *Store) createMetroNode(ctx context.Context, session neo4j.Session, m dzsvc.Metro) error {
	cypher := `
		MERGE (m:Metro {pk: $pk})
		SET m.code = $code,
		    m.name = $name,
		    m.longitude = $longitude,
		    m.latitude = $latitude
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":        m.PK,
		"code":      m.Code,
		"name":      m.Name,
		"longitude": m.Longitude,
		"latitude":  m.Latitude,
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

func (s *Store) createDeviceNode(ctx context.Context, session neo4j.Session, d dzsvc.Device) error {
	cypher := `
		MERGE (dev:Device {pk: $pk})
		SET dev.status = $status,
		    dev.device_type = $device_type,
		    dev.code = $code,
		    dev.public_ip = $public_ip,
		    dev.max_users = $max_users
		WITH dev
		MATCH (c:Contributor {pk: $contributor_pk})
		MERGE (dev)-[:OPERATES]->(c)
		WITH dev
		MATCH (m:Metro {pk: $metro_pk})
		MERGE (dev)-[:LOCATED_IN]->(m)
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":             d.PK,
		"status":         d.Status,
		"device_type":    d.DeviceType,
		"code":           d.Code,
		"public_ip":      d.PublicIP,
		"max_users":      d.MaxUsers,
		"contributor_pk": d.ContributorPK,
		"metro_pk":       d.MetroPK,
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

func (s *Store) createLinkNode(ctx context.Context, session neo4j.Session, l dzsvc.Link) error {
	cypher := `
		MERGE (link:Link {pk: $pk})
		SET link.status = $status,
		    link.code = $code,
		    link.tunnel_net = $tunnel_net,
		    link.link_type = $link_type,
		    link.committed_rtt_ns = $committed_rtt_ns,
		    link.committed_jitter_ns = $committed_jitter_ns,
		    link.bandwidth = $bandwidth,
		    link.isis_delay_override_ns = $isis_delay_override_ns
		WITH link
		MATCH (c:Contributor {pk: $contributor_pk})
		MERGE (link)-[:OWNED_BY]->(c)
		WITH link
		MATCH (devA:Device {pk: $side_a_pk})
		MERGE (link)-[:CONNECTS {side: 'A', iface_name: $side_a_iface_name}]->(devA)
		WITH link
		MATCH (devZ:Device {pk: $side_z_pk})
		MERGE (link)-[:CONNECTS {side: 'Z', iface_name: $side_z_iface_name}]->(devZ)
	`
	res, err := session.Run(ctx, cypher, map[string]any{
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
	})
	if err != nil {
		return err
	}
	_, err = res.Consume(ctx)
	return err
}

func (s *Store) createUserNode(ctx context.Context, session neo4j.Session, u dzsvc.User) error {
	cypher := `
		MERGE (user:User {pk: $pk})
		SET user.owner_pubkey = $owner_pubkey,
		    user.status = $status,
		    user.kind = $kind,
		    user.client_ip = $client_ip,
		    user.dz_ip = $dz_ip,
		    user.tunnel_id = $tunnel_id
		WITH user
		MATCH (dev:Device {pk: $device_pk})
		MERGE (user)-[:ASSIGNED_TO]->(dev)
	`
	var clientIP, dzIP string
	if u.ClientIP != nil {
		clientIP = u.ClientIP.String()
	}
	if u.DZIP != nil {
		dzIP = u.DZIP.String()
	}
	res, err := session.Run(ctx, cypher, map[string]any{
		"pk":           u.PK,
		"owner_pubkey": u.OwnerPubkey,
		"status":       u.Status,
		"kind":         u.Kind,
		"client_ip":    clientIP,
		"dz_ip":        dzIP,
		"tunnel_id":    u.TunnelID,
		"device_pk":    u.DevicePK,
	})
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

			// Create ISIS_ADJACENT relationship
			if err := s.createISISAdjacent(ctx, session, mapping.localPK, mapping.neighborPK, neighbor, now); err != nil {
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
		RETURN link.pk AS pk, link.tunnel_net AS tunnel_net, devA.pk AS side_a_pk, devZ.pk AS side_z_pk
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

		tunnelNetStr, ok := tunnelNet.(string)
		if !ok || tunnelNetStr == "" {
			continue
		}
		sideAPKStr, _ := sideAPK.(string)
		sideZPKStr, _ := sideZPK.(string)
		linkPKStr, _ := linkPK.(string)

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
		}
		tunnelMap[ip2] = tunnelMapping{
			linkPK:     linkPKStr,
			neighborPK: sideZPKStr, // Device at ip2 (higher) is side_z
			localPK:    sideAPKStr,
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
func (s *Store) createISISAdjacent(ctx context.Context, session neo4j.Session, fromPK, toPK string, neighbor isis.Neighbor, timestamp time.Time) error {
	cypher := `
		MATCH (d1:Device {pk: $from_pk})
		MATCH (d2:Device {pk: $to_pk})
		MERGE (d1)-[r:ISIS_ADJACENT]->(d2)
		SET r.metric = $metric,
		    r.neighbor_addr = $neighbor_addr,
		    r.adj_sids = $adj_sids,
		    r.last_seen = $last_seen
	`
	res, err := session.Run(ctx, cypher, map[string]any{
		"from_pk":       fromPK,
		"to_pk":         toPK,
		"metric":        neighbor.Metric,
		"neighbor_addr": neighbor.NeighborAddr,
		"adj_sids":      neighbor.AdjSIDs,
		"last_seen":     timestamp.Unix(),
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
