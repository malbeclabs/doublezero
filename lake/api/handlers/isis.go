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
