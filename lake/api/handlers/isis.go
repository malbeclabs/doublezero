package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// ISISNode represents a device node in the ISIS topology graph
type ISISNode struct {
	Data ISISNodeData `json:"data"`
}

type ISISNodeData struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
	MetroPK    string `json:"metroPK,omitempty"`
	SystemID   string `json:"systemId,omitempty"`
	RouterID   string `json:"routerId,omitempty"`
}

// ISISEdge represents an adjacency edge in the ISIS topology graph
type ISISEdge struct {
	Data ISISEdgeData `json:"data"`
}

type ISISEdgeData struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	Target       string   `json:"target"`
	Metric       uint32   `json:"metric,omitempty"`
	AdjSIDs      []uint32 `json:"adjSids,omitempty"`
	NeighborAddr string   `json:"neighborAddr,omitempty"`
}

// ISISTopologyResponse is the response for the ISIS topology endpoint
type ISISTopologyResponse struct {
	Nodes []ISISNode `json:"nodes"`
	Edges []ISISEdge `json:"edges"`
	Error string     `json:"error,omitempty"`
}

// GetISISTopology returns the full ISIS topology graph
func GetISISTopology(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := ISISTopologyResponse{
		Nodes: []ISISNode{},
		Edges: []ISISEdge{},
	}

	// Get devices with ISIS data
	deviceCypher := `
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (d)-[:LOCATED_IN]->(m:Metro)
		RETURN d.pk AS pk,
		       d.code AS code,
		       d.status AS status,
		       d.device_type AS device_type,
		       d.isis_system_id AS system_id,
		       d.isis_router_id AS router_id,
		       m.pk AS metro_pk
	`

	deviceResult, err := session.Run(ctx, deviceCypher, nil)
	if err != nil {
		log.Printf("ISIS topology device query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	deviceRecords, err := deviceResult.Collect(ctx)
	if err != nil {
		log.Printf("ISIS topology device collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range deviceRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		status, _ := record.Get("status")
		deviceType, _ := record.Get("device_type")
		systemID, _ := record.Get("system_id")
		routerID, _ := record.Get("router_id")
		metroPK, _ := record.Get("metro_pk")

		response.Nodes = append(response.Nodes, ISISNode{
			Data: ISISNodeData{
				ID:         asString(pk),
				Label:      asString(code),
				Status:     asString(status),
				DeviceType: asString(deviceType),
				SystemID:   asString(systemID),
				RouterID:   asString(routerID),
				MetroPK:    asString(metroPK),
			},
		})
	}

	// Get all ISIS adjacencies
	adjCypher := `
		MATCH (from:Device)-[r:ISIS_ADJACENT]->(to:Device)
		RETURN from.pk AS from_pk,
		       to.pk AS to_pk,
		       r.metric AS metric,
		       r.neighbor_addr AS neighbor_addr,
		       r.adj_sids AS adj_sids
	`

	adjResult, err := session.Run(ctx, adjCypher, nil)
	if err != nil {
		log.Printf("ISIS topology adjacency query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	adjRecords, err := adjResult.Collect(ctx)
	if err != nil {
		log.Printf("ISIS topology adjacency collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range adjRecords {
		fromPK, _ := record.Get("from_pk")
		toPK, _ := record.Get("to_pk")
		metric, _ := record.Get("metric")
		neighborAddr, _ := record.Get("neighbor_addr")
		adjSids, _ := record.Get("adj_sids")

		response.Edges = append(response.Edges, ISISEdge{
			Data: ISISEdgeData{
				ID:           asString(fromPK) + "->" + asString(toPK),
				Source:       asString(fromPK),
				Target:       asString(toPK),
				Metric:       uint32(asInt64(metric)),
				NeighborAddr: asString(neighborAddr),
				AdjSIDs:      asUint32Slice(adjSids),
			},
		})
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil) // Reuse existing metric for now

	writeJSON(w, response)
}

// Helper functions

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// PathHop represents a hop in a path
type PathHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
}

// PathResponse is the response for the path endpoint
type PathResponse struct {
	Path        []PathHop `json:"path"`
	TotalMetric uint32    `json:"totalMetric"`
	HopCount    int       `json:"hopCount"`
	Error       string    `json:"error,omitempty"`
}

// GetISISPath finds the shortest path between two devices using ISIS metrics
func GetISISPath(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	mode := r.URL.Query().Get("mode") // "hops" or "latency"

	if fromPK == "" || toPK == "" {
		writeJSON(w, PathResponse{Error: "from and to parameters are required"})
		return
	}

	if fromPK == toPK {
		writeJSON(w, PathResponse{Error: "from and to must be different devices"})
		return
	}

	if mode == "" {
		mode = "hops" // default to fewest hops
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	var cypher string
	if mode == "latency" {
		// Use APOC Dijkstra for weighted shortest path (lowest total metric)
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			CALL apoc.algo.dijkstra(a, b, 'ISIS_ADJACENT>', 'metric') YIELD path, weight
			RETURN [n IN nodes(path) | {
				pk: n.pk,
				code: n.code,
				status: n.status,
				device_type: n.device_type
			}] AS devices,
			weight AS total_metric
		`
	} else {
		// Default: fewest hops using shortestPath
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			MATCH path = shortestPath((a)-[:ISIS_ADJACENT*]->(b))
			WITH path, reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS total_metric
			RETURN [n IN nodes(path) | {
				pk: n.pk,
				code: n.code,
				status: n.status,
				device_type: n.device_type
			}] AS devices,
			total_metric
		`
	}

	result, err := session.Run(ctx, cypher, map[string]any{
		"from_pk": fromPK,
		"to_pk":   toPK,
	})
	if err != nil {
		log.Printf("ISIS path query error: %v", err)
		writeJSON(w, PathResponse{Error: "Failed to find path: " + err.Error()})
		return
	}

	record, err := result.Single(ctx)
	if err != nil {
		log.Printf("ISIS path no result: %v", err)
		writeJSON(w, PathResponse{Error: "No path found between devices"})
		return
	}

	devicesVal, _ := record.Get("devices")
	totalMetric, _ := record.Get("total_metric")

	path := parsePathHops(devicesVal)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, PathResponse{
		Path:        path,
		TotalMetric: uint32(asInt64(totalMetric)),
		HopCount:    len(path) - 1,
	})
}

func parsePathHops(v any) []PathHop {
	if v == nil {
		return []PathHop{}
	}
	arr, ok := v.([]any)
	if !ok {
		return []PathHop{}
	}
	hops := make([]PathHop, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hops = append(hops, PathHop{
			DevicePK:   asString(m["pk"]),
			DeviceCode: asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
		})
	}
	return hops
}

// TopologyDiscrepancy represents a mismatch between configured and ISIS topology
type TopologyDiscrepancy struct {
	Type            string `json:"type"` // "missing_isis", "extra_isis", "metric_mismatch"
	LinkPK          string `json:"linkPK,omitempty"`
	LinkCode        string `json:"linkCode,omitempty"`
	DeviceAPK       string `json:"deviceAPK"`
	DeviceACode     string `json:"deviceACode"`
	DeviceBPK       string `json:"deviceBPK"`
	DeviceBCode     string `json:"deviceBCode"`
	ConfiguredRTTUs uint64 `json:"configuredRttUs,omitempty"`
	ISISMetric      uint32 `json:"isisMetric,omitempty"`
	Details         string `json:"details"`
}

// TopologyCompareResponse is the response for the topology compare endpoint
type TopologyCompareResponse struct {
	ConfiguredLinks int                   `json:"configuredLinks"`
	ISISAdjacencies int                   `json:"isisAdjacencies"`
	MatchedLinks    int                   `json:"matchedLinks"`
	Discrepancies   []TopologyDiscrepancy `json:"discrepancies"`
	Error           string                `json:"error,omitempty"`
}

// GetTopologyCompare compares configured links vs ISIS adjacencies
func GetTopologyCompare(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := TopologyCompareResponse{
		Discrepancies: []TopologyDiscrepancy{},
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

	configuredResult, err := session.Run(ctx, configuredCypher, nil)
	if err != nil {
		log.Printf("Topology compare configured query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	configuredRecords, err := configuredResult.Collect(ctx)
	if err != nil {
		log.Printf("Topology compare configured collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	response.ConfiguredLinks = len(configuredRecords)

	for _, record := range configuredRecords {
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

		hasForward := asBool(hasForwardAdj)
		hasReverse := asBool(hasReverseAdj)
		status := asString(linkStatus)

		if hasForward || hasReverse {
			response.MatchedLinks++
		}

		// Check for missing ISIS adjacencies on active links
		if status == "active" && !hasForward && !hasReverse {
			response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      asString(linkPK),
				LinkCode:    asString(linkCode),
				DeviceAPK:   asString(deviceAPK),
				DeviceACode: asString(deviceACode),
				DeviceBPK:   asString(deviceBPK),
				DeviceBCode: asString(deviceBCode),
				Details:     "Active link has no ISIS adjacency in either direction",
			})
		} else if status == "active" && hasForward != hasReverse {
			direction := "forward only"
			if hasReverse && !hasForward {
				direction = "reverse only"
			}
			response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      asString(linkPK),
				LinkCode:    asString(linkCode),
				DeviceAPK:   asString(deviceAPK),
				DeviceACode: asString(deviceACode),
				DeviceBPK:   asString(deviceBPK),
				DeviceBCode: asString(deviceBCode),
				Details:     "ISIS adjacency is " + direction + " (should be bidirectional)",
			})
		}

		// Check for metric mismatch
		configRTTNs := asInt64(configuredRTTNs)
		isisMetric := asInt64(isisMetricForward)
		if hasForward && configRTTNs > 0 && isisMetric > 0 {
			configRTTUs := uint64(configRTTNs) / 1000
			if configRTTUs > 0 {
				ratio := float64(isisMetric) / float64(configRTTUs)
				if ratio < 0.5 || ratio > 2.0 {
					response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
						Type:            "metric_mismatch",
						LinkPK:          asString(linkPK),
						LinkCode:        asString(linkCode),
						DeviceAPK:       asString(deviceAPK),
						DeviceACode:     asString(deviceACode),
						DeviceBPK:       asString(deviceBPK),
						DeviceBCode:     asString(deviceBCode),
						ConfiguredRTTUs: configRTTUs,
						ISISMetric:      uint32(isisMetric),
						Details:         "ISIS metric differs significantly from configured RTT",
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

	extraResult, err := session.Run(ctx, extraCypher, nil)
	if err != nil {
		log.Printf("Topology compare extra query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	extraRecords, err := extraResult.Collect(ctx)
	if err != nil {
		log.Printf("Topology compare extra collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range extraRecords {
		deviceAPK, _ := record.Get("device_a_pk")
		deviceACode, _ := record.Get("device_a_code")
		deviceBPK, _ := record.Get("device_b_pk")
		deviceBCode, _ := record.Get("device_b_code")
		isisMetric, _ := record.Get("isis_metric")
		neighborAddr, _ := record.Get("neighbor_addr")

		response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
			Type:        "extra_isis",
			DeviceAPK:   asString(deviceAPK),
			DeviceACode: asString(deviceACode),
			DeviceBPK:   asString(deviceBPK),
			DeviceBCode: asString(deviceBCode),
			ISISMetric:  uint32(asInt64(isisMetric)),
			Details:     "ISIS adjacency exists (neighbor: " + asString(neighborAddr) + ") but no configured link found",
		})
	}

	// Count total ISIS adjacencies
	countCypher := `MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count`
	countResult, err := session.Run(ctx, countCypher, nil)
	if err != nil {
		log.Printf("Topology compare count query error: %v", err)
	} else {
		if countRecord, err := countResult.Single(ctx); err == nil {
			count, _ := countRecord.Get("count")
			response.ISISAdjacencies = int(asInt64(count))
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, response)
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

// ImpactDevice represents a device that would be affected by a failure
type ImpactDevice struct {
	PK         string `json:"pk"`
	Code       string `json:"code"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
}

// FailureImpactResponse is the response for the failure impact endpoint
type FailureImpactResponse struct {
	DevicePK           string         `json:"devicePK"`
	DeviceCode         string         `json:"deviceCode"`
	UnreachableDevices []ImpactDevice `json:"unreachableDevices"`
	UnreachableCount   int            `json:"unreachableCount"`
	Error              string         `json:"error,omitempty"`
}

// GetFailureImpact returns devices that would become unreachable if a device goes down
func GetFailureImpact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Get device PK from URL path
	devicePK := r.PathValue("pk")
	if devicePK == "" {
		writeJSON(w, FailureImpactResponse{Error: "device pk is required"})
		return
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := FailureImpactResponse{
		DevicePK:           devicePK,
		UnreachableDevices: []ImpactDevice{},
	}

	// First get the device code
	deviceCypher := `MATCH (d:Device {pk: $pk}) RETURN d.code AS code`
	deviceResult, err := session.Run(ctx, deviceCypher, map[string]any{"pk": devicePK})
	if err != nil {
		log.Printf("Failure impact device query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}
	if deviceRecord, err := deviceResult.Single(ctx); err == nil {
		code, _ := deviceRecord.Get("code")
		response.DeviceCode = asString(code)
	}

	// Find devices that would become unreachable if this device goes down
	// Strategy: Find a reference device (most connected, not the target), then find all devices
	// reachable from it without going through the target. Unreachable = ISIS devices not in that set.
	impactCypher := `
		// First, find a good reference device (most ISIS adjacencies, not the target)
		MATCH (ref:Device)-[:ISIS_ADJACENT]-()
		WHERE ref.pk <> $device_pk AND ref.isis_system_id IS NOT NULL
		WITH ref, count(*) AS adjCount
		ORDER BY adjCount DESC
		LIMIT 1

		// Find all devices reachable from reference without going through target
		CALL {
			WITH ref
			MATCH (target:Device {pk: $device_pk})
			MATCH path = (ref)-[:ISIS_ADJACENT*0..20]-(reachable:Device)
			WHERE reachable.isis_system_id IS NOT NULL
			  AND NONE(n IN nodes(path) WHERE n.pk = $device_pk)
			RETURN DISTINCT reachable
		}

		// Find all ISIS devices
		WITH collect(reachable.pk) AS reachablePKs
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		  AND d.pk <> $device_pk
		  AND NOT d.pk IN reachablePKs
		RETURN d.pk AS pk,
		       d.code AS code,
		       d.status AS status,
		       d.device_type AS device_type
	`

	impactResult, err := session.Run(ctx, impactCypher, map[string]any{
		"device_pk": devicePK,
	})
	if err != nil {
		log.Printf("Failure impact query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	impactRecords, err := impactResult.Collect(ctx)
	if err != nil {
		log.Printf("Failure impact collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range impactRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		status, _ := record.Get("status")
		deviceType, _ := record.Get("device_type")

		response.UnreachableDevices = append(response.UnreachableDevices, ImpactDevice{
			PK:         asString(pk),
			Code:       asString(code),
			Status:     asString(status),
			DeviceType: asString(deviceType),
		})
	}

	response.UnreachableCount = len(response.UnreachableDevices)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, response)
}

// MultiPathHop represents a hop in a path with edge metric information
type MultiPathHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
	EdgeMetric uint32 `json:"edgeMetric,omitempty"` // metric to reach this hop from previous
}

// SinglePath represents one path in a multi-path response
type SinglePath struct {
	Path        []MultiPathHop `json:"path"`
	TotalMetric uint32         `json:"totalMetric"`
	HopCount    int            `json:"hopCount"`
}

// MultiPathResponse is the response for the K-shortest paths endpoint
type MultiPathResponse struct {
	Paths []SinglePath `json:"paths"`
	From  string       `json:"from"`
	To    string       `json:"to"`
	Error string       `json:"error,omitempty"`
}

// GetISISPaths finds K-shortest paths between two devices
func GetISISPaths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	kStr := r.URL.Query().Get("k")

	if fromPK == "" || toPK == "" {
		writeJSON(w, MultiPathResponse{Error: "from and to parameters are required"})
		return
	}

	if fromPK == toPK {
		writeJSON(w, MultiPathResponse{Error: "from and to must be different devices"})
		return
	}

	k := 5 // default
	if kStr != "" {
		if parsed, err := strconv.Atoi(kStr); err == nil && parsed > 0 && parsed <= 10 {
			k = parsed
		}
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MultiPathResponse{
		From:  fromPK,
		To:    toPK,
		Paths: []SinglePath{},
	}

	// Find K-shortest paths by total metric
	// Uses allShortestPaths to get multiple equal-cost paths, plus some longer alternatives
	cypher := `
		MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})

		// First get all shortest paths (equal cost)
		CALL {
			WITH a, b
			MATCH path = allShortestPaths((a)-[:ISIS_ADJACENT*]->(b))
			RETURN path,
			       reduce(cost = 0, r IN relationships(path) | cost + coalesce(r.metric, 1)) AS totalMetric
		}

		WITH path, totalMetric
		ORDER BY totalMetric
		LIMIT 50

		WITH path, totalMetric,
		     [n IN nodes(path) | {
		       pk: n.pk,
		       code: n.code,
		       status: n.status,
		       device_type: n.device_type
		     }] AS nodeList,
		     [r IN relationships(path) | r.metric] AS edgeMetrics
		RETURN nodeList, edgeMetrics, totalMetric
	`

	result, err := session.Run(ctx, cypher, map[string]any{
		"from_pk": fromPK,
		"to_pk":   toPK,
	})
	if err != nil {
		log.Printf("ISIS multi-path query error: %v", err)
		response.Error = "Failed to find paths: " + err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("ISIS multi-path collect error: %v", err)
		response.Error = "Failed to collect paths: " + err.Error()
		writeJSON(w, response)
		return
	}

	if len(records) == 0 {
		response.Error = "No paths found between devices"
		writeJSON(w, response)
		return
	}

	// Track unique paths to avoid duplicates
	seenPaths := make(map[string]bool)

	for _, record := range records {
		nodeListVal, _ := record.Get("nodeList")
		edgeMetricsVal, _ := record.Get("edgeMetrics")
		totalMetric, _ := record.Get("totalMetric")

		hops := parseNodeListWithMetrics(nodeListVal, edgeMetricsVal)
		if len(hops) == 0 {
			continue
		}

		// Create a key for deduplication based on the path's device PKs
		pathKey := ""
		for _, hop := range hops {
			pathKey += hop.DevicePK + ","
		}

		if seenPaths[pathKey] {
			continue
		}
		seenPaths[pathKey] = true

		response.Paths = append(response.Paths, SinglePath{
			Path:        hops,
			TotalMetric: uint32(asInt64(totalMetric)),
			HopCount:    len(hops) - 1,
		})

		if len(response.Paths) >= k {
			break
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)
	log.Printf("ISIS multi-path query returned %d paths in %v", len(response.Paths), duration)

	writeJSON(w, response)
}

func parseNodeListWithMetrics(nodeListVal, edgeMetricsVal any) []MultiPathHop {
	if nodeListVal == nil {
		return []MultiPathHop{}
	}
	nodeArr, ok := nodeListVal.([]any)
	if !ok {
		return []MultiPathHop{}
	}

	// Parse edge metrics
	var edgeMetrics []int64
	if edgeMetricsVal != nil {
		if metricsArr, ok := edgeMetricsVal.([]any); ok {
			for _, m := range metricsArr {
				edgeMetrics = append(edgeMetrics, asInt64(m))
			}
		}
	}

	hops := make([]MultiPathHop, 0, len(nodeArr))
	for i, item := range nodeArr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		hop := MultiPathHop{
			DevicePK:   asString(m["pk"]),
			DeviceCode: asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
		}

		// Edge metric is the metric to reach this hop from the previous one
		// So hop[i] uses edgeMetrics[i-1]
		if i > 0 && i-1 < len(edgeMetrics) {
			hop.EdgeMetric = uint32(edgeMetrics[i-1])
		}

		hops = append(hops, hop)
	}
	return hops
}

// CriticalLink represents a link that is critical for network connectivity
type CriticalLink struct {
	SourcePK   string `json:"sourcePK"`
	SourceCode string `json:"sourceCode"`
	TargetPK   string `json:"targetPK"`
	TargetCode string `json:"targetCode"`
	Metric     uint32 `json:"metric"`
	Criticality string `json:"criticality"` // "critical", "important", "redundant"
}

// CriticalLinksResponse is the response for the critical links endpoint
type CriticalLinksResponse struct {
	Links []CriticalLink `json:"links"`
	Error string         `json:"error,omitempty"`
}

// GetCriticalLinks returns links that are critical for network connectivity
// Critical links are identified based on node degrees and connectivity patterns
func GetCriticalLinks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := CriticalLinksResponse{
		Links: []CriticalLink{},
	}

	// Efficient approach: for each edge, check the degree of both endpoints
	// - If either endpoint has degree 1, this is a critical link (leaf edge)
	// - If min(degreeA, degreeB) == 2, it's important (limited redundancy)
	// - Otherwise it's redundant (well-connected)
	cypher := `
		MATCH (a:Device)-[r:ISIS_ADJACENT]-(b:Device)
		WHERE a.isis_system_id IS NOT NULL
		  AND b.isis_system_id IS NOT NULL
		  AND id(a) < id(b)
		WITH a, b, min(r.metric) AS metric  // Deduplicate multiple edges between same nodes
		// Count neighbors for each endpoint
		OPTIONAL MATCH (a)-[:ISIS_ADJACENT]-(na:Device)
		WHERE na.isis_system_id IS NOT NULL
		WITH a, b, metric, count(DISTINCT na) AS degreeA
		OPTIONAL MATCH (b)-[:ISIS_ADJACENT]-(nb:Device)
		WHERE nb.isis_system_id IS NOT NULL
		WITH a, b, metric, degreeA, count(DISTINCT nb) AS degreeB
		RETURN a.pk AS sourcePK,
		       a.code AS sourceCode,
		       b.pk AS targetPK,
		       b.code AS targetCode,
		       metric,
		       degreeA,
		       degreeB
		ORDER BY CASE
		  WHEN degreeA = 1 OR degreeB = 1 THEN 0
		  WHEN degreeA = 2 OR degreeB = 2 THEN 1
		  ELSE 2
		END, metric DESC
	`

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		log.Printf("Critical links query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("Critical links collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range records {
		sourcePK, _ := record.Get("sourcePK")
		sourceCode, _ := record.Get("sourceCode")
		targetPK, _ := record.Get("targetPK")
		targetCode, _ := record.Get("targetCode")
		metric, _ := record.Get("metric")
		degreeA, _ := record.Get("degreeA")
		degreeB, _ := record.Get("degreeB")

		dA := asInt64(degreeA)
		dB := asInt64(degreeB)
		minDegree := dA
		if dB < dA {
			minDegree = dB
		}

		// Determine criticality based on minimum degree
		var criticality string
		if minDegree <= 1 {
			criticality = "critical" // At least one endpoint has only this connection
		} else if minDegree == 2 {
			criticality = "important" // Limited redundancy
		} else {
			criticality = "redundant" // Well-connected endpoints
		}

		response.Links = append(response.Links, CriticalLink{
			SourcePK:    asString(sourcePK),
			SourceCode:  asString(sourceCode),
			TargetPK:    asString(targetPK),
			TargetCode:  asString(targetCode),
			Metric:      uint32(asInt64(metric)),
			Criticality: criticality,
		})
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	criticalCount := 0
	importantCount := 0
	for _, link := range response.Links {
		if link.Criticality == "critical" {
			criticalCount++
		} else if link.Criticality == "important" {
			importantCount++
		}
	}
	log.Printf("Critical links query returned %d links (%d critical, %d important) in %v",
		len(response.Links), criticalCount, importantCount, duration)

	writeJSON(w, response)
}

// RedundancyIssue represents a single redundancy issue in the network
type RedundancyIssue struct {
	Type        string `json:"type"`        // "leaf_device", "critical_link", "single_exit_metro", "no_backup_device"
	Severity    string `json:"severity"`    // "critical", "warning", "info"
	EntityPK    string `json:"entityPK"`    // PK of affected entity
	EntityCode  string `json:"entityCode"`  // Code/name of affected entity
	EntityType  string `json:"entityType"`  // "device", "link", "metro"
	Description string `json:"description"` // Human-readable description
	Impact      string `json:"impact"`      // Impact description
	// Extra fields for links
	TargetPK   string `json:"targetPK,omitempty"`
	TargetCode string `json:"targetCode,omitempty"`
	// Extra fields for context
	MetroPK   string `json:"metroPK,omitempty"`
	MetroCode string `json:"metroCode,omitempty"`
}

// RedundancyReportResponse is the response for the redundancy report endpoint
type RedundancyReportResponse struct {
	Issues         []RedundancyIssue `json:"issues"`
	Summary        RedundancySummary `json:"summary"`
	Error          string            `json:"error,omitempty"`
}

type RedundancySummary struct {
	TotalIssues     int `json:"totalIssues"`
	CriticalCount   int `json:"criticalCount"`
	WarningCount    int `json:"warningCount"`
	InfoCount       int `json:"infoCount"`
	LeafDevices     int `json:"leafDevices"`
	CriticalLinks   int `json:"criticalLinks"`
	SingleExitMetros int `json:"singleExitMetros"`
}

// GetRedundancyReport returns a comprehensive redundancy analysis report
func GetRedundancyReport(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := RedundancyReportResponse{
		Issues: []RedundancyIssue{},
	}

	// 1. Find leaf devices (devices with only 1 ISIS neighbor)
	leafCypher := `
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (d)-[:ISIS_ADJACENT]-(n:Device)
		WHERE n.isis_system_id IS NOT NULL
		WITH d, count(DISTINCT n) AS neighborCount
		WHERE neighborCount = 1
		OPTIONAL MATCH (d)-[:LOCATED_IN]->(m:Metro)
		RETURN d.pk AS pk,
		       d.code AS code,
		       m.pk AS metroPK,
		       m.code AS metroCode
		ORDER BY d.code
	`

	leafResult, err := session.Run(ctx, leafCypher, nil)
	if err != nil {
		log.Printf("Redundancy report leaf devices query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	leafRecords, err := leafResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report leaf devices collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range leafRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		metroPK, _ := record.Get("metroPK")
		metroCode, _ := record.Get("metroCode")

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "leaf_device",
			Severity:    "critical",
			EntityPK:    asString(pk),
			EntityCode:  asString(code),
			EntityType:  "device",
			Description: "Device has only one ISIS neighbor",
			Impact:      "If the single neighbor fails, this device loses connectivity to the network",
			MetroPK:     asString(metroPK),
			MetroCode:   asString(metroCode),
		})
	}

	// 2. Find critical links (links where at least one endpoint has only this connection)
	criticalLinksCypher := `
		MATCH (a:Device)-[r:ISIS_ADJACENT]-(b:Device)
		WHERE a.isis_system_id IS NOT NULL
		  AND b.isis_system_id IS NOT NULL
		  AND id(a) < id(b)
		WITH a, b, min(r.metric) AS metric
		OPTIONAL MATCH (a)-[:ISIS_ADJACENT]-(na:Device)
		WHERE na.isis_system_id IS NOT NULL
		WITH a, b, metric, count(DISTINCT na) AS degreeA
		OPTIONAL MATCH (b)-[:ISIS_ADJACENT]-(nb:Device)
		WHERE nb.isis_system_id IS NOT NULL
		WITH a, b, metric, degreeA, count(DISTINCT nb) AS degreeB
		WHERE degreeA = 1 OR degreeB = 1
		RETURN a.pk AS sourcePK,
		       a.code AS sourceCode,
		       b.pk AS targetPK,
		       b.code AS targetCode,
		       degreeA,
		       degreeB
		ORDER BY sourceCode
	`

	criticalResult, err := session.Run(ctx, criticalLinksCypher, nil)
	if err != nil {
		log.Printf("Redundancy report critical links query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	criticalRecords, err := criticalResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report critical links collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range criticalRecords {
		sourcePK, _ := record.Get("sourcePK")
		sourceCode, _ := record.Get("sourceCode")
		targetPK, _ := record.Get("targetPK")
		targetCode, _ := record.Get("targetCode")

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "critical_link",
			Severity:    "critical",
			EntityPK:    asString(sourcePK),
			EntityCode:  asString(sourceCode),
			EntityType:  "link",
			TargetPK:    asString(targetPK),
			TargetCode:  asString(targetCode),
			Description: "Link connects a leaf device to the network",
			Impact:      "If this link fails, one or both devices lose network connectivity",
		})
	}

	// 3. Find single-exit metros (metros where only one device has external connections)
	singleExitCypher := `
		MATCH (m:Metro)<-[:LOCATED_IN]-(d:Device)
		WHERE d.isis_system_id IS NOT NULL
		MATCH (d)-[:ISIS_ADJACENT]-(n:Device)
		WHERE n.isis_system_id IS NOT NULL
		OPTIONAL MATCH (n)-[:LOCATED_IN]->(nm:Metro)
		WITH m, d, n, nm
		WHERE nm IS NULL OR nm.pk <> m.pk
		WITH m, count(DISTINCT d) AS exitDeviceCount
		WHERE exitDeviceCount = 1
		RETURN m.pk AS pk,
		       m.code AS code,
		       m.name AS name
		ORDER BY m.code
	`

	singleExitResult, err := session.Run(ctx, singleExitCypher, nil)
	if err != nil {
		log.Printf("Redundancy report single-exit metros query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	singleExitRecords, err := singleExitResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report single-exit metros collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range singleExitRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		name, _ := record.Get("name")

		displayName := asString(name)
		if displayName == "" {
			displayName = asString(code)
		}

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "single_exit_metro",
			Severity:    "warning",
			EntityPK:    asString(pk),
			EntityCode:  displayName,
			EntityType:  "metro",
			Description: "Metro has only one device with external connections",
			Impact:      "If that device fails, the entire metro loses external connectivity",
		})
	}

	// Build summary
	criticalCount := 0
	warningCount := 0
	infoCount := 0
	leafDeviceCount := 0
	criticalLinkCount := 0
	singleExitMetroCount := 0

	for _, issue := range response.Issues {
		switch issue.Severity {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}

		switch issue.Type {
		case "leaf_device":
			leafDeviceCount++
		case "critical_link":
			criticalLinkCount++
		case "single_exit_metro":
			singleExitMetroCount++
		}
	}

	response.Summary = RedundancySummary{
		TotalIssues:      len(response.Issues),
		CriticalCount:    criticalCount,
		WarningCount:     warningCount,
		InfoCount:        infoCount,
		LeafDevices:      leafDeviceCount,
		CriticalLinks:    criticalLinkCount,
		SingleExitMetros: singleExitMetroCount,
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Redundancy report returned %d issues (%d critical, %d warning, %d info) in %v",
		len(response.Issues), criticalCount, warningCount, infoCount, duration)

	writeJSON(w, response)
}

// MetroConnectivity represents connectivity between two metros
type MetroConnectivity struct {
	FromMetroPK   string `json:"fromMetroPK"`
	FromMetroCode string `json:"fromMetroCode"`
	FromMetroName string `json:"fromMetroName"`
	ToMetroPK     string `json:"toMetroPK"`
	ToMetroCode   string `json:"toMetroCode"`
	ToMetroName   string `json:"toMetroName"`
	PathCount     int    `json:"pathCount"`
	MinHops       int    `json:"minHops"`
	MinMetric     int64  `json:"minMetric"`
}

// MetroConnectivityResponse is the response for the metro connectivity endpoint
type MetroConnectivityResponse struct {
	Metros       []MetroInfo         `json:"metros"`
	Connectivity []MetroConnectivity `json:"connectivity"`
	Error        string              `json:"error,omitempty"`
}

// MetroInfo is a lightweight metro representation for the matrix
type MetroInfo struct {
	PK   string `json:"pk"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// GetMetroConnectivity returns the connectivity matrix between all metros
func GetMetroConnectivity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MetroConnectivityResponse{
		Metros:       []MetroInfo{},
		Connectivity: []MetroConnectivity{},
	}

	// First, get all metros that have ISIS-enabled devices
	metroCypher := `
		MATCH (m:Metro)<-[:LOCATED_IN]-(d:Device)
		WHERE d.isis_system_id IS NOT NULL
		WITH m, count(d) AS deviceCount
		WHERE deviceCount > 0
		RETURN m.pk AS pk, m.code AS code, m.name AS name
		ORDER BY m.code
	`

	metroResult, err := session.Run(ctx, metroCypher, nil)
	if err != nil {
		log.Printf("Metro connectivity metro query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	metroRecords, err := metroResult.Collect(ctx)
	if err != nil {
		log.Printf("Metro connectivity metro collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	metroMap := make(map[string]MetroInfo)
	for _, record := range metroRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		name, _ := record.Get("name")

		metro := MetroInfo{
			PK:   asString(pk),
			Code: asString(code),
			Name: asString(name),
		}
		response.Metros = append(response.Metros, metro)
		metroMap[metro.PK] = metro
	}

	// For each pair of metros, find the best path between any devices in those metros
	// This query finds the shortest path between any two ISIS devices in different metros
	connectivityCypher := `
		MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
		MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
		WHERE m1.pk < m2.pk
		  AND d1.isis_system_id IS NOT NULL
		  AND d2.isis_system_id IS NOT NULL
		WITH m1, m2, d1, d2
		MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
		WITH m1, m2,
		     length(path) AS hops,
		     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric
		WITH m1, m2, min(hops) AS minHops, min(metric) AS minMetric, count(*) AS pathCount
		RETURN m1.pk AS fromPK, m1.code AS fromCode, m1.name AS fromName,
		       m2.pk AS toPK, m2.code AS toCode, m2.name AS toName,
		       minHops, minMetric, pathCount
		ORDER BY fromCode, toCode
	`

	connResult, err := session.Run(ctx, connectivityCypher, nil)
	if err != nil {
		log.Printf("Metro connectivity query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	connRecords, err := connResult.Collect(ctx)
	if err != nil {
		log.Printf("Metro connectivity collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range connRecords {
		fromPK, _ := record.Get("fromPK")
		fromCode, _ := record.Get("fromCode")
		fromName, _ := record.Get("fromName")
		toPK, _ := record.Get("toPK")
		toCode, _ := record.Get("toCode")
		toName, _ := record.Get("toName")
		minHops, _ := record.Get("minHops")
		minMetric, _ := record.Get("minMetric")
		pathCount, _ := record.Get("pathCount")

		// Add both directions (matrix is symmetric)
		conn := MetroConnectivity{
			FromMetroPK:   asString(fromPK),
			FromMetroCode: asString(fromCode),
			FromMetroName: asString(fromName),
			ToMetroPK:     asString(toPK),
			ToMetroCode:   asString(toCode),
			ToMetroName:   asString(toName),
			PathCount:     int(asInt64(pathCount)),
			MinHops:       int(asInt64(minHops)),
			MinMetric:     asInt64(minMetric),
		}
		response.Connectivity = append(response.Connectivity, conn)

		// Add reverse direction
		connReverse := MetroConnectivity{
			FromMetroPK:   asString(toPK),
			FromMetroCode: asString(toCode),
			FromMetroName: asString(toName),
			ToMetroPK:     asString(fromPK),
			ToMetroCode:   asString(fromCode),
			ToMetroName:   asString(fromName),
			PathCount:     int(asInt64(pathCount)),
			MinHops:       int(asInt64(minHops)),
			MinMetric:     asInt64(minMetric),
		}
		response.Connectivity = append(response.Connectivity, connReverse)
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil) // Reuse existing metric for now

	log.Printf("Metro connectivity returned %d metros, %d connections in %v",
		len(response.Metros), len(response.Connectivity), duration)

	writeJSON(w, response)
}

// MaintenanceImpactRequest is the request body for maintenance impact analysis
type MaintenanceImpactRequest struct {
	Devices []string `json:"devices"` // Device PKs to take offline
	Links   []string `json:"links"`   // Link PKs to take offline (as "sourcePK:targetPK")
}

// MaintenanceItem represents a device or link being taken offline
type MaintenanceItem struct {
	Type            string   `json:"type"`            // "device" or "link"
	PK              string   `json:"pk"`              // Device PK or link PK
	Code            string   `json:"code"`            // Device code or "sourceCode - targetCode"
	Impact          int      `json:"impact"`          // Number of affected paths/devices
	Disconnected    int      `json:"disconnected"`    // Devices that would lose connectivity
	CausesPartition bool     `json:"causesPartition"` // Would this cause a network partition?
	DisconnectedDevices []string `json:"disconnectedDevices,omitempty"` // Device codes that would be disconnected
}

// MaintenanceAffectedPath represents a path that would be impacted by maintenance
type MaintenanceAffectedPath struct {
	Source       string `json:"source"`       // Source device code
	Target       string `json:"target"`       // Target device code
	SourceMetro  string `json:"sourceMetro"`  // Source metro code
	TargetMetro  string `json:"targetMetro"`  // Target metro code
	HopsBefore   int    `json:"hopsBefore"`   // Hops before maintenance
	HopsAfter    int    `json:"hopsAfter"`    // Hops after maintenance (-1 = disconnected)
	MetricBefore int    `json:"metricBefore"` // Total ISIS metric before
	MetricAfter  int    `json:"metricAfter"`  // Total ISIS metric after (-1 = disconnected)
	Status       string `json:"status"`       // "rerouted", "degraded", or "disconnected"
}

// AffectedMetroPair represents connectivity impact between two metros
type AffectedMetroPair struct {
	SourceMetro string `json:"sourceMetro"`
	TargetMetro string `json:"targetMetro"`
	PathsBefore int    `json:"pathsBefore"` // Number of paths before
	PathsAfter  int    `json:"pathsAfter"`  // Number of paths after
	Status      string `json:"status"`      // "reduced", "degraded", or "disconnected"
}

// MaintenanceImpactResponse is the response for maintenance impact analysis
type MaintenanceImpactResponse struct {
	Items             []MaintenanceItem           `json:"items"`                       // Items with their individual impacts
	TotalImpact       int                         `json:"totalImpact"`                 // Total affected paths when all items are down
	TotalDisconnected int                         `json:"totalDisconnected"`           // Total devices that lose connectivity
	RecommendedOrder  []string                    `json:"recommendedOrder"`            // PKs in recommended maintenance order (least impact first)
	AffectedPaths     []MaintenanceAffectedPath   `json:"affectedPaths,omitempty"`     // Sample of affected paths
	AffectedMetros    []AffectedMetroPair         `json:"affectedMetros,omitempty"`    // Affected metro pairs
	DisconnectedList  []string                    `json:"disconnectedList,omitempty"`  // All devices that would be disconnected
	Error             string                      `json:"error,omitempty"`
}

// PostMaintenanceImpact analyzes the impact of taking multiple devices/links offline
func PostMaintenanceImpact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	// Parse request body
	var req MaintenanceImpactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, MaintenanceImpactResponse{Error: "Invalid request body: " + err.Error()})
		return
	}

	if len(req.Devices) == 0 && len(req.Links) == 0 {
		writeJSON(w, MaintenanceImpactResponse{Error: "No devices or links specified"})
		return
	}

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MaintenanceImpactResponse{
		Items:            []MaintenanceItem{},
		RecommendedOrder: []string{},
		AffectedPaths:    []MaintenanceAffectedPath{},
		AffectedMetros:   []AffectedMetroPair{},
		DisconnectedList: []string{},
	}

	// Collect all device PKs and link endpoints being taken offline
	offlineDevicePKs := make(map[string]bool)
	offlineLinkEndpoints := make(map[string]bool) // "sourcePK:targetPK" format

	for _, pk := range req.Devices {
		offlineDevicePKs[pk] = true
	}

	// Analyze each device
	for _, devicePK := range req.Devices {
		item := analyzeDeviceImpact(ctx, session, devicePK)
		response.Items = append(response.Items, item)
		// Collect disconnected devices
		for _, dc := range item.DisconnectedDevices {
			response.DisconnectedList = append(response.DisconnectedList, dc)
		}
	}

	// Analyze each link - need to look up endpoints first
	for _, linkPK := range req.Links {
		item := analyzeLinkImpact(ctx, session, linkPK)
		response.Items = append(response.Items, item)
		// Collect disconnected devices
		for _, dc := range item.DisconnectedDevices {
			response.DisconnectedList = append(response.DisconnectedList, dc)
		}
		// Track link endpoints for path analysis
		endpoints := getLinkEndpoints(ctx, linkPK)
		if endpoints != "" {
			offlineLinkEndpoints[endpoints] = true
		}
	}

	// Sort items by impact (ascending) for recommended order
	sortedItems := make([]MaintenanceItem, len(response.Items))
	copy(sortedItems, response.Items)

	// Simple bubble sort by impact (least impactful first)
	for i := 0; i < len(sortedItems)-1; i++ {
		for j := 0; j < len(sortedItems)-i-1; j++ {
			if sortedItems[j].Impact > sortedItems[j+1].Impact {
				sortedItems[j], sortedItems[j+1] = sortedItems[j+1], sortedItems[j]
			}
		}
	}

	// Build recommended order
	for _, item := range sortedItems {
		response.RecommendedOrder = append(response.RecommendedOrder, item.PK)
	}

	// Calculate total impact
	for _, item := range response.Items {
		response.TotalImpact += item.Impact
		response.TotalDisconnected += item.Disconnected
	}

	// Compute affected paths (sample of top 10 most impacted paths)
	response.AffectedPaths = computeAffectedPaths(ctx, session, offlineDevicePKs, offlineLinkEndpoints, 10)

	// Compute affected metro pairs
	response.AffectedMetros = computeAffectedMetros(ctx, session, offlineDevicePKs, offlineLinkEndpoints)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Maintenance impact analyzed %d devices, %d links in %v",
		len(req.Devices), len(req.Links), duration)

	writeJSON(w, response)
}

// getLinkEndpoints returns "sourcePK:targetPK" for a link PK
func getLinkEndpoints(ctx context.Context, linkPK string) string {
	query := `SELECT side_a_pk, side_z_pk FROM dz_links_current WHERE pk = $1`
	var sideA, sideZ string
	if err := config.DB.QueryRow(ctx, query, linkPK).Scan(&sideA, &sideZ); err != nil {
		return ""
	}
	if sideA == "" || sideZ == "" {
		return ""
	}
	return sideA + ":" + sideZ
}

// computeAffectedPaths finds paths that would be affected by the maintenance
// and computes alternate routes with before/after hops and metrics
func computeAffectedPaths(ctx context.Context, session neo4j.SessionWithContext,
	offlineDevices map[string]bool, offlineLinks map[string]bool, limit int) []MaintenanceAffectedPath {

	result := []MaintenanceAffectedPath{}

	offlineDevicePKs := make([]string, 0, len(offlineDevices))
	for pk := range offlineDevices {
		offlineDevicePKs = append(offlineDevicePKs, pk)
	}

	// If no offline devices, skip the path computation
	if len(offlineDevicePKs) == 0 {
		return result
	}

	// Find paths that go through offline devices, compute before/after metrics
	// This query finds the original shortest path, then tries to find an alternate
	cypher := `
		// First, find device pairs where the shortest path goes through an offline device
		MATCH (source:Device), (target:Device)
		WHERE source.isis_system_id IS NOT NULL
		  AND target.isis_system_id IS NOT NULL
		  AND source.pk < target.pk
		  AND NOT source.pk IN $offlineDevicePKs
		  AND NOT target.pk IN $offlineDevicePKs

		// Find the current shortest path
		MATCH originalPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
		WHERE any(n IN nodes(originalPath) WHERE n.pk IN $offlineDevicePKs)

		// Calculate original path metrics
		WITH source, target, originalPath,
		     length(originalPath) AS hopsBefore,
		     reduce(m = 0, r IN relationships(originalPath) | m + coalesce(r.metric, 10)) AS metricBefore

		LIMIT $limit

		// Get metro info
		OPTIONAL MATCH (source)-[:LOCATED_IN]->(sm:Metro)
		OPTIONAL MATCH (target)-[:LOCATED_IN]->(tm:Metro)

		// Return with source/target PKs for alternate path lookup
		RETURN source.pk AS sourcePK, source.code AS sourceCode,
		       target.pk AS targetPK, target.code AS targetCode,
		       COALESCE(sm.code, 'unknown') AS sourceMetro,
		       COALESCE(tm.code, 'unknown') AS targetMetro,
		       hopsBefore, metricBefore
	`

	records, err := session.Run(ctx, cypher, map[string]interface{}{
		"offlineDevicePKs": offlineDevicePKs,
		"limit":            limit,
	})
	if err != nil {
		log.Printf("Error computing affected paths: %v", err)
		return result
	}

	// Collect paths that need alternate route computation
	type pathInfo struct {
		sourcePK     string
		targetPK     string
		sourceCode   string
		targetCode   string
		sourceMetro  string
		targetMetro  string
		hopsBefore   int
		metricBefore int
	}
	paths := []pathInfo{}

	for records.Next(ctx) {
		record := records.Record()
		sourcePK, _ := record.Get("sourcePK")
		targetPK, _ := record.Get("targetPK")
		sourceCode, _ := record.Get("sourceCode")
		targetCode, _ := record.Get("targetCode")
		sourceMetro, _ := record.Get("sourceMetro")
		targetMetro, _ := record.Get("targetMetro")
		hopsBefore, _ := record.Get("hopsBefore")
		metricBefore, _ := record.Get("metricBefore")

		paths = append(paths, pathInfo{
			sourcePK:     asString(sourcePK),
			targetPK:     asString(targetPK),
			sourceCode:   asString(sourceCode),
			targetCode:   asString(targetCode),
			sourceMetro:  asString(sourceMetro),
			targetMetro:  asString(targetMetro),
			hopsBefore:   int(asInt64(hopsBefore)),
			metricBefore: int(asInt64(metricBefore)),
		})
	}

	// For each affected path, try to find an alternate route
	for _, p := range paths {
		affectedPath := MaintenanceAffectedPath{
			Source:       p.sourceCode,
			Target:       p.targetCode,
			SourceMetro:  p.sourceMetro,
			TargetMetro:  p.targetMetro,
			HopsBefore:   p.hopsBefore,
			MetricBefore: p.metricBefore,
			HopsAfter:    -1,
			MetricAfter:  -1,
			Status:       "disconnected",
		}

		// Try to find alternate path avoiding offline devices
		altCypher := `
			MATCH (source:Device {pk: $sourcePK}), (target:Device {pk: $targetPK})

			// Find shortest path that avoids offline devices
			MATCH altPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
			WHERE none(n IN nodes(altPath) WHERE n.pk IN $offlineDevicePKs)

			WITH altPath, length(altPath) AS hopsAfter,
			     reduce(m = 0, r IN relationships(altPath) | m + coalesce(r.metric, 10)) AS metricAfter

			RETURN hopsAfter, metricAfter
			LIMIT 1
		`

		altRecords, err := session.Run(ctx, altCypher, map[string]interface{}{
			"sourcePK":         p.sourcePK,
			"targetPK":         p.targetPK,
			"offlineDevicePKs": offlineDevicePKs,
		})
		if err == nil && altRecords.Next(ctx) {
			record := altRecords.Record()
			hopsAfter, _ := record.Get("hopsAfter")
			metricAfter, _ := record.Get("metricAfter")

			affectedPath.HopsAfter = int(asInt64(hopsAfter))
			affectedPath.MetricAfter = int(asInt64(metricAfter))

			// Determine status based on degradation
			hopIncrease := affectedPath.HopsAfter - affectedPath.HopsBefore
			metricIncrease := affectedPath.MetricAfter - affectedPath.MetricBefore

			if hopIncrease > 2 || metricIncrease > 100 {
				affectedPath.Status = "degraded"
			} else {
				affectedPath.Status = "rerouted"
			}
		}

		result = append(result, affectedPath)
	}

	return result
}

// computeAffectedMetros computes metro pairs whose connectivity is impacted
func computeAffectedMetros(ctx context.Context, session neo4j.SessionWithContext,
	offlineDevices map[string]bool, offlineLinks map[string]bool) []AffectedMetroPair {

	result := []AffectedMetroPair{}

	offlineDevicePKs := make([]string, 0, len(offlineDevices))
	for pk := range offlineDevices {
		offlineDevicePKs = append(offlineDevicePKs, pk)
	}

	if len(offlineDevicePKs) == 0 {
		return result
	}

	// Find metro pairs that have paths going through offline devices
	cypher := `
		MATCH (d:Device)-[:LOCATED_IN]->(m:Metro)
		WHERE d.pk IN $offlineDevicePKs
		WITH collect(DISTINCT m.code) AS affectedMetros, collect(d.pk) AS offlineDevices

		MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)-[:ISIS_ADJACENT*1..3]-(d2:Device)-[:LOCATED_IN]->(m2:Metro)
		WHERE m1.code < m2.code
		  AND d1.isis_system_id IS NOT NULL
		  AND d2.isis_system_id IS NOT NULL
		  AND (m1.code IN affectedMetros OR m2.code IN affectedMetros OR
		       any(dk IN offlineDevices WHERE any(n IN nodes((d1)-[:ISIS_ADJACENT*1..3]-(d2)) WHERE n.pk = dk)))

		WITH m1.code AS metro1, m2.code AS metro2, count(*) AS pathCount
		WHERE pathCount > 0

		RETURN metro1, metro2, pathCount
		ORDER BY pathCount DESC
		LIMIT 10
	`

	records, err := session.Run(ctx, cypher, map[string]interface{}{
		"offlineDevicePKs": offlineDevicePKs,
	})
	if err != nil {
		log.Printf("Error computing affected metros: %v", err)
		return result
	}

	for records.Next(ctx) {
		record := records.Record()
		metro1, _ := record.Get("metro1")
		metro2, _ := record.Get("metro2")
		pathCount, _ := record.Get("pathCount")

		pair := AffectedMetroPair{
			SourceMetro: asString(metro1),
			TargetMetro: asString(metro2),
			PathsBefore: int(asInt64(pathCount)),
			PathsAfter:  0, // Simplified - would need more complex query
			Status:      "reduced",
		}

		result = append(result, pair)
	}

	return result
}

// analyzeDeviceImpact computes the impact of taking a single device offline
func analyzeDeviceImpact(ctx context.Context, session neo4j.SessionWithContext, devicePK string) MaintenanceItem {
	item := MaintenanceItem{
		Type: "device",
		PK:   devicePK,
	}

	// Get device code
	codeCypher := `
		MATCH (d:Device {pk: $pk})
		RETURN d.code AS code
	`
	codeResult, err := session.Run(ctx, codeCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		if record, err := codeResult.Single(ctx); err == nil {
			if code, ok := record.Get("code"); ok {
				item.Code = asString(code)
			}
		}
	}

	// Count paths that go through this device
	pathsCypher := `
		MATCH (d:Device {pk: $pk})
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (other:Device)
		WHERE other.isis_system_id IS NOT NULL AND other.pk <> d.pk
		OPTIONAL MATCH path = shortestPath((other)-[:ISIS_ADJACENT*]-(d))
		WITH d, count(path) AS pathCount
		RETURN pathCount
	`
	pathsResult, err := session.Run(ctx, pathsCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		if record, err := pathsResult.Single(ctx); err == nil {
			if pathCount, ok := record.Get("pathCount"); ok {
				item.Impact = int(asInt64(pathCount))
			}
		}
	}

	// Check if this device is critical (would disconnect others)
	// A device is critical if any of its neighbors have degree 1 (only connected to this device)
	criticalCypher := `
		MATCH (d:Device {pk: $pk})-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE d.isis_system_id IS NOT NULL AND neighbor.isis_system_id IS NOT NULL
		WITH neighbor
		MATCH (neighbor)-[:ISIS_ADJACENT]-(any:Device)
		WHERE any.isis_system_id IS NOT NULL
		WITH neighbor, count(DISTINCT any) AS degree
		WHERE degree = 1
		RETURN neighbor.code AS disconnectedCode
	`
	criticalResult, err := session.Run(ctx, criticalCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		for criticalResult.Next(ctx) {
			record := criticalResult.Record()
			if code, ok := record.Get("disconnectedCode"); ok {
				item.DisconnectedDevices = append(item.DisconnectedDevices, asString(code))
				item.Disconnected++
			}
		}
		item.CausesPartition = item.Disconnected > 0
	}

	return item
}

// analyzeLinkImpact computes the impact of taking a single link offline
func analyzeLinkImpact(ctx context.Context, session neo4j.SessionWithContext, linkPK string) MaintenanceItem {
	item := MaintenanceItem{
		Type: "link",
		PK:   linkPK,
	}

	// Look up link from ClickHouse to get side_a_pk and side_z_pk
	linkQuery := `
		SELECT
			l.code,
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(dz.code, '') as side_z_code
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		WHERE l.pk = $1
	`
	var linkCode, sideAPK, sideZPK, sideACode, sideZCode string
	if err := config.DB.QueryRow(ctx, linkQuery, linkPK).Scan(&linkCode, &sideAPK, &sideZPK, &sideACode, &sideZCode); err != nil {
		item.Code = "Link not found"
		return item
	}

	if sideAPK == "" || sideZPK == "" {
		item.Code = linkCode + " (missing endpoints)"
		return item
	}

	item.Code = sideACode + " - " + sideZCode
	sourcePK, targetPK := sideAPK, sideZPK

	// Check if removing this link would disconnect devices
	// If either endpoint has degree 1, removing the link disconnects that device
	degreeCypher := `
		MATCH (s:Device {pk: $sourcePK}), (t:Device {pk: $targetPK})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device) WHERE sn.isis_system_id IS NOT NULL
		WITH s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device) WHERE tn.isis_system_id IS NOT NULL
		WITH s, t, sourceDegree, count(DISTINCT tn) AS targetDegree
		RETURN s.code AS sourceCode, t.code AS targetCode, sourceDegree, targetDegree
	`
	degreeResult, err := session.Run(ctx, degreeCypher, map[string]interface{}{
		"sourcePK": sourcePK,
		"targetPK": targetPK,
	})
	if err == nil {
		if record, err := degreeResult.Single(ctx); err == nil {
			sourceCode, _ := record.Get("sourceCode")
			targetCode, _ := record.Get("targetCode")
			sourceDegree, _ := record.Get("sourceDegree")
			targetDegree, _ := record.Get("targetDegree")
			sDeg := int(asInt64(sourceDegree))
			tDeg := int(asInt64(targetDegree))

			// If either has degree 1, this link is critical
			if sDeg == 1 || tDeg == 1 {
				item.CausesPartition = true
				if sDeg == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, asString(sourceCode))
					item.Disconnected++
				}
				if tDeg == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, asString(targetCode))
					item.Disconnected++
				}
			}
		}
	}

	// Count paths that use this link
	// We count device pairs where the shortest path goes through this link
	pathsCypher := `
		MATCH (s:Device {pk: $sourcePK})-[:ISIS_ADJACENT]-(t:Device {pk: $targetPK})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL
		// Get neighbors of source (excluding target)
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sNeighbor:Device)
		WHERE sNeighbor.isis_system_id IS NOT NULL AND sNeighbor.pk <> t.pk
		WITH s, t, collect(DISTINCT sNeighbor.pk) AS sourceNeighbors
		// Get neighbors of target (excluding source)
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tNeighbor:Device)
		WHERE tNeighbor.isis_system_id IS NOT NULL AND tNeighbor.pk <> s.pk
		WITH sourceNeighbors, collect(DISTINCT tNeighbor.pk) AS targetNeighbors
		// Rough estimate: paths affected = sourceNeighbors * targetNeighbors
		RETURN size(sourceNeighbors) * size(targetNeighbors) AS affectedPaths
	`
	pathsResult, err := session.Run(ctx, pathsCypher, map[string]interface{}{
		"sourcePK": sourcePK,
		"targetPK": targetPK,
	})
	if err == nil {
		if record, err := pathsResult.Single(ctx); err == nil {
			if affectedPaths, ok := record.Get("affectedPaths"); ok {
				item.Impact = int(asInt64(affectedPaths))
			}
		}
	}

	return item
}
