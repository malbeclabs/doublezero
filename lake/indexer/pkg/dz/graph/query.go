package graph

import (
	"context"
	"fmt"

	dzsvc "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

// PathWeight determines how paths are weighted for shortest path queries.
type PathWeight string

const (
	PathWeightHops      PathWeight = "hops"
	PathWeightRTT       PathWeight = "rtt"
	PathWeightBandwidth PathWeight = "bandwidth"
)

// PathSegment represents a segment in a path (either a device or a link).
type PathSegment struct {
	Type   string // "device" or "link"
	PK     string
	Code   string
	Status string

	// Link-specific fields
	RTTNs     uint64
	Bandwidth uint64
	IsDrained bool
}

// RouteHop represents a hop in a route with full details.
type RouteHop struct {
	Type   string // "device" or "link"
	PK     string
	Code   string
	Status string

	// Device-specific fields
	DeviceType string
	MetroPK    string

	// Link-specific fields
	RTTNs     uint64
	JitterNs  uint64
	Bandwidth uint64
	IsDrained bool
}

// NetworkSubgraph represents a subgraph of the network around a device.
type NetworkSubgraph struct {
	Devices []NetworkDevice
	Links   []NetworkLink
}

// ISISAdjacency represents an ISIS adjacency between two devices.
type ISISAdjacency struct {
	FromDevicePK string
	FromCode     string
	ToDevicePK   string
	ToCode       string
	Metric       uint32
	NeighborAddr string
	AdjSIDs      []uint32
}

// TopologyDiscrepancy represents a difference between configured and ISIS topology.
type TopologyDiscrepancy struct {
	Type        string // "missing_isis", "extra_isis", "metric_mismatch"
	LinkPK      string // For configured links
	LinkCode    string
	DeviceAPK   string
	DeviceACode string
	DeviceBPK   string
	DeviceBCode string
	// For metric mismatches
	ConfiguredRTTNs uint64 // Configured RTT in nanoseconds
	ISISMetric      uint32 // ISIS metric (typically microseconds)
	// Additional context
	Details string
}

// TopologyComparison contains the results of comparing configured vs ISIS topology.
type TopologyComparison struct {
	// ConfiguredLinks is the count of links in the configured topology
	ConfiguredLinks int
	// ISISAdjacencies is the count of ISIS adjacencies discovered
	ISISAdjacencies int
	// MatchedLinks is the count of links that have corresponding ISIS adjacencies
	MatchedLinks int
	// Discrepancies lists all differences found
	Discrepancies []TopologyDiscrepancy
}

// ISISDevice represents a device with ISIS properties.
type ISISDevice struct {
	PK         string
	Code       string
	Status     string
	DeviceType string
	SystemID   string
	RouterID   string
}

// ISISLink represents a link with ISIS properties.
type ISISLink struct {
	PK         string
	Code       string
	Status     string
	SideAPK    string
	SideZPK    string
	ISISMetric uint32
	AdjSIDs    []uint32
}

// NetworkDevice represents a device in a network subgraph.
type NetworkDevice struct {
	PK         string
	Code       string
	Status     string
	DeviceType string
}

// NetworkLink represents a link in a network subgraph.
type NetworkLink struct {
	PK        string
	Code      string
	Status    string
	RTTNs     uint64
	Bandwidth uint64
	IsDrained bool
	SideAPK   string
	SideZPK   string
}

// UnreachableIfDown returns devices that would become unreachable if the specified device goes down.
// maxHops limits how far to search in the graph (0 = unlimited).
func (s *Store) UnreachableIfDown(ctx context.Context, devicePK string, maxHops int) ([]dzsvc.Device, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	// Find devices that only have paths through the specified device
	// This uses a simplified approach: find devices where all paths to other devices
	// pass through the target device
	hopLimit := 10
	if maxHops > 0 {
		hopLimit = maxHops
	}

	cypher := fmt.Sprintf(`
		MATCH (target:Device {pk: $device_pk})
		MATCH (other:Device)
		WHERE other.pk <> $device_pk
		  AND other.status = 'active'
		WITH target, other
		// Check if there's any path to this device that doesn't go through target
		WHERE NOT EXISTS {
			MATCH path = (other)-[:CONNECTS*1..%d]-(anyDevice:Device)
			WHERE anyDevice.pk <> $device_pk
			  AND anyDevice <> other
			  AND NOT ANY(n IN nodes(path) WHERE n:Device AND n.pk = $device_pk)
			  AND ALL(link IN [n IN nodes(path) WHERE n:Link] | link.status = 'active')
		}
		RETURN other.pk AS pk,
		       other.status AS status,
		       other.device_type AS device_type,
		       other.code AS code,
		       other.public_ip AS public_ip,
		       other.max_users AS max_users
	`, hopLimit)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"device_pk": devicePK,
		})
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		devices := make([]dzsvc.Device, 0, len(records))
		for _, record := range records {
			pk, _ := record.Get("pk")
			status, _ := record.Get("status")
			deviceType, _ := record.Get("device_type")
			code, _ := record.Get("code")
			publicIP, _ := record.Get("public_ip")
			maxUsers, _ := record.Get("max_users")

			devices = append(devices, dzsvc.Device{
				PK:         asString(pk),
				Status:     asString(status),
				DeviceType: asString(deviceType),
				Code:       asString(code),
				PublicIP:   asString(publicIP),
				MaxUsers:   uint16(asInt64(maxUsers)),
			})
		}
		return devices, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]dzsvc.Device), nil
}

// ReachableFromMetro returns devices reachable from a metro, optionally filtering for active links only.
func (s *Store) ReachableFromMetro(ctx context.Context, metroPK string, activeOnly bool) ([]dzsvc.Device, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	var cypher string
	if activeOnly {
		cypher = `
			MATCH (m:Metro {pk: $metro_pk})<-[:LOCATED_IN]-(start:Device)
			WHERE start.status = 'active'
			OPTIONAL MATCH path = (start)-[:CONNECTS*1..10]-(other:Device)
			WHERE other.status = 'active'
			  AND ALL(n IN nodes(path) WHERE
			    (n:Device) OR (n:Link AND n.status = 'active'))
			WITH DISTINCT coalesce(other, start) AS device
			RETURN device.pk AS pk,
			       device.status AS status,
			       device.device_type AS device_type,
			       device.code AS code,
			       device.public_ip AS public_ip,
			       device.max_users AS max_users
		`
	} else {
		cypher = `
			MATCH (m:Metro {pk: $metro_pk})<-[:LOCATED_IN]-(start:Device)
			OPTIONAL MATCH path = (start)-[:CONNECTS*1..10]-(other:Device)
			WITH DISTINCT coalesce(other, start) AS device
			RETURN device.pk AS pk,
			       device.status AS status,
			       device.device_type AS device_type,
			       device.code AS code,
			       device.public_ip AS public_ip,
			       device.max_users AS max_users
		`
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"metro_pk": metroPK,
		})
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		devices := make([]dzsvc.Device, 0, len(records))
		for _, record := range records {
			pk, _ := record.Get("pk")
			if pk == nil {
				continue
			}
			status, _ := record.Get("status")
			deviceType, _ := record.Get("device_type")
			code, _ := record.Get("code")
			publicIP, _ := record.Get("public_ip")
			maxUsers, _ := record.Get("max_users")

			devices = append(devices, dzsvc.Device{
				PK:         asString(pk),
				Status:     asString(status),
				DeviceType: asString(deviceType),
				Code:       asString(code),
				PublicIP:   asString(publicIP),
				MaxUsers:   uint16(asInt64(maxUsers)),
			})
		}
		return devices, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]dzsvc.Device), nil
}

// ShortestPath finds the shortest path between two devices.
// weightBy determines how the path is weighted (hops, RTT, or bandwidth).
func (s *Store) ShortestPath(ctx context.Context, fromPK, toPK string, weightBy PathWeight) ([]PathSegment, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	var cypher string
	switch weightBy {
	case PathWeightRTT:
		// Use Dijkstra with RTT as weight (requires APOC)
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			CALL apoc.algo.dijkstra(a, b, 'CONNECTS', 'committed_rtt_ns')
			YIELD path, weight
			RETURN [n IN nodes(path) |
				CASE WHEN n:Device THEN {type: 'device', pk: n.pk, code: n.code, status: n.status}
				     WHEN n:Link THEN {type: 'link', pk: n.pk, code: n.code, status: n.status,
				                       rtt_ns: n.committed_rtt_ns, bandwidth: n.bandwidth,
				                       is_drained: n.isis_delay_override_ns > 0}
				END
			] AS segments
		`
	case PathWeightBandwidth:
		// For bandwidth, we want to maximize, so we use inverse
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			MATCH path = shortestPath((a)-[:CONNECTS*]-(b))
			RETURN [n IN nodes(path) |
				CASE WHEN n:Device THEN {type: 'device', pk: n.pk, code: n.code, status: n.status}
				     WHEN n:Link THEN {type: 'link', pk: n.pk, code: n.code, status: n.status,
				                       rtt_ns: n.committed_rtt_ns, bandwidth: n.bandwidth,
				                       is_drained: n.isis_delay_override_ns > 0}
				END
			] AS segments
		`
	default: // PathWeightHops
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			MATCH path = shortestPath((a)-[:CONNECTS*]-(b))
			RETURN [n IN nodes(path) |
				CASE WHEN n:Device THEN {type: 'device', pk: n.pk, code: n.code, status: n.status}
				     WHEN n:Link THEN {type: 'link', pk: n.pk, code: n.code, status: n.status,
				                       rtt_ns: n.committed_rtt_ns, bandwidth: n.bandwidth,
				                       is_drained: n.isis_delay_override_ns > 0}
				END
			] AS segments
		`
	}

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"from_pk": fromPK,
			"to_pk":   toPK,
		})
		if err != nil {
			return nil, err
		}

		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}

		segmentsVal, _ := record.Get("segments")
		segments := parsePathSegments(segmentsVal)
		return segments, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]PathSegment), nil
}

// ExplainRoute returns a detailed route explanation between two devices.
func (s *Store) ExplainRoute(ctx context.Context, fromPK, toPK string) ([]RouteHop, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	cypher := `
		MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
		MATCH path = shortestPath((a)-[:CONNECTS*]-(b))
		RETURN [n IN nodes(path) |
			CASE WHEN n:Device THEN {
				type: 'device',
				pk: n.pk,
				code: n.code,
				status: n.status,
				device_type: n.device_type
			}
			WHEN n:Link THEN {
				type: 'link',
				pk: n.pk,
				code: n.code,
				status: n.status,
				rtt_ns: n.committed_rtt_ns,
				jitter_ns: n.committed_jitter_ns,
				bandwidth: n.bandwidth,
				is_drained: n.isis_delay_override_ns > 0
			}
			END
		] AS route
	`

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"from_pk": fromPK,
			"to_pk":   toPK,
		})
		if err != nil {
			return nil, err
		}

		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}

		routeVal, _ := record.Get("route")
		hops := parseRouteHops(routeVal)
		return hops, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]RouteHop), nil
}

// NetworkAroundDevice returns the network subgraph around a device up to N hops.
func (s *Store) NetworkAroundDevice(ctx context.Context, devicePK string, hops int) (*NetworkSubgraph, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	if hops <= 0 {
		hops = 2
	}

	cypher := fmt.Sprintf(`
		MATCH (center:Device {pk: $device_pk})
		OPTIONAL MATCH path = (center)-[:CONNECTS*1..%d]-(neighbor)
		WITH collect(path) AS paths, center
		UNWIND CASE WHEN size(paths) = 0 THEN [null] ELSE paths END AS p
		WITH DISTINCT CASE WHEN p IS NULL THEN center ELSE nodes(p) END AS nodeList
		UNWIND nodeList AS n
		WITH DISTINCT n
		WHERE n IS NOT NULL
		WITH collect(n) AS allNodes
		UNWIND allNodes AS node
		WITH node, allNodes
		RETURN
			CASE WHEN node:Device THEN 'device' ELSE 'link' END AS node_type,
			node.pk AS pk,
			node.code AS code,
			node.status AS status,
			CASE WHEN node:Device THEN node.device_type ELSE null END AS device_type,
			CASE WHEN node:Link THEN node.committed_rtt_ns ELSE null END AS rtt_ns,
			CASE WHEN node:Link THEN node.bandwidth ELSE null END AS bandwidth,
			CASE WHEN node:Link THEN node.isis_delay_override_ns > 0 ELSE null END AS is_drained,
			CASE WHEN node:Link THEN
				[(node)-[:CONNECTS]->(d:Device) | d.pk]
			ELSE null END AS connected_devices
	`, hops)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"device_pk": devicePK,
		})
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		subgraph := &NetworkSubgraph{
			Devices: make([]NetworkDevice, 0),
			Links:   make([]NetworkLink, 0),
		}

		for _, record := range records {
			nodeType, _ := record.Get("node_type")
			pk, _ := record.Get("pk")
			code, _ := record.Get("code")
			status, _ := record.Get("status")

			if asString(nodeType) == "device" {
				deviceType, _ := record.Get("device_type")
				subgraph.Devices = append(subgraph.Devices, NetworkDevice{
					PK:         asString(pk),
					Code:       asString(code),
					Status:     asString(status),
					DeviceType: asString(deviceType),
				})
			} else {
				rttNs, _ := record.Get("rtt_ns")
				bandwidth, _ := record.Get("bandwidth")
				isDrained, _ := record.Get("is_drained")
				connectedDevices, _ := record.Get("connected_devices")

				var sideAPK, sideZPK string
				if devs, ok := connectedDevices.([]any); ok && len(devs) >= 2 {
					sideAPK = asString(devs[0])
					sideZPK = asString(devs[1])
				} else if devs, ok := connectedDevices.([]any); ok && len(devs) == 1 {
					sideAPK = asString(devs[0])
				}

				subgraph.Links = append(subgraph.Links, NetworkLink{
					PK:        asString(pk),
					Code:      asString(code),
					Status:    asString(status),
					RTTNs:     uint64(asInt64(rttNs)),
					Bandwidth: uint64(asInt64(bandwidth)),
					IsDrained: asBool(isDrained),
					SideAPK:   sideAPK,
					SideZPK:   sideZPK,
				})
			}
		}
		return subgraph, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*NetworkSubgraph), nil
}

// ISISAdjacencies returns all ISIS adjacencies for a device.
func (s *Store) ISISAdjacencies(ctx context.Context, devicePK string) ([]ISISAdjacency, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	cypher := `
		MATCH (from:Device {pk: $device_pk})-[r:ISIS_ADJACENT]->(to:Device)
		RETURN from.pk AS from_pk,
		       from.code AS from_code,
		       to.pk AS to_pk,
		       to.code AS to_code,
		       r.metric AS metric,
		       r.neighbor_addr AS neighbor_addr,
		       r.adj_sids AS adj_sids
	`

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"device_pk": devicePK,
		})
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		adjacencies := make([]ISISAdjacency, 0, len(records))
		for _, record := range records {
			fromPK, _ := record.Get("from_pk")
			fromCode, _ := record.Get("from_code")
			toPK, _ := record.Get("to_pk")
			toCode, _ := record.Get("to_code")
			metric, _ := record.Get("metric")
			neighborAddr, _ := record.Get("neighbor_addr")
			adjSids, _ := record.Get("adj_sids")

			adjacencies = append(adjacencies, ISISAdjacency{
				FromDevicePK: asString(fromPK),
				FromCode:     asString(fromCode),
				ToDevicePK:   asString(toPK),
				ToCode:       asString(toCode),
				Metric:       uint32(asInt64(metric)),
				NeighborAddr: asString(neighborAddr),
				AdjSIDs:      asUint32Slice(adjSids),
			})
		}
		return adjacencies, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]ISISAdjacency), nil
}

// ISISTopology returns the full ISIS topology (all devices with ISIS data and their adjacencies).
func (s *Store) ISISTopology(ctx context.Context) ([]ISISDevice, []ISISAdjacency, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	// Get devices with ISIS data
	deviceCypher := `
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		RETURN d.pk AS pk,
		       d.code AS code,
		       d.status AS status,
		       d.device_type AS device_type,
		       d.isis_system_id AS system_id,
		       d.isis_router_id AS router_id
	`

	devices, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, deviceCypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		devices := make([]ISISDevice, 0, len(records))
		for _, record := range records {
			pk, _ := record.Get("pk")
			code, _ := record.Get("code")
			status, _ := record.Get("status")
			deviceType, _ := record.Get("device_type")
			systemID, _ := record.Get("system_id")
			routerID, _ := record.Get("router_id")

			devices = append(devices, ISISDevice{
				PK:         asString(pk),
				Code:       asString(code),
				Status:     asString(status),
				DeviceType: asString(deviceType),
				SystemID:   asString(systemID),
				RouterID:   asString(routerID),
			})
		}
		return devices, nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Get all ISIS adjacencies
	adjCypher := `
		MATCH (from:Device)-[r:ISIS_ADJACENT]->(to:Device)
		RETURN from.pk AS from_pk,
		       from.code AS from_code,
		       to.pk AS to_pk,
		       to.code AS to_code,
		       r.metric AS metric,
		       r.neighbor_addr AS neighbor_addr,
		       r.adj_sids AS adj_sids
	`

	adjacencies, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, adjCypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		adjacencies := make([]ISISAdjacency, 0, len(records))
		for _, record := range records {
			fromPK, _ := record.Get("from_pk")
			fromCode, _ := record.Get("from_code")
			toPK, _ := record.Get("to_pk")
			toCode, _ := record.Get("to_code")
			metric, _ := record.Get("metric")
			neighborAddr, _ := record.Get("neighbor_addr")
			adjSids, _ := record.Get("adj_sids")

			adjacencies = append(adjacencies, ISISAdjacency{
				FromDevicePK: asString(fromPK),
				FromCode:     asString(fromCode),
				ToDevicePK:   asString(toPK),
				ToCode:       asString(toCode),
				Metric:       uint32(asInt64(metric)),
				NeighborAddr: asString(neighborAddr),
				AdjSIDs:      asUint32Slice(adjSids),
			})
		}
		return adjacencies, nil
	})
	if err != nil {
		return nil, nil, err
	}

	return devices.([]ISISDevice), adjacencies.([]ISISAdjacency), nil
}

// ShortestPathByISISMetric finds the shortest path using ISIS metrics as weights.
// This traverses ISIS_ADJACENT relationships directly (control plane path).
func (s *Store) ShortestPathByISISMetric(ctx context.Context, fromPK, toPK string) ([]ISISDevice, uint32, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	cypher := `
		MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
		MATCH path = shortestPath((a)-[:ISIS_ADJACENT*]->(b))
		WITH path, reduce(total = 0, r IN relationships(path) | total + r.metric) AS total_metric
		RETURN [n IN nodes(path) | {
			pk: n.pk,
			code: n.code,
			status: n.status,
			device_type: n.device_type,
			system_id: n.isis_system_id,
			router_id: n.isis_router_id
		}] AS devices,
		total_metric
	`

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, map[string]any{
			"from_pk": fromPK,
			"to_pk":   toPK,
		})
		if err != nil {
			return nil, err
		}

		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}

		devicesVal, _ := record.Get("devices")
		totalMetric, _ := record.Get("total_metric")

		devices := parseISISDevices(devicesVal)
		return struct {
			devices     []ISISDevice
			totalMetric uint32
		}{devices, uint32(asInt64(totalMetric))}, nil
	})
	if err != nil {
		return nil, 0, err
	}

	r := result.(struct {
		devices     []ISISDevice
		totalMetric uint32
	})
	return r.devices, r.totalMetric, nil
}

// LinksWithISISMetrics returns all links that have ISIS metrics set.
func (s *Store) LinksWithISISMetrics(ctx context.Context) ([]ISISLink, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	cypher := `
		MATCH (l:Link)
		WHERE l.isis_metric IS NOT NULL
		OPTIONAL MATCH (l)-[:CONNECTS]->(d:Device)
		WITH l, collect(d.pk) AS device_pks
		RETURN l.pk AS pk,
		       l.code AS code,
		       l.status AS status,
		       l.isis_metric AS isis_metric,
		       l.isis_adj_sids AS adj_sids,
		       device_pks[0] AS side_a_pk,
		       device_pks[1] AS side_z_pk
	`

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		links := make([]ISISLink, 0, len(records))
		for _, record := range records {
			pk, _ := record.Get("pk")
			code, _ := record.Get("code")
			status, _ := record.Get("status")
			isisMetric, _ := record.Get("isis_metric")
			adjSids, _ := record.Get("adj_sids")
			sideAPK, _ := record.Get("side_a_pk")
			sideZPK, _ := record.Get("side_z_pk")

			links = append(links, ISISLink{
				PK:         asString(pk),
				Code:       asString(code),
				Status:     asString(status),
				SideAPK:    asString(sideAPK),
				SideZPK:    asString(sideZPK),
				ISISMetric: uint32(asInt64(isisMetric)),
				AdjSIDs:    asUint32Slice(adjSids),
			})
		}
		return links, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]ISISLink), nil
}

// configuredLinkResult holds the result of querying a configured link
type configuredLinkResult struct {
	linkPK            string
	linkCode          string
	linkStatus        string
	configuredRTTNs   int64
	deviceAPK         string
	deviceACode       string
	deviceBPK         string
	deviceBCode       string
	hasForwardAdj     bool
	hasReverseAdj     bool
	isisMetricForward int64
}

// extraISISResult holds the result of an extra ISIS adjacency
type extraISISResult struct {
	deviceAPK    string
	deviceACode  string
	deviceBPK    string
	deviceBCode  string
	isisMetric   int64
	neighborAddr string
}

// CompareTopology compares configured (serviceability) topology with ISIS-discovered topology.
// It returns discrepancies including missing ISIS adjacencies, extra ISIS adjacencies, and metric mismatches.
func (s *Store) CompareTopology(ctx context.Context) (*TopologyComparison, error) {
	session, err := s.cfg.Neo4j.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close(ctx)

	comparison := &TopologyComparison{
		Discrepancies: make([]TopologyDiscrepancy, 0),
	}

	// Query 1: Find configured links and check if they have ISIS adjacencies
	configuredCypher := `
		MATCH (l:Link)-[:CONNECTS]->(da:Device)
		MATCH (l)-[:CONNECTS]->(db:Device)
		WHERE da.pk < db.pk
		OPTIONAL MATCH (da)-[isis:ISIS_ADJACENT]->(db)
		OPTIONAL MATCH (db)-[isis_rev:ISIS_ADJACENT]->(da)
		RETURN l.pk AS link_pk,
		       l.code AS link_code,
		       l.status AS link_status,
		       l.committed_rtt_ns AS configured_rtt_ns,
		       da.pk AS device_a_pk,
		       da.code AS device_a_code,
		       db.pk AS device_b_pk,
		       db.code AS device_b_code,
		       isis.metric AS isis_metric_forward,
		       isis IS NOT NULL AS has_forward_adj,
		       isis_rev IS NOT NULL AS has_reverse_adj
	`

	configuredResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, configuredCypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		results := make([]configuredLinkResult, 0, len(records))
		for _, record := range records {
			linkPK, _ := record.Get("link_pk")
			linkCode, _ := record.Get("link_code")
			linkStatus, _ := record.Get("link_status")
			configuredRTTNs, _ := record.Get("configured_rtt_ns")
			deviceAPK, _ := record.Get("device_a_pk")
			deviceACode, _ := record.Get("device_a_code")
			deviceBPK, _ := record.Get("device_b_pk")
			deviceBCode, _ := record.Get("device_b_code")
			hasForwardAdj, _ := record.Get("has_forward_adj")
			hasReverseAdj, _ := record.Get("has_reverse_adj")
			isisMetricForward, _ := record.Get("isis_metric_forward")

			results = append(results, configuredLinkResult{
				linkPK:            asString(linkPK),
				linkCode:          asString(linkCode),
				linkStatus:        asString(linkStatus),
				configuredRTTNs:   asInt64(configuredRTTNs),
				deviceAPK:         asString(deviceAPK),
				deviceACode:       asString(deviceACode),
				deviceBPK:         asString(deviceBPK),
				deviceBCode:       asString(deviceBCode),
				hasForwardAdj:     asBool(hasForwardAdj),
				hasReverseAdj:     asBool(hasReverseAdj),
				isisMetricForward: asInt64(isisMetricForward),
			})
		}
		return results, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query configured links: %w", err)
	}

	configuredLinks := configuredResult.([]configuredLinkResult)
	comparison.ConfiguredLinks = len(configuredLinks)

	for _, link := range configuredLinks {
		if link.hasForwardAdj || link.hasReverseAdj {
			comparison.MatchedLinks++
		}

		// Check for missing ISIS adjacencies on active links
		if link.linkStatus == "active" && !link.hasForwardAdj && !link.hasReverseAdj {
			comparison.Discrepancies = append(comparison.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      link.linkPK,
				LinkCode:    link.linkCode,
				DeviceAPK:   link.deviceAPK,
				DeviceACode: link.deviceACode,
				DeviceBPK:   link.deviceBPK,
				DeviceBCode: link.deviceBCode,
				Details:     "Active link has no ISIS adjacency in either direction",
			})
		} else if link.linkStatus == "active" && link.hasForwardAdj != link.hasReverseAdj {
			direction := "forward only"
			if link.hasReverseAdj && !link.hasForwardAdj {
				direction = "reverse only"
			}
			comparison.Discrepancies = append(comparison.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      link.linkPK,
				LinkCode:    link.linkCode,
				DeviceAPK:   link.deviceAPK,
				DeviceACode: link.deviceACode,
				DeviceBPK:   link.deviceBPK,
				DeviceBCode: link.deviceBCode,
				Details:     fmt.Sprintf("ISIS adjacency is %s (should be bidirectional)", direction),
			})
		}

		// Check for metric mismatch
		if link.hasForwardAdj && link.configuredRTTNs > 0 && link.isisMetricForward > 0 {
			configRTTUs := uint64(link.configuredRTTNs) / 1000
			isisMetric := uint32(link.isisMetricForward)
			if configRTTUs > 0 {
				ratio := float64(isisMetric) / float64(configRTTUs)
				if ratio < 0.5 || ratio > 2.0 {
					comparison.Discrepancies = append(comparison.Discrepancies, TopologyDiscrepancy{
						Type:            "metric_mismatch",
						LinkPK:          link.linkPK,
						LinkCode:        link.linkCode,
						DeviceAPK:       link.deviceAPK,
						DeviceACode:     link.deviceACode,
						DeviceBPK:       link.deviceBPK,
						DeviceBCode:     link.deviceBCode,
						ConfiguredRTTNs: uint64(link.configuredRTTNs),
						ISISMetric:      isisMetric,
						Details:         fmt.Sprintf("ISIS metric (%d µs) differs significantly from configured RTT (%d µs)", isisMetric, configRTTUs),
					})
				}
			}
		}
	}

	// Query 2: Find ISIS adjacencies that don't correspond to any configured link
	extraCypher := `
		MATCH (da:Device)-[isis:ISIS_ADJACENT]->(db:Device)
		WHERE NOT EXISTS {
			MATCH (l:Link)-[:CONNECTS]->(da)
			MATCH (l)-[:CONNECTS]->(db)
		}
		RETURN da.pk AS device_a_pk,
		       da.code AS device_a_code,
		       db.pk AS device_b_pk,
		       db.code AS device_b_code,
		       isis.metric AS isis_metric,
		       isis.neighbor_addr AS neighbor_addr
	`

	extraResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, extraCypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		results := make([]extraISISResult, 0, len(records))
		for _, record := range records {
			deviceAPK, _ := record.Get("device_a_pk")
			deviceACode, _ := record.Get("device_a_code")
			deviceBPK, _ := record.Get("device_b_pk")
			deviceBCode, _ := record.Get("device_b_code")
			isisMetric, _ := record.Get("isis_metric")
			neighborAddr, _ := record.Get("neighbor_addr")

			results = append(results, extraISISResult{
				deviceAPK:    asString(deviceAPK),
				deviceACode:  asString(deviceACode),
				deviceBPK:    asString(deviceBPK),
				deviceBCode:  asString(deviceBCode),
				isisMetric:   asInt64(isisMetric),
				neighborAddr: asString(neighborAddr),
			})
		}
		return results, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query extra ISIS adjacencies: %w", err)
	}

	extraISIS := extraResult.([]extraISISResult)
	for _, extra := range extraISIS {
		comparison.Discrepancies = append(comparison.Discrepancies, TopologyDiscrepancy{
			Type:        "extra_isis",
			DeviceAPK:   extra.deviceAPK,
			DeviceACode: extra.deviceACode,
			DeviceBPK:   extra.deviceBPK,
			DeviceBCode: extra.deviceBCode,
			ISISMetric:  uint32(extra.isisMetric),
			Details:     fmt.Sprintf("ISIS adjacency exists (neighbor_addr: %s) but no configured link found", extra.neighborAddr),
		})
	}

	// Count total ISIS adjacencies
	countCypher := `MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count`
	countResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, countCypher, nil)
		if err != nil {
			return nil, err
		}
		record, err := res.Single(ctx)
		if err != nil {
			return nil, err
		}
		count, _ := record.Get("count")
		return asInt64(count), nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to count ISIS adjacencies: %w", err)
	}
	comparison.ISISAdjacencies = int(countResult.(int64))

	return comparison, nil
}

// Helper functions for type conversion

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func asBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func parsePathSegments(v any) []PathSegment {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	segments := make([]PathSegment, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		seg := PathSegment{
			Type:      asString(m["type"]),
			PK:        asString(m["pk"]),
			Code:      asString(m["code"]),
			Status:    asString(m["status"]),
			RTTNs:     uint64(asInt64(m["rtt_ns"])),
			Bandwidth: uint64(asInt64(m["bandwidth"])),
			IsDrained: asBool(m["is_drained"]),
		}
		segments = append(segments, seg)
	}
	return segments
}

func parseRouteHops(v any) []RouteHop {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	hops := make([]RouteHop, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hop := RouteHop{
			Type:       asString(m["type"]),
			PK:         asString(m["pk"]),
			Code:       asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
			RTTNs:      uint64(asInt64(m["rtt_ns"])),
			JitterNs:   uint64(asInt64(m["jitter_ns"])),
			Bandwidth:  uint64(asInt64(m["bandwidth"])),
			IsDrained:  asBool(m["is_drained"]),
		}
		hops = append(hops, hop)
	}
	return hops
}

func parseISISDevices(v any) []ISISDevice {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	devices := make([]ISISDevice, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		device := ISISDevice{
			PK:         asString(m["pk"]),
			Code:       asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
			SystemID:   asString(m["system_id"]),
			RouterID:   asString(m["router_id"]),
		}
		devices = append(devices, device)
	}
	return devices
}

func asUint32Slice(v any) []uint32 {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]uint32, 0, len(arr))
	for _, item := range arr {
		result = append(result, uint32(asInt64(item)))
	}
	return result
}
